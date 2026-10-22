package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"flashpass/backend/internal/models"
	"flashpass/backend/internal/rabbitmq"
	"flashpass/backend/internal/redis"
	"flashpass/backend/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrOrderNotFound      = errors.New("order not found")
	ErrEventNotFound      = errors.New("event not found")
	ErrInvalidOrderStatus = errors.New("order cannot be paid in current status")
	ErrUserHasActiveOrder = errors.New("user already has an active or paid order, cannot unlock")
)

// ClockDriftGraceWindow allows boundary payment submissions (within 5s) to tolerate network transit delays and NTP clock skew.
const ClockDriftGraceWindow = 5 * time.Second

type PayOrderResult struct {
	OrderID string             `json:"order_id"`
	Status  models.OrderStatus `json:"status"`
	Message string             `json:"message"`
}

type EventMetrics struct {
	EventID       string `json:"event_id"`
	Stock         int    `json:"stock"`
	PendingOrders int    `json:"pending_orders"`
	PaidOrders    int    `json:"paid_orders"`
	ExpiredOrders int    `json:"expired_orders"`
	TotalOrders   int    `json:"total_orders"`
}

type EventDetails struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	TotalTickets     int       `json:"total_tickets"`
	AvailableTickets int       `json:"available_tickets"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time"`
}

type OrderService interface {
	PayOrder(ctx context.Context, orderID uuid.UUID) (*PayOrderResult, error)
	GetEventMetrics(ctx context.Context, eventID uuid.UUID) (*EventMetrics, error)
	GetEventDetails(ctx context.Context, eventID uuid.UUID) (*EventDetails, error)
	PrewarmEvent(ctx context.Context, eventID uuid.UUID) (title string, stock int, err error)
	ResetEvent(ctx context.Context, eventID uuid.UUID, customTickets int) (int, error)
	UnlockUser(ctx context.Context, eventID string, userID string) error
	CancelOrder(ctx context.Context, orderID, eventID uuid.UUID, userID string) error
}

type orderService struct {
	orderRepo  repository.OrderRepository
	eventRepo  repository.EventRepository
	refundRepo repository.RefundRepository
	stockCache redis.StockCache
	producer   rabbitmq.EventProducer
}

func NewOrderService(
	orderRepo repository.OrderRepository,
	eventRepo repository.EventRepository,
	refundRepo repository.RefundRepository,
	stockCache redis.StockCache,
	producer rabbitmq.EventProducer,
) OrderService {
	return &orderService{
		orderRepo:  orderRepo,
		eventRepo:  eventRepo,
		refundRepo: refundRepo,
		stockCache: stockCache,
		producer:   producer,
	}
}

func (s *orderService) PayOrder(ctx context.Context, orderID uuid.UUID) (*PayOrderResult, error) {
	orderIDStr := orderID.String()
	lockKey := redis.PayLockKey(orderIDStr)

	accquired, _ := s.stockCache.AcquireLock(ctx, lockKey, 5*time.Second)
	if !accquired {
		time.Sleep(150 * time.Millisecond)
	} else {
		defer func() { _ = s.stockCache.ReleaseLock(ctx, lockKey) }()
	}

	order, err := s.orderRepo.GetByID(ctx, orderID)
	if err != nil {
		time.Sleep(100 * time.Millisecond)
		order, err = s.orderRepo.GetByID(ctx, orderID)
		if err != nil {
			return nil, ErrOrderNotFound
		}
	}

	if order.Status == models.OrderStatusPaid {
		return &PayOrderResult{
			OrderID: orderIDStr,
			Status:  models.OrderStatusPaid,
			Message: "Order already paid",
		}, nil
	}

	if order.Status == models.OrderStatusRefundPending || order.Status == models.OrderStatusRefunded {
		return &PayOrderResult{
			OrderID: orderIDStr,
			Status:  order.Status,
			Message: "Order has expired and refund is already being processed",
		}, nil
	}

	if order.Status == models.OrderStatusExpired || time.Now().After(order.ExpiresAt.Add(ClockDriftGraceWindow)) {
		refundID := uuid.New()
		idempKey := "refund-" + orderIDStr

		if err := s.refundRepo.CreateLatePaymentRefund(ctx, orderID, refundID, idempKey, 500000); err != nil {
			log.Printf("[PayOrder ERROR] Failed to record refund for order %s: %v", orderIDStr, err)
		}

		refundPayload := map[string]interface{}{
			"refund_id": refundID.String(),
			"order_id":  orderIDStr,
			"reason":    "late_payment_after_expiration",
			"timestamp": time.Now().UTC(),
		}
		if err := s.producer.PublishOrderRefund(ctx, refundPayload); err != nil {
			log.Printf("[PayOrder ERROR] Failed to publish order.refund for order %s: %v", orderIDStr, err)
		}

		return &PayOrderResult{
			OrderID: orderIDStr,
			Status:  models.OrderStatusRefundPending,
			Message: "Order has expired and ticket was released. Auto-refund initiated.",
		}, nil
	}

	if order.Status != models.OrderStatusPendingPayment {
		return nil, fmt.Errorf("%w: %s", ErrInvalidOrderStatus, order.Status)
	}

	updated, err := s.orderRepo.UpdateStatus(ctx, orderID, models.OrderStatusPendingPayment, models.OrderStatusPaid)
	if err != nil || !updated {
		recheckOrder, recheckErr := s.orderRepo.GetByID(ctx, orderID)
		if recheckErr == nil && recheckOrder.Status == models.OrderStatusPaid {
			return &PayOrderResult{
				OrderID: orderIDStr,
				Status:  models.OrderStatusPaid,
				Message: "Order already paid",
			}, nil
		}
		return nil, fmt.Errorf("failed to update order payment status: %w", err)
	}

	return &PayOrderResult{
		OrderID: orderIDStr,
		Status:  models.OrderStatusPaid,
		Message: "Payment successful",
	}, nil
}

func (s *orderService) GetEventMetrics(ctx context.Context, eventID uuid.UUID) (*EventMetrics, error) {
	eventIDStr := eventID.String()
	stock, _ := s.stockCache.GetStock(ctx, eventIDStr)

	pending, paid, expired, total, err := s.orderRepo.GetMetricsByEventID(ctx, eventID)
	if err != nil {
		return nil, err
	}

	return &EventMetrics{
		EventID:       eventIDStr,
		Stock:         stock,
		PendingOrders: pending,
		PaidOrders:    paid,
		ExpiredOrders: expired,
		TotalOrders:   total,
	}, nil
}

func (s *orderService) GetEventDetails(ctx context.Context, eventID uuid.UUID) (*EventDetails, error) {
	event, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		return nil, ErrEventNotFound
	}

	available := event.AvailableTickets
	if liveStock, err := s.stockCache.GetStock(ctx, eventID.String()); err == nil {
		available = liveStock
	}

	return &EventDetails{
		ID:               event.ID.String(),
		Title:            event.Title,
		TotalTickets:     event.TotalTickets,
		AvailableTickets: available,
		StartTime:        event.StartTime,
		EndTime:          event.EndTime,
	}, nil
}

func (s *orderService) PrewarmEvent(ctx context.Context, eventID uuid.UUID) (string, int, error) {
	event, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		return "", 0, ErrEventNotFound
	}

	if err := s.stockCache.PrewarmStock(ctx, eventID.String(), event.AvailableTickets); err != nil {
		return "", 0, fmt.Errorf("failed to prewarm Redis cache: %w", err)
	}

	return event.Title, event.AvailableTickets, nil
}

func (s *orderService) ResetEvent(ctx context.Context, eventID uuid.UUID, customTickets int) (int, error) {
	event, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		return 0, ErrEventNotFound
	}

	totalTickets := event.TotalTickets
	if customTickets > 0 {
		totalTickets = customTickets
	}

	if err := s.eventRepo.ResetEvent(ctx, eventID, totalTickets); err != nil {
		return 0, fmt.Errorf("failed to reset event in database: %w", err)
	}

	if err := s.stockCache.ResetEvent(ctx, eventID.String(), totalTickets); err != nil {
		return 0, fmt.Errorf("failed to reset event in Redis: %w", err)
	}

	return totalTickets, nil
}

func (s *orderService) UnlockUser(ctx context.Context, eventID string, userID string) error {
	eventUUID, err := uuid.Parse(eventID)
	if err != nil {
		return fmt.Errorf("invalid event UUID: %w", err)
	}

	activeCount, err := s.orderRepo.CountActiveOrdersByUser(ctx, eventUUID, userID)
	if err != nil {
		return fmt.Errorf("failed to check active orders: %w", err)
	}

	if activeCount > 0 {
		return ErrUserHasActiveOrder
	}

	return s.stockCache.ClearUserBooked(ctx, eventID, userID)
}

func (s *orderService) CancelOrder(ctx context.Context, orderID, eventID uuid.UUID, userID string) error {
	order, err := s.orderRepo.GetByID(ctx, orderID)
	if err != nil {
		idempKey := "tombstone-" + orderID.String()
		if err := s.orderRepo.CreateTombstone(ctx, orderID, eventID, userID, idempKey); err != nil {
			return fmt.Errorf("failed to create cancellation tombstone: %w", err)
		}
	} else {
		if order.Status == models.OrderStatusPaid {
			return errors.New("cannot cancel already paid order")
		}
		if order.Status == models.OrderStatusCancelled {

			return nil
		}
		if _, err := s.orderRepo.CancelOrder(ctx, orderID); err != nil {
			return fmt.Errorf("failed to cancel order: %w", err)
		}
	}

	eventIDStr := eventID.String()
	_ = s.stockCache.RollbackStock(ctx, eventIDStr, userID, 1)
	_ = s.eventRepo.IncrementAvailableTickets(ctx, eventID, 1)
	return nil
}