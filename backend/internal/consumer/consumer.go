package consumer

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"
	"flashpass/backend/internal/database"
	"flashpass/backend/internal/rabbitmq"
	"flashpass/backend/internal/redis"
)

type OrderCreateMsg struct {
	OrderID        string    `json:"order_id"`
	EventID        string    `json:"event_id"`
	UserID         string    `json:"user_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type OrderTimeoutMsg struct {
	OrderID string `json:"order_id"`
	EventID string `json:"event_id"`
	UserID  string `json:"user_id"`
}

type Service struct {
	db       *database.DB
	redis    *redis.Client
	producer *rabbitmq.Producer
}

func NewService(db *database.DB, redis *redis.Client, producer *rabbitmq.Producer) *Service {
	return &Service{
		db:       db,
		redis:    redis,
		producer: producer,
	}
}

// Start launches both Order Creation Worker and Delayed Timeout Worker
func (s *Service) Start(ctx context.Context) error {
	ch, err := s.producer.Channel()
	if err != nil {
		return err
	}
	_ = ch.Qos(100, 0, false)

	// 1. Order Creation Consumer
	msgsCreate, err := ch.Consume(
		rabbitmq.OrderCreateQueue,
		"",    // consumer tag
		false, // auto-ack (manual ack for at-least-once guarantee)
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// 2. Order Timeout Consumer
	chTimeout, err := s.producer.Channel()
	if err != nil {
		return err
	}
	_ = chTimeout.Qos(100, 0, false)

	msgsTimeout, err := chTimeout.Consume(
		rabbitmq.OrderTimeoutQueue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	log.Println("[Consumer] Background workers started: Order Creation & Delayed Timeout consumers listening.")

	go s.runOrderCreateWorker(ctx, msgsCreate)
	go s.runTimeoutWorker(ctx, msgsTimeout)

	return nil
}

func (s *Service) runOrderCreateWorker(ctx context.Context, msgs <-chan rabbitmq.Delivery) {
	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-msgs:
			if !ok {
				return
			}
			s.handleOrderCreate(ctx, d)
		}
	}
}

func (s *Service) handleOrderCreate(ctx context.Context, d rabbitmq.Delivery) {
	var msg OrderCreateMsg
	if err := json.Unmarshal(d.Body, &msg); err != nil {
		log.Printf("[Order Worker ERROR] Invalid message format: %v", err)
		_ = d.Nack(false, false)
		return
	}

	orderUUID, err := uuid.Parse(msg.OrderID)
	if err != nil {
		_ = d.Nack(false, false)
		return
	}
	eventUUID, err := uuid.Parse(msg.EventID)
	if err != nil {
		_ = d.Nack(false, false)
		return
	}

	// Persist order in PostgreSQL with status PENDING_PAYMENT
	query := `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'PENDING_PAYMENT', $5, NOW(), NOW())
		ON CONFLICT (idempotency_key) DO NOTHING;
	`
	_, err = s.db.Pool.Exec(ctx, query, orderUUID, eventUUID, msg.UserID, msg.IdempotencyKey, msg.ExpiresAt)
	if err != nil {
		log.Printf("[Order Worker ERROR] Failed to persist order in PostgreSQL: %v", err)
		// Only requeue once to prevent poison-pill death loops on permanent data violation
		_ = d.Nack(false, !d.Redelivered)
		return
	}

	// Schedule delayed message for 10-minute timeout check
	delay := time.Until(msg.ExpiresAt)
	if delay <= 0 {
		delay = 10 * time.Minute
	}
	timeoutPayload := OrderTimeoutMsg{
		OrderID: msg.OrderID,
		EventID: msg.EventID,
		UserID:  msg.UserID,
	}

	if err := s.producer.PublishDelayedOrderTimeout(ctx, timeoutPayload, delay.Milliseconds()); err != nil {
		log.Printf("[Order Worker ERROR] Failed to publish delayed message: %v", err)
		_ = d.Nack(false, true)
		return
	}

	_ = d.Ack(false)
	log.Printf("[Order Worker] Order %s persisted (user=%s, event=%s). Scheduled timeout in %v",
		msg.OrderID, msg.UserID, msg.EventID, delay)
}

func (s *Service) runTimeoutWorker(ctx context.Context, msgs <-chan rabbitmq.Delivery) {
	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-msgs:
			if !ok {
				return
			}
			s.handleOrderTimeout(ctx, d)
		}
	}
}

func (s *Service) handleOrderTimeout(ctx context.Context, d rabbitmq.Delivery) {
	var msg OrderTimeoutMsg
	if err := json.Unmarshal(d.Body, &msg); err != nil {
		log.Printf("[Timeout Worker ERROR] Invalid timeout message format: %v", err)
		_ = d.Nack(false, false)
		return
	}

	orderUUID, err := uuid.Parse(msg.OrderID)
	if err != nil {
		_ = d.Nack(false, false)
		return
	}

	// Check current status in PostgreSQL
	var status string
	err = s.db.Pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", orderUUID).Scan(&status)
	if err != nil {
		log.Printf("[Timeout Worker ERROR] Order %s lookup failed: %v", msg.OrderID, err)
		_ = d.Nack(false, false)
		return
	}

	if status == "PENDING_PAYMENT" {
		// Mark order as EXPIRED
		_, err := s.db.Pool.Exec(ctx, `
			UPDATE orders SET status = 'EXPIRED', updated_at = NOW()
			WHERE id = $1 AND status = 'PENDING_PAYMENT'
		`, orderUUID)
		if err != nil {
			log.Printf("[Timeout Worker ERROR] Failed to update order status to EXPIRED: %v", err)
			_ = d.Nack(false, true)
			return
		}

		// Rollback Redis stock & clear user booked flag atomically via Lua script
		if err := s.redis.RollbackStock(ctx, msg.EventID, msg.UserID, 1); err != nil {
			log.Printf("[Timeout Worker ERROR] Redis stock rollback failed: %v", err)
		} else {
			log.Printf("[Timeout Worker] Order %s EXPIRED! Stock returned to event %s, user %s unblocked.",
				msg.OrderID, msg.EventID, msg.UserID)
		}
	} else {
		log.Printf("[Timeout Worker] Order %s already resolved with status: %s (no rollback needed)",
			msg.OrderID, status)
	}

	_ = d.Ack(false)
}
