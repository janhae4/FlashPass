package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"flashpass/backend/internal/models"
	"flashpass/backend/internal/rabbitmq"
	"flashpass/backend/internal/redis"
	"flashpass/backend/internal/repository"
)

var (
	ErrUserAlreadyBooked = errors.New("user already booked a ticket for this event")
	ErrSoldOut           = errors.New("sold out")
	ErrQueueFailed       = errors.New("failed to queue order reservation")
	ErrUnknownResponse   = errors.New("unknown reservation response")
)

type BookTicketRequest struct {
	EventID        string `json:"event_id"`
	UserID         string `json:"user_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type BookingResult struct {
	OrderID   string             `json:"order_id"`
	EventID   string             `json:"event_id"`
	UserID    string             `json:"user_id"`
	Status    models.OrderStatus `json:"status"`
	ExpiresAt string             `json:"expires_at,omitempty"`
	Message   string             `json:"message,omitempty"`
	IsCached  bool               `json:"is_cached,omitempty"`
}

type BookingService interface {
	BookTicket(ctx context.Context, req BookTicketRequest) (*BookingResult, error)
}

type bookingService struct {
	stockCache       redis.StockCache
	eventRepo        repository.EventRepository
	producer         rabbitmq.EventProducer
	reconcileService ReconcileService
}

func NewBookingService(
	stockCache redis.StockCache,
	eventRepo repository.EventRepository,
	producer rabbitmq.EventProducer,
	reconcileService ReconcileService,
) BookingService {
	return &bookingService{
		stockCache:       stockCache,
		eventRepo:        eventRepo,
		producer:         producer,
		reconcileService: reconcileService,
	}
}

func (s *bookingService) BookTicket(ctx context.Context, req BookTicketRequest) (*BookingResult, error) {
	eventUUID, err := uuid.Parse(req.EventID)
	if err != nil {
		return nil, fmt.Errorf("invalid event UUID: %w", err)
	}	

	// 1. Check Idempotency Key in Redis Cache
	cachedOrderID, exists, err := s.stockCache.CheckIdempotency(ctx, req.IdempotencyKey)
	if err == nil && exists {
		return &BookingResult{
			OrderID:  cachedOrderID,
			EventID:  req.EventID,
			UserID:   req.UserID,
			Status:   models.OrderStatusPendingPayment,
			Message:  "Duplicate request: returning existing reservation",
			IsCached: true,
		}, nil
	}

	// 2. Execute Atomic Redis Lua Script
	result, err := s.stockCache.BookTicket(ctx, req.EventID, req.UserID, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to execute stock reservation: %w", err)
	}

	switch result {
	case redis.ResultAlreadyBooked:
		return nil, ErrUserAlreadyBooked
	case redis.ResultSoldOut:
		s.reconcileService.TriggerEmergencyAudit(req.EventID)
		return nil, ErrSoldOut
	case redis.ResultSuccess, redis.ResultSuccessHitZero:
		return s.completeBooking(ctx, req, eventUUID, result)
	default:
		return nil, ErrUnknownResponse
	}
}

func (s *bookingService) completeBooking(ctx context.Context, req BookTicketRequest, eventUUID uuid.UUID, result int64) (*BookingResult, error) {
	if result == redis.ResultSuccessHitZero {
		s.reconcileService.TriggerEmergencyAudit(req.EventID)
	}

	// Database Invariant Guard
	decremented, err := s.eventRepo.DecrementAvailableTickets(ctx, eventUUID)
	if err != nil || !decremented {
		_ = s.stockCache.RollbackStock(ctx, req.EventID, req.UserID, 1)
		return nil, ErrSoldOut
	}

	orderID := uuid.New().String()
	expiresAt := time.Now().Add(10*time.Minute + 30*time.Second)

	_ = s.stockCache.SetIdempotency(ctx, req.IdempotencyKey, orderID, 30*time.Second)

	orderEvent := map[string]interface{}{
		"order_id":        orderID,
		"event_id":        req.EventID,
		"user_id":         req.UserID,
		"idempotency_key": req.IdempotencyKey,
		"expires_at":      expiresAt,
	}

	if err := s.producer.PublishOrderCreate(ctx, orderEvent); err != nil {
		_ = s.stockCache.RollbackStock(ctx, req.EventID, req.UserID, 1)
		_ = s.eventRepo.IncrementAvailableTickets(ctx, eventUUID, 1)
		return nil, ErrQueueFailed
	}

	return &BookingResult{
		OrderID:   orderID,
		EventID:   req.EventID,
		UserID:    req.UserID,
		Status:    models.OrderStatusPendingPayment,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	}, nil
}
