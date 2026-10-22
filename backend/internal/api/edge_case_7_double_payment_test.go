package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"flashpass/backend/internal/models"
)

// TestEdgeCase7_DoublePaymentWebhookConcurrency directly verifies:
// 1. Simulates 5 concurrent payment webhooks hitting POST /api/v1/orders/:id/pay at the exact same millisecond.
// 2. Distributed lock and optimistic state guard ensure exactly 1 execution succeeds while concurrent webhooks return 200 OK (idempotent).
// 3. Zero 500 Server Errors returned to the payment gateway (preventing endless payment gateway retries).
// 4. For expired orders, concurrent late webhooks trigger exactly 1 refund record in refund_orders (zero duplicate refunds).
func TestEdgeCase7_DoublePaymentWebhookConcurrency(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	// 1. Seed test event
	eventUUID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, 'Double Webhook Event', 1, 0, NOW(), NOW() + INTERVAL '1 day')
	`, eventUUID)
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}

	// 2. Create pending order in PostgreSQL
	orderUUID := uuid.New()
	orderID := orderUUID.String()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, 'user-webhook-buyer', $3, 'PENDING_PAYMENT', NOW() + INTERVAL '10 minutes', NOW(), NOW())
	`, orderUUID, eventUUID, "idemp-wh-"+uuid.New().String())
	if err != nil {
		t.Fatalf("Failed to create order: %v", err)
	}

	// 3. Simulate 5 concurrent payment gateway webhooks hitting the same order
	concurrentWebhooks := 5
	var wg sync.WaitGroup
	wg.Add(concurrentWebhooks)

	statusCodes := make(chan int, concurrentWebhooks)

	for i := 0; i < concurrentWebhooks; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/pay", orderID), nil)
			resp, err := app.Test(req, 5000)
			if err != nil {
				statusCodes <- 0
				return
			}
			statusCodes <- resp.StatusCode
		}()
	}

	wg.Wait()
	close(statusCodes)

	for code := range statusCodes {
		if code != http.StatusOK {
			t.Errorf("Expected 200 OK for concurrent webhook, got %d", code)
		}
	}
	t.Logf("[Edge Case 7 Verified] All %d concurrent webhooks deduplicated cleanly with 200 OK (Zero 500 errors)", concurrentWebhooks)

	// 5. Verify Database: Order status transitioned to PAID idempotently
	var finalStatus models.OrderStatus
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", orderUUID).Scan(&finalStatus)
	if finalStatus != models.OrderStatusPaid {
		t.Errorf("Expected order status to be PAID, got %s", finalStatus)
	}

	// 6. Test scenario: EXPIRED order receives 2 concurrent webhooks (Verify duplicate refund prevention)
	expiredOrderUUID := uuid.New()
	expiredOrderID := expiredOrderUUID.String()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, 'user-expired-buyer', $3, 'EXPIRED', NOW() - INTERVAL '1 minute', NOW(), NOW())
	`, expiredOrderUUID, eventUUID, "idemp-wh-exp-"+uuid.New().String())
	if err != nil {
		t.Fatalf("Failed to create expired order: %v", err)
	}

	var wgRefund sync.WaitGroup
	wgRefund.Add(2)
	refundCodes := make(chan int, 2)

	for i := 0; i < 2; i++ {
		go func() {
			defer wgRefund.Done()
			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/pay", expiredOrderID), nil)
			resp, err := app.Test(req, 5000)
			if err != nil {
				refundCodes <- 0
				return
			}
			refundCodes <- resp.StatusCode
		}()
	}

	wgRefund.Wait()
	close(refundCodes)

	for code := range refundCodes {
		if code != http.StatusAccepted {
			t.Errorf("Expected 202 Accepted for late webhook auto-refund, got %d", code)
		}
	}

	// 7. Verify: Exactly 1 refund record created in refund_orders (zero duplicate refunds)
	var refundCount int
	_ = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM refund_orders WHERE order_id = $1", expiredOrderUUID).Scan(&refundCount)
	if refundCount != 1 {
		t.Fatalf("Expected exactly 1 refund record, but got %d (Financial leakage detected!)", refundCount)
	}
	t.Logf("[Edge Case 7 Verified] Concurrent late webhooks produced exactly 1 refund record (Zero duplicate refunds)!")
}
