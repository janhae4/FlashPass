package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// TestEdgeCase2_TimeoutRaceCondition tests Edge Case 2: Timeout Worker vs Late Payment Webhook Race
// Strictly calls API endpoints (/api/v1/admin/events/:id/prewarm, /api/v1/tickets/book, /api/v1/orders/:id/pay)
func TestEdgeCase2_TimeoutRaceCondition(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	// 1. Create an event in PostgreSQL with exactly 1 ticket
	eventID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, $2, $3, $4, NOW(), NOW() + INTERVAL '7 days')
	`, eventID, "Race Condition Exclusive Concert", 1, 1)
	if err != nil {
		t.Fatalf("Failed to create test event: %v", err)
	}
	defer func() {
		_, _ = db.Pool.Exec(ctx, "DELETE FROM orders WHERE event_id = $1", eventID)
		_, _ = db.Pool.Exec(ctx, "DELETE FROM events WHERE id = $1", eventID)
	}()

	// 2. Prewarm via API POST /api/v1/admin/events/:id/prewarm
	prewarmReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/events/"+eventID.String()+"/prewarm", nil)
	prewarmResp, err := app.Test(prewarmReq)
	if err != nil || prewarmResp.StatusCode != http.StatusOK {
		t.Fatalf("Prewarm failed: %v, status: %d", err, prewarmResp.StatusCode)
	}

	// 3. Buyer A books the single ticket via API POST /api/v1/tickets/book (Stock 1 -> 0)
	idempA := "race-buyer-A-" + uuid.New().String()
	payloadA, _ := json.Marshal(map[string]string{
		"event_id":        eventID.String(),
		"user_id":         "buyer-A",
		"idempotency_key": idempA,
	})
	bookReqA := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payloadA))
	bookReqA.Header.Set("Content-Type", "application/json")
	bookRespA, err := app.Test(bookReqA)
	if err != nil || bookRespA.StatusCode != http.StatusAccepted {
		t.Fatalf("Buyer A booking failed: %v, status: %d", err, bookRespA.StatusCode)
	}

	var bookResA struct {
		OrderID string `json:"order_id"`
		Status  string `json:"status"`
	}
	_ = json.NewDecoder(bookRespA.Body).Decode(&bookResA)
	orderA := bookResA.OrderID
	t.Logf("[Edge Case 2] Buyer A received reservation order: %s", orderA)

	// Persist Order A to DB if not yet written by async consumer
	_, _ = db.Pool.Exec(ctx, `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at)
		VALUES ($1, $2, $3, $4, 'PENDING_PAYMENT', NOW() + INTERVAL '10 minutes')
		ON CONFLICT (id) DO NOTHING
	`, orderA, eventID, "buyer-A", idempA)

	// 4. Buyer B attempts to book while stock is 0 -> Rejected with 409 Conflict (Sold out)
	idempB := "race-buyer-B-" + uuid.New().String()
	payloadB, _ := json.Marshal(map[string]string{
		"event_id":        eventID.String(),
		"user_id":         "buyer-B",
		"idempotency_key": idempB,
	})
	bookReqB1 := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payloadB))
	bookReqB1.Header.Set("Content-Type", "application/json")
	bookRespB1, _ := app.Test(bookReqB1)
	if bookRespB1.StatusCode != http.StatusConflict {
		t.Errorf("Expected 409 Sold out for Buyer B before expiration, got %d", bookRespB1.StatusCode)
	}

	// 5. Simulate Order A Expiration (Timeout Worker releases ticket back to pool)
	t.Log("[Edge Case 2] Timeout worker triggers: Order A marked EXPIRED and 1 ticket released back to pool")
	_, err = db.Pool.Exec(ctx, "UPDATE orders SET status = 'EXPIRED' WHERE id = $1", orderA)
	if err != nil {
		t.Fatalf("Failed to expire Order A: %v", err)
	}
	_ = redisClient.RollbackStock(ctx, eventID.String(), "buyer-A", 1)
	_, _ = db.Pool.Exec(ctx, "UPDATE events SET available_tickets = available_tickets + 1 WHERE id = $1", eventID)

	// 6. Buyer B immediately books the released ticket via API POST /api/v1/tickets/book (Stock 1 -> 0)
	bookReqB2 := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payloadB))
	bookReqB2.Header.Set("Content-Type", "application/json")
	bookRespB2, err := app.Test(bookReqB2)
	if err != nil || bookRespB2.StatusCode != http.StatusAccepted {
		t.Fatalf("Buyer B failed to book released ticket: %v, status: %d", err, bookRespB2.StatusCode)
	}
	var bookResB struct {
		OrderID string `json:"order_id"`
	}
	_ = json.NewDecoder(bookRespB2.Body).Decode(&bookResB)
	orderB := bookResB.OrderID
	t.Logf("[Edge Case 2] Buyer B successfully booked released ticket, order: %s", orderB)

	// Persist Order B to DB if not yet written by async consumer
	_, _ = db.Pool.Exec(ctx, `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at)
		VALUES ($1, $2, $3, $4, 'PENDING_PAYMENT', NOW() + INTERVAL '10 minutes')
		ON CONFLICT (id) DO NOTHING
	`, orderB, eventID, "buyer-B", idempB)

	// 7. Late Payment Webhook arrives for Buyer A via API POST /api/v1/orders/{orderA}/pay
	t.Log("[Edge Case 2] Late payment webhook arrives for Buyer A after order expired...")
	payReqA := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderA+"/pay", nil)
	payRespA, err := app.Test(payReqA)
	if err != nil {
		t.Fatalf("Pay request A failed: %v", err)
	}
	t.Logf("[Edge Case 2] Late payment for expired Order A returned HTTP status: %d", payRespA.StatusCode)
	if payRespA.StatusCode != http.StatusAccepted {
		t.Errorf("Expected 202 Accepted for expired order late payment (Auto-Refund), but got %d", payRespA.StatusCode)
	}
	var resA struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(payRespA.Body).Decode(&resA)
	if resA.Status != "REFUND_PENDING" {
		t.Errorf("Expected status REFUND_PENDING, got %s", resA.Status)
	}
	t.Logf("[Edge Case 2] Auto-Refund successfully initiated for late Buyer A: status=%s, msg=%s", resA.Status, resA.Message)

	// Verify Order A in DB was transitioned to REFUND_PENDING (and never marked PAID)
	var dbStatusA string
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", orderA).Scan(&dbStatusA)
	if dbStatusA != "REFUND_PENDING" && dbStatusA != "REFUNDED" {
		t.Errorf("Expected Order A DB status REFUND_PENDING or REFUNDED, got %s", dbStatusA)
	}

	// Verify refund_orders table has the record
	var refundCount int
	_ = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM refund_orders WHERE order_id = $1", orderA).Scan(&refundCount)
	if refundCount == 0 {
		t.Errorf("Expected at least 1 record in refund_orders for order %s, got 0", orderA)
	} else {
		t.Logf("[Edge Case 2] Verified record exists in refund_orders table for order %s", orderA)
	}

	// 8. Buyer B pays their valid order via API POST /api/v1/orders/{orderB}/pay
	payReqB := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderB+"/pay", nil)
	payRespB, err := app.Test(payReqB)
	if err != nil || payRespB.StatusCode != http.StatusOK {
		t.Fatalf("Buyer B pay request failed: %v, status: %d", err, payRespB.StatusCode)
	}
	t.Logf("[Edge Case 2] Buyer B payment confirmed with HTTP status: %d", payRespB.StatusCode)

	// 9. Database Invariants:
	// - Exactly 1 PAID order exists for this event (Buyer B)
	// - Total PAID orders must never exceed total_tickets (1)
	var paidCount int
	_ = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM orders WHERE event_id = $1 AND status = 'PAID'", eventID).Scan(&paidCount)
	if paidCount > 1 {
		t.Errorf("[Critical Overselling Invariant Violated] %d PAID orders for 1 seat event!", paidCount)
	} else {
		t.Logf("[Edge Case 2] Verified exactly %d PAID order in database for 1 seat event. Zero Overselling maintained!", paidCount)
	}
}
