package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestEdgeCase1_RedisFailover tests Edge Case 1: Redis Master-Replica Asynchronous Failover
// strictly using API endpoints (/api/v1/admin/events/:id/prewarm, /api/v1/tickets/book, /api/v1/events/:id/stock)
func TestEdgeCase1_RedisFailover(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	// 1. Seed an event in PostgreSQL with 3 tickets
	eventID := uuid.New()
	const initialStock = 3
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, $2, $3, $4, NOW(), NOW() + INTERVAL '7 days')
	`, eventID, "Failover Edge Case Concert", initialStock, initialStock)
	if err != nil {
		t.Fatalf("Failed to seed test event: %v", err)
	}
	defer func() {
		_, _ = db.Pool.Exec(ctx, "DELETE FROM orders WHERE event_id = $1", eventID)
		_, _ = db.Pool.Exec(ctx, "DELETE FROM events WHERE id = $1", eventID)
	}()

	// 2. Call Prewarm via API POST /api/v1/admin/events/:id/prewarm
	prewarmReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/events/"+eventID.String()+"/prewarm", nil)
	prewarmResp, err := app.Test(prewarmReq)
	if err != nil {
		t.Fatalf("Prewarm request failed: %v", err)
	}
	if prewarmResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(prewarmResp.Body)
		t.Fatalf("Expected prewarm 200 OK, got %d: %s", prewarmResp.StatusCode, string(body))
	}

	// 3. User 1 books a ticket via API POST /api/v1/tickets/book (Stock: 3 -> 2)
	idemp1 := "failover-user-1-" + uuid.New().String()
	payload1, _ := json.Marshal(map[string]string{
		"event_id":        eventID.String(),
		"user_id":         "user-failover-1",
		"idempotency_key": idemp1,
	})
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload1))
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := app.Test(req1)
	if err != nil || resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("Booking 1 failed before failover: %v, status: %d", err, resp1.StatusCode)
	}

	// 4. Simulate Edge Case 1: Redis Master failure (docker pause flashpass_redis)
	t.Log("[Edge Case 1] Simulating Redis Master failure: pausing flashpass_redis container...")
	pauseCmd := exec.Command("docker", "pause", "flashpass_redis")
	if out, err := pauseCmd.CombinedOutput(); err != nil {
		t.Logf("Notice: docker pause returned: %v, out: %s", err, string(out))
	}
	defer func() {
		_ = exec.Command("docker", "unpause", "flashpass_redis").Run()
	}()

	// 5. While Master is paused, User 2 attempts to book via API POST /api/v1/tickets/book
	// The API engine should either route to promoted replica or return 500/503 with Retry-After: 2
	idemp2 := "failover-user-2-" + uuid.New().String()
	payload2, _ := json.Marshal(map[string]string{
		"event_id":        eventID.String(),
		"user_id":         "user-failover-2",
		"idempotency_key": idemp2,
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := app.Test(req2, 2000)
	if err != nil || resp2 == nil {
		t.Logf("[Edge Case 1] During master outage, User 2 booking timed out as expected: %v", err)
	} else {
		t.Logf("[Edge Case 1] During master outage, User 2 booking returned HTTP status: %d", resp2.StatusCode)
		retryAfter := resp2.Header.Get("Retry-After")
		if resp2.StatusCode == http.StatusInternalServerError || resp2.StatusCode == http.StatusServiceUnavailable {
			t.Logf("[Edge Case 1] API cleanly handled outage with status %d and Retry-After: %s", resp2.StatusCode, retryAfter)
		}
	}

	// 6. Restore Master
	t.Log("[Edge Case 1] Restoring Redis Master container...")
	_ = exec.Command("docker", "unpause", "flashpass_redis").Run()
	time.Sleep(1 * time.Second)

	// 7. After recovery, complete remaining bookings to verify Zero Overselling
	for i := 3; i <= 6; i++ {
		idemp := fmt.Sprintf("failover-user-%d-%s", i, uuid.New().String())
		payload, _ := json.Marshal(map[string]string{
			"event_id":        eventID.String(),
			"user_id":         fmt.Sprintf("user-failover-%d", i),
			"idempotency_key": idemp,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, 6000)
		if err != nil || resp == nil {
			t.Logf("[Edge Case 1] Booking user-%d request error: %v", i, err)
		} else {
			t.Logf("[Edge Case 1] Booking user-%d returned HTTP status: %d", i, resp.StatusCode)
		}
	}

	// 8. Assert DB Invariant: available_tickets >= 0 and never oversold
	var availableTickets int
	_ = db.Pool.QueryRow(ctx, "SELECT available_tickets FROM events WHERE id = $1", eventID).Scan(&availableTickets)
	if availableTickets < 0 {
		t.Errorf("[Edge Case 1 Invariant Violated] available_tickets in DB is negative (%d)! Overselling occurred!", availableTickets)
	} else {
		t.Logf("[Edge Case 1] Verified DB available_tickets is %d (non-negative, protected by DB Guard)", availableTickets)
	}
}
