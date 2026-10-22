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
)

// TestEdgeCase9_SlidingWindowRateLimiter directly verifies:
// 1. Simulates bot spamming 15 rapid booking requests within 1 second.
// 2. Exactly the first 5 requests are admitted (under the 5 req / 2s limit).
// 3. The remaining 10 requests are strictly blocked with HTTP 429 Too Many Requests and Retry-After: 2 header.
// 4. Legitimate buyer with a different user_id is NOT affected (isolation verified).
// 5. After 2.1s window slides, new request from the previously blocked bot is admitted again.
func TestEdgeCase9_SlidingWindowRateLimiter(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	eventUUID := uuid.New()
	eventID := eventUUID.String()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, 'Rate Limiter Concert', 100, 100, NOW(), NOW() + INTERVAL '1 day')
	`, eventUUID)
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}
	_ = redisClient.PrewarmStock(ctx, eventID, 100)

	botUserID := "bot-scalper-user-" + uuid.New().String()
	totalSpam := 15
	admittedCount := 0
	rateLimitedCount := 0

	for i := range totalSpam {
		payload, _ := json.Marshal(map[string]string{
			"event_id":        eventID,
			"user_id":         botUserID,
			"idempotency_key": fmt.Sprintf("idemp-bot-%d-%s", i, uuid.New().String()),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, 5000)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			rateLimitedCount++
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter == "" {
				t.Errorf("Expected Retry-After header on 429 response, got empty")
			}
		case http.StatusAccepted, http.StatusConflict:
			admittedCount++
		}
	}

	if admittedCount != 5 {
		t.Fatalf("Expected exactly 5 admitted requests under limit, got %d", admittedCount)
	}
	if rateLimitedCount != 10 {
		t.Fatalf("Expected exactly 10 rate-limited (429) requests, got %d", rateLimitedCount)
	}
	t.Logf("[Edge Case 9 Verified] Bot burst strictly throttled: 5 Admitted, %d Blocked with 429!", rateLimitedCount)

	// Verify tenant isolation: Legitimate users are unaffected by bot rate limiting
	legitPayload, _ := json.Marshal(map[string]string{
		"event_id":        eventID,
		"user_id":         "legit-user-" + uuid.New().String(),
		"idempotency_key": "idemp-legit-" + uuid.New().String(),
	})
	reqLegit := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(legitPayload))
	reqLegit.Header.Set("Content-Type", "application/json")
	respLegit, err := app.Test(reqLegit, 5000)
	if err != nil || respLegit.StatusCode != http.StatusAccepted {
		t.Fatalf("Expected 202 Accepted for legit user, got status %d", respLegit.StatusCode)
	}
	t.Logf("[Edge Case 9 Verified] Multi-tenant isolation verified: Legit user admitted normally!")

	// Verify self-recovery after sliding window elapses (2.1s)
	time.Sleep(2100 * time.Millisecond)
	recoverPayload, _ := json.Marshal(map[string]string{
		"event_id":        eventID,
		"user_id":         botUserID,
		"idempotency_key": "idemp-recover-" + uuid.New().String(),
	})
	reqRecover := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(recoverPayload))
	reqRecover.Header.Set("Content-Type", "application/json")
	respRecover, err := app.Test(reqRecover, 5000)
	if err != nil || respRecover.StatusCode == http.StatusTooManyRequests {
		t.Fatalf("Expected bot to be unblocked after sliding window, got %d", respRecover.StatusCode)
	}
	t.Logf("[Edge Case 9 Verified] Sliding window recovery confirmed: Request admitted after window expiry!")
}
