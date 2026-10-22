package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"flashpass/backend/internal/models"
	"flashpass/backend/internal/repository"
)

// TestEdgeCase8_ClockDriftGraceWindow directly verifies:
// 1. Boundary payment arriving 2s after expires_at (within 5s grace window) succeeds with 200 OK.
// 2. Late payment arriving 10s after expires_at (past 5s grace window) is rejected and triggers 202 Auto-Refund.
// 3. Premature worker expiration attempt when expires_at > NOW() is strictly rejected by DB guard (zero premature releases).
func TestEdgeCase8_ClockDriftGraceWindow(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	// 1. Seed test event
	eventUUID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, 'Clock Drift Event', 10, 8, NOW(), NOW() + INTERVAL '1 day')
	`, eventUUID)
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}

	// Scenario 1: Payment arrives 2s late (within 5s Grace Window) -> Succeeds with 200 OK
	graceOrderUUID := uuid.New()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, 'user-grace-buyer', $3, 'PENDING_PAYMENT', NOW() - INTERVAL '2 seconds', NOW(), NOW())
	`, graceOrderUUID, eventUUID, "idemp-grace-"+uuid.New().String())
	if err != nil {
		t.Fatalf("Failed to create grace order: %v", err)
	}

	reqGrace := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/pay", graceOrderUUID), nil)
	respGrace, err := app.Test(reqGrace, 5000)
	if err != nil || respGrace.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for payment within grace window, got code=%d, err=%v", respGrace.StatusCode, err)
	}

	var graceStatus models.OrderStatus
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", graceOrderUUID).Scan(&graceStatus)
	if graceStatus != models.OrderStatusPaid {
		t.Fatalf("Expected order to be PAID within grace window, got %s", graceStatus)
	}
	t.Logf("[Edge Case 8 Verified] Payment arriving 2s past deadline accepted under 5s Grace Window with 200 OK!")

	// Scenario 2: Payment arrives 10s late (exceeding 5s Grace Window) -> Routed to Auto-Refund with 202 Accepted
	lateOrderUUID := uuid.New()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, 'user-late-buyer', $3, 'PENDING_PAYMENT', NOW() - INTERVAL '10 seconds', NOW(), NOW())
	`, lateOrderUUID, eventUUID, "idemp-late-"+uuid.New().String())
	if err != nil {
		t.Fatalf("Failed to create late order: %v", err)
	}

	reqLate := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/pay", lateOrderUUID), nil)
	respLate, err := app.Test(reqLate, 5000)
	if err != nil || respLate.StatusCode != http.StatusAccepted {
		t.Fatalf("Expected 202 Accepted for payment past grace window, got code=%d, err=%v", respLate.StatusCode, err)
	}

	var lateStatus models.OrderStatus
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", lateOrderUUID).Scan(&lateStatus)
	if lateStatus != models.OrderStatusRefundPending {
		t.Fatalf("Expected order to be REFUND_PENDING past grace window, got %s", lateStatus)
	}
	t.Logf("[Edge Case 8 Verified] Payment arriving 10s past deadline correctly routed to Auto-Refund with 202 Accepted!")

	// Scenario 3: Worker attempts to expire an order whose expires_at is in the future (+30s) -> DB Guard rejects atomically
	futureOrderUUID := uuid.New()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, 'user-future-buyer', $3, 'PENDING_PAYMENT', NOW() + INTERVAL '30 seconds', NOW(), NOW())
	`, futureOrderUUID, eventUUID, "idemp-future-"+uuid.New().String())
	if err != nil {
		t.Fatalf("Failed to create future order: %v", err)
	}

	orderRepo := repository.NewOrderRepository(db)
	expired, err := orderRepo.ExpireIfPastDeadline(ctx, futureOrderUUID)
	if err != nil {
		t.Fatalf("Unexpected error from ExpireIfPastDeadline: %v", err)
	}
	if expired {
		t.Fatalf("Expected ExpireIfPastDeadline to return false for future order, but returned true (Clock Skew Vulnerability)!")
	}

	var futureStatus models.OrderStatus
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", futureOrderUUID).Scan(&futureStatus)
	if futureStatus != models.OrderStatusPendingPayment {
		t.Fatalf("Future order was prematurely changed to %s!", futureStatus)
	}
	t.Logf("[Edge Case 8 Verified] DB-Authoritative Guard prevented premature expiration of future order!")
}
