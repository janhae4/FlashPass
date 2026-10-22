package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"flashpass/backend/internal/redis"
)

// TestEdgeCase3_DualWriteGhostInventory tests Edge Case 3: Dual-Write Partial Failure (Ghost Inventory) & Self-Healing
// Directly targets API endpoints (/api/v1/admin/events/:id/prewarm, /api/v1/tickets/book, /api/v1/events/:id/stock, /api/v1/admin/events/:id/reconcile)
func TestEdgeCase3_DualWriteGhostInventory(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	// 1. Seed test event with exactly 1 ticket in PostgreSQL
	eventID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, $2, $3, $4, NOW(), NOW() + INTERVAL '7 days')
	`, eventID, "Ghost Inventory Edge Event", 1, 1)
	if err != nil {
		t.Fatalf("Failed to seed event: %v", err)
	}
	defer func() {
		_, _ = db.Pool.Exec(ctx, "DELETE FROM orders WHERE event_id = $1", eventID)
		_, _ = db.Pool.Exec(ctx, "DELETE FROM events WHERE id = $1", eventID)
	}()

	// 2. Prewarm via API POST /api/v1/admin/events/:id/prewarm
	prewarmReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/events/"+eventID.String()+"/prewarm", nil)
	prewarmResp, err := app.Test(prewarmReq)
	if err != nil || prewarmResp.StatusCode != http.StatusOK {
		t.Fatalf("Prewarm request failed: %v", err)
	}

	// 3. Simulate Dual-Write Partial Failure:
	// A booking crashes or loses network right after Redis Lua decrements stock (1 -> 0),
	// but before any order record can be stored or queued into DB.
	t.Log("[Edge Case 3] Simulating Partial Failure: Redis decremented but 0 orders in PostgreSQL...")
	_, _ = redisClient.BookTicket(ctx, eventID.String(), "crashed-buyer-1", 1)

	// 4. Verify Ghost Inventory Symptom via API:
	// - Stock in Redis is 0 (GET /api/v1/events/:id/stock)
	stockReq := httptest.NewRequest(http.MethodGet, "/api/v1/events/"+eventID.String()+"/stock", nil)
	stockResp, err := app.Test(stockReq)
	if err != nil || stockResp.StatusCode != http.StatusOK {
		t.Fatalf("Get stock request failed: %v", err)
	}
	var stockRes struct {
		Stock int `json:"stock"`
	}
	_ = json.NewDecoder(stockResp.Body).Decode(&stockRes)
	if stockRes.Stock != 0 {
		t.Errorf("Expected Redis stock to be 0 after partial failure, got %d", stockRes.Stock)
	}

	// - Buyer 2 tries to book via API POST /api/v1/tickets/book -> Rejected with 409 Conflict (Sold out)
	idemp2 := "ghost-buyer-2-" + uuid.New().String()
	payload2, _ := json.Marshal(map[string]string{
		"event_id":        eventID.String(),
		"user_id":         "buyer-2",
		"idempotency_key": idemp2,
	})
	bookReq2 := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload2))
	bookReq2.Header.Set("Content-Type", "application/json")
	bookResp2, _ := app.Test(bookReq2)
	if bookResp2.StatusCode != http.StatusConflict {
		t.Errorf("Expected 409 Conflict (Sold out) due to ghost inventory, got %d", bookResp2.StatusCode)
	}

	// - Verify Database has 0 orders (Ghost inventory confirmed: stock is 0, orders is 0!)
	var orderCount int
	_ = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM orders WHERE event_id = $1", eventID).Scan(&orderCount)
	if orderCount != 0 {
		t.Errorf("Expected 0 orders in DB, got %d", orderCount)
	}
	t.Logf("[Edge Case 3 Confirmed] Ghost Inventory Detected! Stock=0 but Active Orders=0 (Ticket vanished into thin air)")

	// 5. Trigger Self-Healing via API POST /api/v1/admin/events/:id/reconcile
	t.Log("[Edge Case 3] Invoking Stock Auditor Reconciliation API...")
	reconcileReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/events/"+eventID.String()+"/reconcile", nil)
	reconcileResp, err := app.Test(reconcileReq)
	if err != nil || reconcileResp.StatusCode != http.StatusOK {
		t.Fatalf("Reconcile request failed: %v", err)
	}
	var reconcileRes struct {
		Discrepancy   int  `json:"discrepancy"`
		ExpectedStock int  `json:"expected_stock"`
		ActualStock   int  `json:"actual_stock"`
		Healed        bool `json:"healed"`
	}
	_ = json.NewDecoder(reconcileResp.Body).Decode(&reconcileRes)
	if !reconcileRes.Healed || reconcileRes.Discrepancy != 1 || reconcileRes.ExpectedStock != 1 {
		t.Errorf("Expected Auditor to heal 1 ghost ticket, got: %+v", reconcileRes)
	}
	t.Logf("[Edge Case 3 Self-Healed] Stock Auditor detected deficit=%d and restored Redis stock to %d",
		reconcileRes.Discrepancy, reconcileRes.ExpectedStock)

	// 6. After Healing, Buyer 2 books again via API POST /api/v1/tickets/book -> Successfully receives 202 Accepted!
	bookReq2Retry := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload2))
	bookReq2Retry.Header.Set("Content-Type", "application/json")
	bookResp2Retry, err := app.Test(bookReq2Retry)
	if err != nil || bookResp2Retry.StatusCode != http.StatusAccepted {
		t.Fatalf("Buyer 2 booking should succeed after healing, got status %d", bookResp2Retry.StatusCode)
	}
	t.Logf("[Edge Case 3 Verified] Buyer 2 successfully purchased the recovered ghost ticket (Status 202 Accepted)!")

	// 7. Verify Sub-Case 1: Ghost Surplus Clamping (Prevent Overselling when Redis > DB)
	_ = redisClient.PrewarmStock(ctx, eventID.String(), 5)
	surplusReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/events/%s/reconcile", eventID.String()), nil)
	surplusResp, err := app.Test(surplusReq)
	if err != nil || surplusResp.StatusCode != http.StatusOK {
		t.Fatalf("Surplus reconcile request failed: %v", err)
	}
	var surplusRes struct {
		Discrepancy   int  `json:"discrepancy"`
		ExpectedStock int  `json:"expected_stock"`
		ActualStock   int  `json:"actual_stock"`
		Healed        bool `json:"healed"`
	}
	_ = json.NewDecoder(surplusResp.Body).Decode(&surplusRes)
	if !surplusRes.Healed || surplusRes.Discrepancy != -4 {
		t.Errorf("Expected Auditor to clamp surplus discrepancy (-4), got: %+v", surplusRes)
	}
	t.Logf("[Edge Case 3 Sub-Case 1 Verified] Stock Auditor clamped Redis surplus stock from 5 down to %d (Preventing Overselling)!", surplusRes.ExpectedStock)

	// 8. Verify Sub-Case 2: Orphaned User Lock Self-Healing
	orphanedUserID := "user-3"
	_ = redisClient.RDB().Set(ctx, redis.UserBookedKey(eventID.String(), orphanedUserID), "1", 10*time.Minute).Err()

	// Booking fails with 409 Conflict due to stale lock
	payload3, _ := json.Marshal(map[string]string{
		"event_id":        eventID.String(),
		"user_id":         orphanedUserID,
		"idempotency_key": "idemp-user-3-" + uuid.New().String(),
	})
	bookReq3 := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload3))
	bookReq3.Header.Set("Content-Type", "application/json")
	bookResp3, _ := app.Test(bookReq3)
	if bookResp3.StatusCode != http.StatusConflict {
		t.Fatalf("Expected 409 Conflict for locked user, got %d", bookResp3.StatusCode)
	}

	// Call Admin Unlock API to clear orphaned lock
	unlockReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/events/%s/users/%s/unlock", eventID.String(), orphanedUserID), nil)
	unlockResp, err := app.Test(unlockReq)
	if err != nil || unlockResp.StatusCode != http.StatusOK {
		t.Fatalf("Unlock API failed: %v", err)
	}

	// Re-add 1 ticket to event and verify User 3 can now book successfully
	_ = redisClient.PrewarmStock(ctx, eventID.String(), 1)
	_, _ = db.Pool.Exec(ctx, "UPDATE events SET total_tickets = total_tickets + 1, available_tickets = 1 WHERE id = $1", eventID)

	bookReq3Retry := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload3))
	bookReq3Retry.Header.Set("Content-Type", "application/json")
	bookResp3Retry, _ := app.Test(bookReq3Retry)
	if bookResp3Retry.StatusCode != http.StatusAccepted {
		t.Fatalf("Expected 202 Accepted after unlocking user, got %d", bookResp3Retry.StatusCode)
	}
	t.Logf("[Edge Case 3 Sub-Case 2 Verified] Successfully unlocked orphaned user lock and completed booking (Status 202)!")
}
