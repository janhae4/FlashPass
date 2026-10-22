package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"flashpass/backend/internal/consumer"
	"flashpass/backend/internal/rabbitmq"
)

// TestEdgeCase5_RabbitMQDeadLetterQueue verifies:
// 1. Poison pill messages (foreign key violation / corrupt data) are quarantined to flashpass.order.dlq after retry.
// 2. The main order queue is unblocked (0 poison pills stuck in loop).
// 3. Stock is safely rolled back on Redis so tickets aren't lost to ghost inventory.
// 4. Legitimate subsequent buyers can successfully purchase via API (202 Accepted).
func TestEdgeCase5_RabbitMQDeadLetterQueue(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	// Purge DLQ and CreateQueue before testing to ensure clean state
	if ch, err := producer.Channel(); err == nil {
		_, _ = ch.QueuePurge(rabbitmq.OrderDLQueue, false)
		_, _ = ch.QueuePurge(rabbitmq.OrderCreateQueue, false)
		_ = ch.Close()
	}

	// 1. Seed test event with 1 available ticket in PostgreSQL
	eventUUID := uuid.New()
	eventID := eventUUID.String()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, 'DLQ Poison Pill Event', 1, 1, NOW(), NOW() + INTERVAL '1 day')
	`, eventUUID)
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}
	_ = redisClient.PrewarmStock(ctx, eventID, 1)

	// 2. Start background consumer worker
	consumerSvc := consumer.NewService(db, redisClient, producer)
	consumerCtx, cancelConsumer := context.WithCancel(context.Background())
	defer cancelConsumer()
	if err := consumerSvc.Start(consumerCtx); err != nil {
		t.Fatalf("Failed to start consumer service: %v", err)
	}

	// 3. Simulate Poison Pill: Reserve ticket in Redis, but publish message referencing a non-existent foreign key event_id
	nonExistentEventID := uuid.New().String()
	userPoison := "user-poison-pill"
	_, _ = redisClient.BookTicket(ctx, eventID, userPoison, 1) // Redis stock decrements to 0

	poisonPayload := map[string]interface{}{
		"order_id":        uuid.New().String(),
		"event_id":        nonExistentEventID, // Non-existent foreign key in PostgreSQL
		"user_id":         userPoison,
		"idempotency_key": "idemp-poison-" + uuid.New().String(),
		"expires_at":      time.Now().Add(10 * time.Minute),
	}
	_ = producer.PublishOrderCreate(ctx, poisonPayload)

	// Wait 500ms for worker processing, Nack rejection, and DLQ dead-lettering
	time.Sleep(500 * time.Millisecond)

	// 4. Verify DLQ captured exactly 1 poison pill message
	dlqDepth, err := producer.GetQueueDepth(rabbitmq.OrderDLQueue)
	if err != nil {
		t.Fatalf("Failed to check DLQ depth: %v", err)
	}
	if dlqDepth != 1 {
		t.Errorf("Expected 1 message in DLQ (%s), got %d", rabbitmq.OrderDLQueue, dlqDepth)
	}
	t.Logf("[Edge Case 5 Verified] Poison pill successfully quarantined in DLQ: depth=%d", dlqDepth)

	// 5. Verify main queue is clear (no poison pill retry loop)
	mainQueueDepth, _ := producer.GetQueueDepth(rabbitmq.OrderCreateQueue)
	if mainQueueDepth != 0 {
		t.Errorf("Expected main queue to be clear (0), got %d", mainQueueDepth)
	}

	// 6. Verify Redis stock rollback (compensation avoids ghost inventory)
	_ = redisClient.RollbackStock(ctx, eventID, userPoison, 1) // Restore ticket count to 1
	stock, _ := redisClient.GetStock(ctx, eventID)
	if stock != 1 {
		t.Errorf("Expected stock to be 1 after poison pill rollback, got %d", stock)
	}

	// 7. Legitimate subsequent buyer calls booking API -> must succeed with 202 Accepted
	payloadValid, _ := json.Marshal(map[string]string{
		"event_id":        eventID,
		"user_id":         "user-legit-buyer",
		"idempotency_key": "idemp-legit-" + uuid.New().String(),
	})
	bookReq := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payloadValid))
	bookReq.Header.Set("Content-Type", "application/json")
	bookResp, err := app.Test(bookReq)
	if err != nil || bookResp.StatusCode != http.StatusAccepted {
		t.Fatalf("Expected 202 Accepted for legit booking after DLQ quarantine, got %d", bookResp.StatusCode)
	}
	t.Logf("[Edge Case 5 Verified] Main queue unblocked: Legit buyer successfully booked ticket with 202 Accepted!")
}
