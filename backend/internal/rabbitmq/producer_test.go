package rabbitmq

import (
	"context"
	"testing"
	"time"

	"flashpass/backend/internal/config"
)

func TestRabbitMQProducer(t *testing.T) {
	cfg := config.Load()
	producer, err := NewProducer(cfg)
	if err != nil {
		t.Fatalf("Failed to create RabbitMQ producer: %v", err)
	}
	defer producer.Close()

	payload := map[string]interface{}{
		"order_id":        "test-order-1",
		"event_id":        "test-event-1",
		"user_id":         "test-user-1",
		"idempotency_key": "test-key-1",
		"expires_at":      time.Now().Add(10 * time.Minute),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := producer.PublishOrderCreate(ctx, payload); err != nil {
		t.Fatalf("Failed to publish order.create event: %v", err)
	}

	// Purge test message from queue
	ch, err := producer.Channel()
	if err == nil {
		_, _ = ch.QueuePurge(OrderCreateQueue, false)
		_ = ch.Close()
	}
}
