package consumer

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"flashpass/backend/internal/database"
	"flashpass/backend/internal/models"
	"flashpass/backend/internal/rabbitmq"
	"flashpass/backend/internal/redis"
	"flashpass/backend/internal/repository"
	"flashpass/backend/internal/service"

	"github.com/google/uuid"
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
	db               *database.DB
	redis            redis.StockCache
	producer         rabbitmq.EventProducer
	eventRepo        repository.EventRepository
	orderRepo        repository.OrderRepository
	refundRepo       repository.RefundRepository
	reconcileService service.ReconcileService
	wg               sync.WaitGroup
}

func NewService(db *database.DB, redisClient redis.StockCache, producer rabbitmq.EventProducer) *Service {
	eventRepo := repository.NewEventRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	refundRepo := repository.NewRefundRepository(db)
	reconcileSvc := service.NewReconcileService(eventRepo, redisClient)

	return &Service{
		db:               db,
		redis:            redisClient,
		producer:         producer,
		eventRepo:        eventRepo,
		orderRepo:        orderRepo,
		refundRepo:       refundRepo,
		reconcileService: reconcileSvc,
	}
}

// Start launches both Order Creation Worker, Delayed Timeout Worker, Auto-Refund, and Schedulers
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

	chRefund, err := s.producer.Channel()
	if err != nil {
		return err
	}
	_ = chRefund.Qos(100, 0, false)

	msgsRefund, err := chRefund.Consume(
		rabbitmq.OrderRefundQueue,
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

	log.Println("[Consumer] Background workers started: Order Creation, Delayed Timeout, Auto-Refund & Retry Scheduler listening.")

	s.wg.Add(5)
	go func() {
		defer s.wg.Done()
		s.runOrderCreateWorker(ctx, msgsCreate)
	}()
	go func() {
		defer s.wg.Done()
		s.runTimeoutWorker(ctx, msgsTimeout)
	}()
	go func() {
		defer s.wg.Done()
		s.runRefundWorker(ctx, msgsRefund)
	}()
	go func() {
		defer s.wg.Done()
		s.runRefundRetryScheduler(ctx)
	}()
	go func() {
		defer s.wg.Done()
		s.runReconcileStockScheduler(ctx)
	}()

	return nil
}

// Stop waits for all running worker goroutines to complete gracefully
func (s *Service) Stop() {
	s.wg.Wait()
	log.Println("[Consumer] All workers drained and stopped cleanly.")
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

	order := &models.Order{
		ID:             orderUUID,
		EventID:        eventUUID,
		UserID:         msg.UserID,
		IdempotencyKey: msg.IdempotencyKey,
		Status:         models.OrderStatusPendingPayment,
		ExpiresAt:      msg.ExpiresAt,
	}

	if err := s.orderRepo.Create(ctx, order); err != nil {
		log.Printf("[Order Worker ERROR] Failed to persist order in PostgreSQL: %v", err)
		// Only requeue once to prevent poison-pill death loops on permanent data violation
		_ = d.Nack(false, !d.Redelivered)
		return
	}

	delay := time.Until(msg.ExpiresAt)
	if delay <= 0 {
		delay = 10*time.Minute + 30*time.Second
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
	eventUUID, err := uuid.Parse(msg.EventID)
	if err != nil {
		_ = d.Nack(false, false)
		return
	}

	order, err := s.orderRepo.GetByID(ctx, orderUUID)
	if err != nil {
		log.Printf("[Timeout Worker ERROR] Order %s lookup failed: %v", msg.OrderID, err)
		_ = d.Nack(false, false)
		return
	}

	if order.Status == models.OrderStatusPendingPayment {
		updated, err := s.orderRepo.ExpireIfPastDeadline(ctx, orderUUID)
		if err != nil {
			log.Printf("[Timeout Worker ERROR] Failed to update order status to EXPIRED: %v", err)
			_ = d.Nack(false, true)
			return
		}
		if !updated {
			// Clock Skew Guard: If expires_at on DB is still in the future, do not expire prematurely!
			log.Printf("[Timeout Worker] Order %s expires_at is not past DB NOW() yet (Clock Skew detected). Skipping premature expiration.", msg.OrderID)
			_ = d.Ack(false)
			return
		}

		if err := s.redis.RollbackStock(ctx, msg.EventID, msg.UserID, 1); err != nil {
			log.Printf("[Timeout Worker ERROR] Redis stock rollback failed: %v", err)
		} else {
			_ = s.eventRepo.IncrementAvailableTickets(ctx, eventUUID, 1)
			log.Printf("[Timeout Worker] Order %s EXPIRED! Stock returned to event %s, user %s unblocked.",
				msg.OrderID, msg.EventID, msg.UserID)
		}
	} else {
		log.Printf("[Timeout Worker] Order %s already resolved with status: %s (no rollback needed)",
			msg.OrderID, order.Status)
	}

	_ = d.Ack(false)
}

func (s *Service) runRefundWorker(ctx context.Context, msgs <-chan rabbitmq.Delivery) {
	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-msgs:
			if !ok {
				return
			}
			s.handleOrderRefund(ctx, d)
		}
	}
}

func (s *Service) handleOrderRefund(ctx context.Context, d rabbitmq.Delivery) {
	var msg struct {
		RefundID string `json:"refund_id"`
		OrderID  string `json:"order_id"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(d.Body, &msg); err != nil {
		log.Printf("[Refund Worker ERROR] Invalid refund message: %v", err)
		_ = d.Nack(false, false)
		return
	}

	orderUUID, err := uuid.Parse(msg.OrderID)
	if err != nil {
		log.Printf("[Refund Worker ERROR] Invalid order UUID %s: %v", msg.OrderID, err)
		_ = d.Nack(false, false)
		return
	}

	log.Printf("[Refund Worker] Processing automatic refund for order %s (reason: %s)...", msg.OrderID, msg.Reason)

	updated, err := s.refundRepo.UpdateRefundSuccess(ctx, orderUUID)
	if err != nil {
		log.Printf("[Refund Worker ERROR] Failed to update order %s to REFUNDED: %v", msg.OrderID, err)
		_ = s.refundRepo.MarkRetry(ctx, orderUUID, err.Error())
		_ = d.Nack(false, true)
		return
	}

	if !updated {
		log.Printf("[Refund Worker WARNING] No matching order %s found to refund", msg.OrderID)
	}

	_ = d.Ack(false)
	log.Printf("[Refund Worker SUCCESS] Order %s successfully refunded and transitioned to REFUNDED.", msg.OrderID)
}

// runRefundRetryScheduler runs as a background cron ticker (every 30s)
// scanning for PENDING refund orders due for retry and re-dispatching them to RabbitMQ
func (s *Service) runRefundRetryScheduler(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Println("[Refund Scheduler] Background retry cron started (polling every 30s).")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scanAndRetryPendingRefunds(ctx)
		}
	}
}

func (s *Service) scanAndRetryPendingRefunds(ctx context.Context) {
	refunds, err := s.refundRepo.GetPendingForRetry(ctx, 50)
	if err != nil {
		log.Printf("[Refund Scheduler ERROR] Failed to scan pending refunds: %v", err)
		return
	}

	var count int
	for _, r := range refunds {
		payload := map[string]interface{}{
			"refund_id": r.ID.String(),
			"order_id":  r.OrderID.String(),
			"reason":    r.Reason,
		}
		if err := s.producer.PublishOrderRefund(ctx, payload); err != nil {
			log.Printf("[Refund Scheduler ERROR] Failed to republish refund %s: %v", r.ID, err)
			continue
		}
		count++
	}

	if count > 0 {
		log.Printf("[Refund Scheduler] Successfully republished %d pending refund(s) to RabbitMQ queue.", count)
	}
}

func (s *Service) runReconcileStockScheduler(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	suspected := make(map[string]int)
	log.Println("[Reconcile Scheduler] Background reconcile cron started (polling every 1 minute).")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileStock(ctx, suspected)
		}
	}
}

func (s *Service) reconcileStock(ctx context.Context, suspected map[string]int) {
	queueDepth, err := s.producer.GetQueueDepth(rabbitmq.OrderCreateQueue)
	if err != nil {
		log.Printf("[Reconcile Scheduler ERROR] Failed to check RabbitMQ queue depth: %v", err)
		queueDepth = -1
	}

	if err := s.reconcileService.ReconcileAll(ctx, queueDepth, suspected); err != nil {
		log.Printf("[Reconcile Scheduler ERROR] ReconcileAll failed: %v", err)
	}
}
