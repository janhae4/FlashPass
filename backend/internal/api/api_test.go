package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"flashpass/backend/internal/config"
	"flashpass/backend/internal/database"
	"flashpass/backend/internal/rabbitmq"
	"flashpass/backend/internal/redis"
)

func setupTestApp(t *testing.T) (*fiber.App, *database.DB, *redis.Client, *rabbitmq.Producer) {
	cfg := config.Load()
	ctx := context.Background()

	db, err := database.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	migrator := database.NewMigrator(db)
	if err := migrator.Up(ctx); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	redisClient, err := redis.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to connect to Redis: %v", err)
	}

	producer, err := rabbitmq.NewProducer(cfg)
	if err != nil {
		t.Fatalf("Failed to create RabbitMQ producer: %v", err)
	}

	app := fiber.New()
	handler := NewHandler(db, redisClient, producer)
	SetupRouter(app, handler)

	return app, db, redisClient, producer
}

func TestHealthCheck(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Health check request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestAdminPrewarmAndStockInspection(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	// Create test event in DB with 50 tickets
	eventID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, $2, $3, $4, NOW(), NOW() + INTERVAL '7 days')
	`, eventID, "Prewarm Test Concert", 50, 50)
	if err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}
	defer func() {
		_, _ = db.Pool.Exec(ctx, "DELETE FROM events WHERE id = $1", eventID)
	}()

	// 1. Call Pre-warm Endpoint
	prewarmReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/events/"+eventID.String()+"/prewarm", nil)
	prewarmResp, err := app.Test(prewarmReq)
	if err != nil {
		t.Fatalf("Prewarm request failed: %v", err)
	}
	if prewarmResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(prewarmResp.Body)
		t.Fatalf("Expected prewarm status 200, got %d. Body: %s", prewarmResp.StatusCode, string(body))
	}

	// 2. Call Stock Endpoint
	stockReq := httptest.NewRequest(http.MethodGet, "/api/v1/events/"+eventID.String()+"/stock", nil)
	stockResp, err := app.Test(stockReq)
	if err != nil {
		t.Fatalf("Get stock request failed: %v", err)
	}
	if stockResp.StatusCode != http.StatusOK {
		t.Fatalf("Expected stock status 200, got %d", stockResp.StatusCode)
	}

	var stockData struct {
		EventID string `json:"event_id"`
		Stock   int    `json:"stock"`
	}
	_ = json.NewDecoder(stockResp.Body).Decode(&stockData)
	if stockData.Stock != 50 {
		t.Errorf("Expected stock 50, got %d", stockData.Stock)
	}
}

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

	// 2. Successful booking for User 1
	idempKey1 := "idemp-key-1-" + uuid.New().String()
	payload1, _ := json.Marshal(map[string]string{
		"event_id":        eventID.String(),
		"user_id":         "user-1",
		"idempotency_key": idempKey1,
	})
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

	// 3. Idempotency test - Same request with same idempotency_key returns cached response
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
	if res1Dup.OrderID != res1.OrderID {
		t.Errorf("Expected cached order ID %s, got %s", res1.OrderID, res1Dup.OrderID)
	}

	// 4. Duplicate booking test - User 1 tries to book AGAIN with a NEW idempotency key
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

	// 5. User 2 books the last ticket (stock = 1 -> 0)
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

	// 6. User 3 tries to book when SOLD OUT (Fail-Fast: stock = 0)
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
}
