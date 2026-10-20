package consumer

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"flashpass/backend/internal/config"
	"flashpass/backend/internal/database"
	"flashpass/backend/internal/rabbitmq"
	"flashpass/backend/internal/redis"
)

func TestConsumerOrderCreationAndTimeoutRollback(t *testing.T) {
	cfg := config.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Connect DB, Redis, RabbitMQ
	db, err := database.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to connect DB: %v", err)
	}
	defer db.Close()

	redisClient, err := redis.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to connect Redis: %v", err)
	}
	defer redisClient.Close()

	producer, err := rabbitmq.NewProducer(cfg)
	if err != nil {
		t.Fatalf("Failed to create Producer: %v", err)
	}
	defer producer.Close()

	// 2. Start Consumer Service
	svc := NewService(db, redisClient, producer)
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Failed to start Consumer Service: %v", err)
	}

	// 3. Create test event in DB
	eventID := uuid.New()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, $2, $3, $4, NOW(), NOW() + INTERVAL '7 days')
	`, eventID, "Consumer Test Concert", 10, 10)
	if err != nil {
		t.Fatalf("Failed to insert event: %v", err)
	}
	defer func() {
		_, _ = db.Pool.Exec(ctx, "DELETE FROM events WHERE id = $1", eventID)
	}()

	// Prewarm stock in Redis: 10
	_ = redisClient.PrewarmStock(ctx, eventID.String(), 10)

	// 4. Simulate Booking: Decrement stock in Redis
	userID := "user-async-" + uuid.New().String()[:6]
	res, err := redisClient.BookTicket(ctx, eventID.String(), userID, 1)
	if err != nil || res != redis.ResultSuccess {
		t.Fatalf("BookTicket failed: res=%d, err=%v", res, err)
	}

	// Stock is now 9
	stock, _ := redisClient.GetStock(ctx, eventID.String())
	if stock != 9 {
		t.Fatalf("Expected stock 9, got %d", stock)
	}

	// 5. Publish order.create event with a short expiration (1.5 seconds) to test delayed expiration
	orderID := uuid.New()
	idempKey := "idemp-async-" + uuid.New().String()
	shortExpiresAt := time.Now().Add(1500 * time.Millisecond)

	orderEvent := map[string]interface{}{
		"order_id":        orderID.String(),
		"event_id":        eventID.String(),
		"user_id":         userID,
		"idempotency_key": idempKey,
		"expires_at":      shortExpiresAt,
	}

	if err := producer.PublishOrderCreate(ctx, orderEvent); err != nil {
		t.Fatalf("Failed to publish order.create: %v", err)
	}

	// 6. Verify order gets persisted in PostgreSQL within 2 seconds
	var persistedStatus string
	var found bool
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		err = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", orderID).Scan(&persistedStatus)
		if err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Order was not persisted to PostgreSQL by consumer worker")
	}
	if persistedStatus != "PENDING_PAYMENT" && persistedStatus != "EXPIRED" {
		t.Fatalf("Unexpected initial order status: %s", persistedStatus)
	}

	// 7. Wait for delayed message to trigger (expires in 1.5s -> wait up to 3s total)
	// Timeout worker should mark order EXPIRED and rollback Redis stock back to 10!
	var expired bool
	for i := 0; i < 30; i++ {
		time.Sleep(150 * time.Millisecond)
		var st string
		_ = db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", orderID).Scan(&st)
		if st == "EXPIRED" {
			expired = true
			break
		}
	}

	if !expired {
		t.Fatalf("Order was not marked EXPIRED by delayed timeout worker")
	}

	// 8. Verify Redis stock rolled back from 9 back to 10
	stock, _ = redisClient.GetStock(ctx, eventID.String())
	if stock != 10 {
		t.Errorf("Expected stock rolled back to 10, got %d", stock)
	}

	// 9. Verify user booking lock was cleared: User should be able to book again
	res, err = redisClient.BookTicket(ctx, eventID.String(), userID, 1)
	if err != nil || res != redis.ResultSuccess {
		t.Errorf("Expected user to be able to book after rollback, got res=%d, err=%v", res, err)
	}

	// Clean up order
	_, _ = db.Pool.Exec(ctx, "DELETE FROM orders WHERE id = $1", orderID)
}
