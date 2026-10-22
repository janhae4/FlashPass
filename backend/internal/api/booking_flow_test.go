package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestBookTicketFlow(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	// Seed event in DB & Redis with exactly 2 tickets
	eventID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, $2, $3, $4, NOW(), NOW() + INTERVAL '7 days')
	`, eventID, "Limited Ticket Sale", 2, 2)
	if err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}
	defer func() {
		_, _ = db.Pool.Exec(ctx, "DELETE FROM events WHERE id = $1", eventID)
	}()

	// Prewarm stock in Redis
	_ = redisClient.PrewarmStock(ctx, eventID.String(), 2)

	// 1. Validation test - Missing idempotency_key
	t.Run("Validation_MissingIdempotencyKey", func(t *testing.T) {
		badPayload, _ := json.Marshal(map[string]string{
			"event_id": eventID.String(),
			"user_id":  "user-100",
		})
		badReq := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(badPayload))
		badReq.Header.Set("Content-Type", "application/json")
		badResp, _ := app.Test(badReq)
		if badResp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 for missing idempotency_key, got %d", badResp.StatusCode)
		}
	})

	var res1OrderID string
	idempKey1 := "idemp-key-1-" + uuid.New().String()
	payload1, _ := json.Marshal(map[string]string{
		"event_id":        eventID.String(),
		"user_id":         "user-1",
		"idempotency_key": idempKey1,
	})

	// 2. Successful booking for User 1
	t.Run("Success_User1Booking", func(t *testing.T) {
		req1 := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload1))
		req1.Header.Set("Content-Type", "application/json")
		resp1, err := app.Test(req1)
		if err != nil {
			t.Fatalf("Booking 1 failed: %v", err)
		}
		if resp1.StatusCode != http.StatusAccepted {
			body, _ := io.ReadAll(resp1.Body)
			t.Fatalf("Expected 202 Accepted for booking 1, got %d. Body: %s", resp1.StatusCode, string(body))
		}

		var res1 struct {
			OrderID string `json:"order_id"`
			Status  string `json:"status"`
		}
		_ = json.NewDecoder(resp1.Body).Decode(&res1)
		if res1.OrderID == "" || res1.Status != "PENDING_PAYMENT" {
			t.Fatalf("Invalid reservation response: %+v", res1)
		}
		res1OrderID = res1.OrderID
	})

	// 3. Idempotency test - Same request with same idempotency_key returns cached response
	t.Run("Idempotency_CachedResponse", func(t *testing.T) {
		req1Duplicate := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload1))
		req1Duplicate.Header.Set("Content-Type", "application/json")
		resp1Dup, _ := app.Test(req1Duplicate)
		if resp1Dup.StatusCode != http.StatusAccepted {
			t.Errorf("Expected 202 Accepted for duplicate idempotency key, got %d", resp1Dup.StatusCode)
		}
		var res1Dup struct {
			OrderID string `json:"order_id"`
		}
		_ = json.NewDecoder(resp1Dup.Body).Decode(&res1Dup)
		if res1Dup.OrderID != res1OrderID {
			t.Errorf("Expected cached order ID %s, got %s", res1OrderID, res1Dup.OrderID)
		}
	})

	// 4. Duplicate booking test - User 1 tries to book AGAIN with a NEW idempotency key
	t.Run("Conflict_DuplicateUserBooking", func(t *testing.T) {
		idempKey1New := "idemp-key-new-" + uuid.New().String()
		payload1New, _ := json.Marshal(map[string]string{
			"event_id":        eventID.String(),
			"user_id":         "user-1",
			"idempotency_key": idempKey1New,
		})
		req1New := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload1New))
		req1New.Header.Set("Content-Type", "application/json")
		resp1New, _ := app.Test(req1New)
		if resp1New.StatusCode != http.StatusConflict {
			body, _ := io.ReadAll(resp1New.Body)
			t.Errorf("Expected 409 Conflict for duplicate user booking, got %d. Body: %s", resp1New.StatusCode, string(body))
		}
	})

	// 5. User 2 books the last ticket (stock = 1 -> 0)
	t.Run("Success_User2ExhaustStock", func(t *testing.T) {
		idempKey2 := "idemp-key-2-" + uuid.New().String()
		payload2, _ := json.Marshal(map[string]string{
			"event_id":        eventID.String(),
			"user_id":         "user-2",
			"idempotency_key": idempKey2,
		})
		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload2))
		req2.Header.Set("Content-Type", "application/json")
		resp2, _ := app.Test(req2)
		if resp2.StatusCode != http.StatusAccepted {
			t.Fatalf("Expected 202 Accepted for booking 2, got %d", resp2.StatusCode)
		}

		// Stock in Redis should now be 0
		stock, _ := redisClient.GetStock(ctx, eventID.String())
		if stock != 0 {
			t.Errorf("Expected stock 0, got %d", stock)
		}
	})

	// 6. User 3 tries to book when SOLD OUT (Fail-Fast: stock = 0)
	t.Run("FailFast_SoldOut", func(t *testing.T) {
		idempKey3 := "idemp-key-3-" + uuid.New().String()
		payload3, _ := json.Marshal(map[string]string{
			"event_id":        eventID.String(),
			"user_id":         "user-3",
			"idempotency_key": idempKey3,
		})
		req3 := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload3))
		req3.Header.Set("Content-Type", "application/json")
		resp3, _ := app.Test(req3)
		if resp3.StatusCode != http.StatusConflict {
			t.Errorf("Expected 409 Conflict for sold out, got %d", resp3.StatusCode)
		}

		var res3 struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp3.Body).Decode(&res3)
		if res3.Error != "Sold out" {
			t.Errorf("Expected error 'Sold out', got '%s'", res3.Error)
		}
	})
}
