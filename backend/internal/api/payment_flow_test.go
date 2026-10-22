package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestPayOrderFlow(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	// 1. Create test event and pending order in PostgreSQL
	eventID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, $2, $3, $4, NOW(), NOW() + INTERVAL '7 days')
	`, eventID, "Payment Test Event", 10, 10)
	if err != nil {
		t.Fatalf("Failed to create test event: %v", err)
	}
	defer func() {
		_, _ = db.Pool.Exec(ctx, "DELETE FROM events WHERE id = $1", eventID)
	}()

	orderID := uuid.New()
	idempKey := "idemp-pay-" + uuid.New().String()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at)
		VALUES ($1, $2, $3, $4, 'PENDING_PAYMENT', NOW() + INTERVAL '10 minutes')
	`, orderID, eventID, "user-pay-1", idempKey)
	if err != nil {
		t.Fatalf("Failed to create test order: %v", err)
	}
	defer func() {
		_, _ = db.Pool.Exec(ctx, "DELETE FROM orders WHERE id = $1", orderID)
	}()

	// 2. Pay pending order -> 200 OK & status updated to PAID
	t.Run("Success_PayPendingOrder", func(t *testing.T) {
		payReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID.String()+"/pay", nil)
		payResp, err := app.Test(payReq)
		if err != nil {
			t.Fatalf("Pay request failed: %v", err)
		}
		if payResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(payResp.Body)
			t.Fatalf("Expected 200 OK for payment, got %d. Body: %s", payResp.StatusCode, string(body))
		}

		var payResult struct {
			OrderID string `json:"order_id"`
			Status  string `json:"status"`
		}
		_ = json.NewDecoder(payResp.Body).Decode(&payResult)
		if payResult.Status != "PAID" {
			t.Errorf("Expected status PAID, got %s", payResult.Status)
		}

		// Verify status in PostgreSQL is PAID
		var dbStatus string
		_ = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", orderID).Scan(&dbStatus)
		if dbStatus != "PAID" {
			t.Errorf("Expected DB status PAID, got %s", dbStatus)
		}
	})

	// 3. Pay already paid order -> 200 OK
	t.Run("Idempotent_AlreadyPaid", func(t *testing.T) {
		payAgainResp, _ := app.Test(httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID.String()+"/pay", nil))
		if payAgainResp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK for already paid order, got %d", payAgainResp.StatusCode)
		}
	})

	// 4. Pay expired order -> 202 Accepted & triggers Auto-Refund (status: REFUND_PENDING)
	t.Run("AutoRefund_ExpiredOrder", func(t *testing.T) {
		expiredOrderID := uuid.New()
		_, _ = db.Pool.Exec(ctx, `
			INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at)
			VALUES ($1, $2, $3, $4, 'EXPIRED', NOW() - INTERVAL '1 minute')
		`, expiredOrderID, eventID, "user-pay-expired", "idemp-pay-expired-"+uuid.New().String())
		defer func() {
			_, _ = db.Pool.Exec(ctx, "DELETE FROM orders WHERE id = $1", expiredOrderID)
		}()

		expiredReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+expiredOrderID.String()+"/pay", nil)
		expiredResp, _ := app.Test(expiredReq)
		if expiredResp.StatusCode != http.StatusAccepted {
			t.Errorf("Expected 202 Accepted for expired order payment (Auto-Refund), got %d", expiredResp.StatusCode)
		}
		var expiredData struct {
			Status string `json:"status"`
		}
		_ = json.NewDecoder(expiredResp.Body).Decode(&expiredData)
		if expiredData.Status != "REFUND_PENDING" {
			t.Errorf("Expected status REFUND_PENDING, got %s", expiredData.Status)
		}
	})

	// 5. Pay non-existent order -> 404 Not Found
	t.Run("NotFound_MissingOrder", func(t *testing.T) {
		notFoundReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+uuid.New().String()+"/pay", nil)
		notFoundResp, _ := app.Test(notFoundReq)
		if notFoundResp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected 404 Not Found for missing order, got %d", notFoundResp.StatusCode)
		}
	})
}
