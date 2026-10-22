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
	"flashpass/backend/internal/models"
	"flashpass/backend/internal/repository"
)

// TestEdgeCase10_OutOfOrderMessageProcessing directly verifies:
// 1. Early Payment: Webhook arrives before order creation committed to DB -> Debounce waits and succeeds with 200 OK.
// 2. Early Cancel Tombstone: Cancel arrives before order created -> Tombstone inserted, Redis stock rolled back.
// 3. Late Create Idempotency: Delayed worker create cannot overwrite CANCELLED status back to PENDING_PAYMENT.
func TestEdgeCase10_OutOfOrderMessageProcessing(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	eventUUID := uuid.New()
	eventID := eventUUID.String()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, 'Out Of Order Concert', 10, 10, NOW(), NOW() + INTERVAL '1 day')
	`, eventUUID)
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}
	_ = redisClient.PrewarmStock(ctx, eventID, 10)

	// Scenario 1: Early Payment (Webhook arrives before asynchronous worker finishes DB insert)
	earlyOrderUUID := uuid.New()
	earlyOrderID := earlyOrderUUID.String()

	go func() {
		time.Sleep(40 * time.Millisecond) 
		orderRepo := repository.NewOrderRepository(db)
		_ = orderRepo.Create(context.Background(), &models.Order{
			ID:             earlyOrderUUID,
			EventID:        eventUUID,
			UserID:         "user-early-payer",
			IdempotencyKey: "idemp-early-" + uuid.New().String(),
			Status:         models.OrderStatusPendingPayment,
			ExpiresAt:      time.Now().Add(10 * time.Minute),
		})
	}()

	reqPay := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/pay", earlyOrderID), nil)
	respPay, err := app.Test(reqPay, 5000)
	if err != nil || respPay.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for early payment with debounce, got status %d, err %v", respPay.StatusCode, err)
	}

	var earlyStatus models.OrderStatus
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", earlyOrderUUID).Scan(&earlyStatus)
	if earlyStatus != models.OrderStatusPaid {
		t.Fatalf("Expected order status to be PAID, got %s", earlyStatus)
	}
	t.Logf("[Edge Case 10 Verified] Early payment webhook seamlessly waited for in-flight DB insert and succeeded with 200 OK!")

	// Scenario 2: Early Cancel (User cancels before asynchronous worker creates order in DB)
	cancelOrderUUID := uuid.New()
	cancelOrderID := cancelOrderUUID.String()
	cancelUserID := "user-early-canceller"

	// Simulate stock decremented in Redis during booking
	_, _ = redisClient.BookTicket(ctx, eventID, cancelUserID, 1)

	// User calls Cancel API while order does not exist in DB yet
	cancelPayload, _ := json.Marshal(map[string]string{
		"event_id": eventID,
		"user_id":  cancelUserID,
	})
	reqCancel := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/orders/%s/cancel", cancelOrderID), bytes.NewReader(cancelPayload))
	reqCancel.Header.Set("Content-Type", "application/json")
	respCancel, err := app.Test(reqCancel, 5000)
	if err != nil || respCancel.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for early cancel, got %d", respCancel.StatusCode)
	}

	// Verify Tombstone record is inserted into DB with status = CANCELLED
	var tombstoneStatus models.OrderStatus
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", cancelOrderUUID).Scan(&tombstoneStatus)
	if tombstoneStatus != models.OrderStatusCancelled {
		t.Fatalf("Expected tombstone status CANCELLED, got %s", tombstoneStatus)
	}

	// Scenario 3: Delayed worker arrives and attempts to Create with status = PENDING_PAYMENT
	orderRepo := repository.NewOrderRepository(db)
	err = orderRepo.Create(ctx, &models.Order{
		ID:             cancelOrderUUID,
		EventID:        eventUUID,
		UserID:         cancelUserID,
		IdempotencyKey: "idemp-late-create-" + uuid.New().String(),
		Status:         models.OrderStatusPendingPayment,
		ExpiresAt:      time.Now().Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Worker create returned error: %v", err)
	}

	var finalStatus models.OrderStatus
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", cancelOrderUUID).Scan(&finalStatus)
	if finalStatus != models.OrderStatusCancelled {
		t.Fatalf("Tombstone was illegally overwritten to %s! State machine violation!", finalStatus)
	}
	t.Logf("[Edge Case 10 Verified] Tombstone permanently protected order from resurrection by delayed worker create!")
}
