package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"flashpass/backend/internal/models"
	"flashpass/backend/internal/service"
	"github.com/google/uuid"
)

type mockOrderRepo struct {
	getByIDFn                 func(ctx context.Context, id uuid.UUID) (*models.Order, error)
	updateStatusFn            func(ctx context.Context, id uuid.UUID, cur, next models.OrderStatus) (bool, error)
	getMetricsByEventIDFn     func(ctx context.Context, eventID uuid.UUID) (int, int, int, int, error)
	deleteByEventIDFn         func(ctx context.Context, eventID uuid.UUID) error
	countActiveOrdersByUserFn func(ctx context.Context, eventID uuid.UUID, userID string) (int, error)
}

func (m *mockOrderRepo) Create(ctx context.Context, order *models.Order) error { return nil }
func (m *mockOrderRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.Order, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}
func (m *mockOrderRepo) UpdateStatus(ctx context.Context, id uuid.UUID, cur, next models.OrderStatus) (bool, error) {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(ctx, id, cur, next)
	}
	return true, nil
}
func (m *mockOrderRepo) GetMetricsByEventID(ctx context.Context, eventID uuid.UUID) (int, int, int, int, error) {
	if m.getMetricsByEventIDFn != nil {
		return m.getMetricsByEventIDFn(ctx, eventID)
	}
	return 0, 0, 0, 0, nil
}
func (m *mockOrderRepo) DeleteByEventID(ctx context.Context, eventID uuid.UUID) error { return nil }
func (m *mockOrderRepo) CountActiveOrdersByUser(ctx context.Context, eventID uuid.UUID, userID string) (int, error) {
	if m.countActiveOrdersByUserFn != nil {
		return m.countActiveOrdersByUserFn(ctx, eventID, userID)
	}
	return 0, nil
}
func (m *mockOrderRepo) ExpireIfPastDeadline(ctx context.Context, id uuid.UUID) (bool, error) {
	return true, nil
}

type mockRefundRepo struct {
	createLatePaymentRefundFn func(ctx context.Context, orderID, refundID uuid.UUID, idempKey string, amount int64) error
	updateRefundSuccessFn     func(ctx context.Context, orderID uuid.UUID) (bool, error)
	markRetryFn               func(ctx context.Context, orderID uuid.UUID, lastError string) error
	getPendingForRetryFn      func(ctx context.Context, limit int) ([]*models.RefundOrder, error)
}

func (m *mockRefundRepo) CreateLatePaymentRefund(ctx context.Context, orderID, refundID uuid.UUID, idempKey string, amount int64) error {
	if m.createLatePaymentRefundFn != nil {
		return m.createLatePaymentRefundFn(ctx, orderID, refundID, idempKey, amount)
	}
	return nil
}
func (m *mockRefundRepo) UpdateRefundSuccess(ctx context.Context, orderID uuid.UUID) (bool, error) {
	return true, nil
}
func (m *mockRefundRepo) MarkRetry(ctx context.Context, orderID uuid.UUID, lastError string) error {
	return nil
}
func (m *mockRefundRepo) GetPendingForRetry(ctx context.Context, limit int) ([]*models.RefundOrder, error) {
	return nil, nil
}

func (m *mockOrderRepo) CancelOrder(ctx context.Context, id uuid.UUID) (*models.Order, error) {
	return nil, nil
}
func (m *mockOrderRepo) CreateTombstone(ctx context.Context, orderID, eventID uuid.UUID, userID, idempKey string) error {
	return nil
}

func TestOrderService_PayOrder_Success(t *testing.T) {
	ctx := context.Background()
	orderID := uuid.New()

	mockOrder := &mockOrderRepo{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*models.Order, error) {
			return &models.Order{
				ID:        orderID,
				Status:    models.OrderStatusPendingPayment,
				ExpiresAt: time.Now().Add(10 * time.Minute),
			}, nil
		},
		updateStatusFn: func(ctx context.Context, id uuid.UUID, cur, next models.OrderStatus) (bool, error) {
			return true, nil
		},
	}

	orderSvc := service.NewOrderService(mockOrder, &mockEventRepo{}, &mockRefundRepo{}, &mockStockCache{}, &mockProducer{})

	res, err := orderSvc.PayOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("Expected payment success, got: %v", err)
	}
	if res.Status != models.OrderStatusPaid {
		t.Errorf("Expected status PAID, got %s", res.Status)
	}
}

func TestOrderService_PayOrder_ExpiredAutoRefund(t *testing.T) {
	ctx := context.Background()
	orderID := uuid.New()
	refundCreated := false
	refundPublished := false

	mockOrder := &mockOrderRepo{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*models.Order, error) {
			return &models.Order{
				ID:        orderID,
				Status:    models.OrderStatusExpired,
				ExpiresAt: time.Now().Add(-1 * time.Minute),
			}, nil
		},
	}
	mockRefund := &mockRefundRepo{
		createLatePaymentRefundFn: func(ctx context.Context, oID, rID uuid.UUID, key string, amt int64) error {
			refundCreated = true
			return nil
		},
	}
	mockProd := &mockProducer{
		publishRefundFn: func(ctx context.Context, payload interface{}) error {
			refundPublished = true
			return nil
		},
	}

	orderSvc := service.NewOrderService(mockOrder, &mockEventRepo{}, mockRefund, &mockStockCache{}, mockProd)

	res, err := orderSvc.PayOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("Expected auto-refund flow without error, got: %v", err)
	}
	if res.Status != models.OrderStatusRefundPending {
		t.Errorf("Expected status REFUND_PENDING, got %s", res.Status)
	}
	if !refundCreated {
		t.Errorf("Expected refund record to be created in DB")
	}
	if !refundPublished {
		t.Errorf("Expected refund event to be published to RabbitMQ")
	}
}

func TestOrderService_UnlockUser(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New().String()
	userID := "user-unlock"

	// Case 1: User has active orders -> Cannot unlock
	mockOrderActive := &mockOrderRepo{
		countActiveOrdersByUserFn: func(ctx context.Context, eID uuid.UUID, uID string) (int, error) {
			return 1, nil
		},
	}
	orderSvc := service.NewOrderService(mockOrderActive, &mockEventRepo{}, &mockRefundRepo{}, &mockStockCache{}, &mockProducer{})
	err := orderSvc.UnlockUser(ctx, eventID, userID)
	if !errors.Is(err, service.ErrUserHasActiveOrder) {
		t.Fatalf("Expected ErrUserHasActiveOrder, got: %v", err)
	}

	// Case 2: User has 0 active orders -> Successfully unlocked
	lockCleared := false
	mockOrderClean := &mockOrderRepo{
		countActiveOrdersByUserFn: func(ctx context.Context, eID uuid.UUID, uID string) (int, error) {
			return 0, nil
		},
	}
	mockStock := &mockStockCache{
		clearUserBookedFn: func(ctx context.Context, eID, uID string) error {
			lockCleared = true
			return nil
		},
	}
	orderSvcClean := service.NewOrderService(mockOrderClean, &mockEventRepo{}, &mockRefundRepo{}, mockStock, &mockProducer{})
	err = orderSvcClean.UnlockUser(ctx, eventID, userID)
	if err != nil {
		t.Fatalf("Expected successful unlock, got: %v", err)
	}
	if !lockCleared {
		t.Errorf("Expected Redis user lock to be cleared")
	}
}
