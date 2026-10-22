package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"flashpass/backend/internal/models"
	"flashpass/backend/internal/redis"
	"flashpass/backend/internal/repository"
	"flashpass/backend/internal/service"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	goredis "github.com/redis/go-redis/v9"
)

// MockStockCache implements redis.StockCache for unit tests
type mockStockCache struct {
	bookTicketFn       func(ctx context.Context, eventID, userID string, quantity int) (int64, error)
	rollbackStockFn    func(ctx context.Context, eventID, userID string, quantity int) error
	clearUserBookedFn  func(ctx context.Context, eventID, userID string) error
	prewarmStockFn     func(ctx context.Context, eventID string, stock int) error
	getStockFn         func(ctx context.Context, eventID string) (int, error)
	checkIdempotencyFn func(ctx context.Context, idempotencyKey string) (string, bool, error)
	setIdempotencyFn   func(ctx context.Context, idempotencyKey string, orderID string, ttl time.Duration) error
	acquireLockFn      func(ctx context.Context, key string, ttl time.Duration) (bool, error)
	releaseLockFn      func(ctx context.Context, key string) error
	checkRateLimitFn   func(ctx context.Context, identifier string, limit int, window time.Duration) (bool, int, error)
}

func (m *mockStockCache) CheckRateLimit(ctx context.Context, identifier string, limit int, window time.Duration) (bool, int, error) {
	return true, limit - 1, nil
}

func (m *mockStockCache) AcquireLockWithWatchdog(ctx context.Context, key string, ttl time.Duration) (*redis.WatchdogLock, bool, error) {
	return nil, true, nil
}

// ReleaseLock implements [redis.StockCache].
func (m *mockStockCache) ReleaseLock(ctx context.Context, key string) error {
	if m.releaseLockFn != nil {
		return m.releaseLockFn(ctx, key)
	}
	return nil
}

func (m *mockStockCache) BookTicket(ctx context.Context, eventID, userID string, quantity int) (int64, error) {
	if m.bookTicketFn != nil {
		return m.bookTicketFn(ctx, eventID, userID, quantity)
	}
	return redis.ResultSuccess, nil
}
func (m *mockStockCache) RollbackStock(ctx context.Context, eventID, userID string, quantity int) error {
	if m.rollbackStockFn != nil {
		return m.rollbackStockFn(ctx, eventID, userID, quantity)
	}
	return nil
}
func (m *mockStockCache) ClearUserBooked(ctx context.Context, eventID, userID string) error {
	if m.clearUserBookedFn != nil {
		return m.clearUserBookedFn(ctx, eventID, userID)
	}
	return nil
}
func (m *mockStockCache) PrewarmStock(ctx context.Context, eventID string, stock int) error {
	if m.prewarmStockFn != nil {
		return m.prewarmStockFn(ctx, eventID, stock)
	}
	return nil
}
func (m *mockStockCache) GetStock(ctx context.Context, eventID string) (int, error) {
	if m.getStockFn != nil {
		return m.getStockFn(ctx, eventID)
	}
	return 10, nil
}
func (m *mockStockCache) CheckIdempotency(ctx context.Context, idempotencyKey string) (string, bool, error) {
	if m.checkIdempotencyFn != nil {
		return m.checkIdempotencyFn(ctx, idempotencyKey)
	}
	return "", false, nil
}
func (m *mockStockCache) SetIdempotency(ctx context.Context, idempotencyKey string, orderID string, ttl time.Duration) error {
	if m.setIdempotencyFn != nil {
		return m.setIdempotencyFn(ctx, idempotencyKey, orderID, ttl)
	}
	return nil
}
func (m *mockStockCache) IsUserBooked(ctx context.Context, eventID, userID string) (bool, error) {
	return false, nil
}
func (m *mockStockCache) ResetEvent(ctx context.Context, eventID string, stock int) error {
	return nil
}
func (m *mockStockCache) IsMasterAlive(ctx context.Context) bool {
	return true
}
func (m *mockStockCache) AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if m.acquireLockFn != nil {
		return m.acquireLockFn(ctx, key, ttl)
	}
	return true, nil
}
func (m *mockStockCache) Ping(ctx context.Context) error {
	return nil
}
func (m *mockStockCache) Close() error {
	return nil
}
func (m *mockStockCache) RDB() *goredis.Client {
	return nil
}

// MockEventRepo implements repository.EventRepository
type mockEventRepo struct {
	getByIDFn                    func(ctx context.Context, id uuid.UUID) (*models.Event, error)
	updateAvailableTicketsFn     func(ctx context.Context, id uuid.UUID, tickets int) error
	decrementAvailableTicketsFn  func(ctx context.Context, id uuid.UUID) (bool, error)
	incrementAvailableTicketsFn  func(ctx context.Context, id uuid.UUID, quantity int) error
	resetEventFn                 func(ctx context.Context, id uuid.UUID, totalTickets int) error
	getEventWithActiveCountFn    func(ctx context.Context, id uuid.UUID) (int, int, error)
	getAllEventsWithActiveCounts func(ctx context.Context) ([]repository.EventWithActiveCount, error)
}

func (m *mockEventRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.Event, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return &models.Event{ID: id, Title: "Test Event", TotalTickets: 100, AvailableTickets: 100}, nil
}
func (m *mockEventRepo) UpdateAvailableTickets(ctx context.Context, id uuid.UUID, tickets int) error {
	if m.updateAvailableTicketsFn != nil {
		return m.updateAvailableTicketsFn(ctx, id, tickets)
	}
	return nil
}
func (m *mockEventRepo) DecrementAvailableTickets(ctx context.Context, id uuid.UUID) (bool, error) {
	if m.decrementAvailableTicketsFn != nil {
		return m.decrementAvailableTicketsFn(ctx, id)
	}
	return true, nil
}
func (m *mockEventRepo) IncrementAvailableTickets(ctx context.Context, id uuid.UUID, quantity int) error {
	if m.incrementAvailableTicketsFn != nil {
		return m.incrementAvailableTicketsFn(ctx, id, quantity)
	}
	return nil
}
func (m *mockEventRepo) ResetEvent(ctx context.Context, id uuid.UUID, totalTickets int) error {
	if m.resetEventFn != nil {
		return m.resetEventFn(ctx, id, totalTickets)
	}
	return nil
}
func (m *mockEventRepo) GetEventWithActiveCount(ctx context.Context, id uuid.UUID) (int, int, error) {
	if m.getEventWithActiveCountFn != nil {
		return m.getEventWithActiveCountFn(ctx, id)
	}
	return 100, 10, nil
}
func (m *mockEventRepo) GetAllEventsWithActiveCounts(ctx context.Context) ([]repository.EventWithActiveCount, error) {
	if m.getAllEventsWithActiveCounts != nil {
		return m.getAllEventsWithActiveCounts(ctx)
	}
	return nil, nil
}

// MockProducer implements rabbitmq.EventProducer
type mockProducer struct {
	publishOrderCreateFn func(ctx context.Context, payload interface{}) error
	publishTimeoutFn     func(ctx context.Context, payload interface{}, delayMs int64) error
	publishRefundFn      func(ctx context.Context, payload interface{}) error
	getQueueDepthFn      func(queueName string) (int, error)
}

func (m *mockProducer) PublishOrderCreate(ctx context.Context, payload interface{}) error {
	if m.publishOrderCreateFn != nil {
		return m.publishOrderCreateFn(ctx, payload)
	}
	return nil
}
func (m *mockProducer) PublishDelayedOrderTimeout(ctx context.Context, payload interface{}, delayMs int64) error {
	if m.publishTimeoutFn != nil {
		return m.publishTimeoutFn(ctx, payload, delayMs)
	}
	return nil
}
func (m *mockProducer) PublishOrderRefund(ctx context.Context, payload interface{}) error {
	if m.publishRefundFn != nil {
		return m.publishRefundFn(ctx, payload)
	}
	return nil
}
func (m *mockProducer) GetQueueDepth(queueName string) (int, error) {
	if m.getQueueDepthFn != nil {
		return m.getQueueDepthFn(queueName)
	}
	return 0, nil
}
func (m *mockProducer) Channel() (*amqp.Channel, error) {
	return nil, nil
}
func (m *mockProducer) Close() {}

// MockReconcileService implements service.ReconcileService
type mockReconcileService struct {
	reconcileEventFn        func(ctx context.Context, eventUUID uuid.UUID) (*service.ReconcileResult, error)
	triggerEmergencyAuditFn func(eventID string)
	reconcileAllFn          func(ctx context.Context, queueDepth int, suspected map[string]int) error
}

func (m *mockReconcileService) ReconcileEvent(ctx context.Context, eventUUID uuid.UUID) (*service.ReconcileResult, error) {
	if m.reconcileEventFn != nil {
		return m.reconcileEventFn(ctx, eventUUID)
	}
	return &service.ReconcileResult{EventID: eventUUID.String(), Healed: false}, nil
}
func (m *mockReconcileService) TriggerEmergencyAudit(eventID string) {
	if m.triggerEmergencyAuditFn != nil {
		m.triggerEmergencyAuditFn(eventID)
	}
}
func (m *mockReconcileService) ReconcileAll(ctx context.Context, queueDepth int, suspected map[string]int) error {
	if m.reconcileAllFn != nil {
		return m.reconcileAllFn(ctx, queueDepth, suspected)
	}
	return nil
}

func TestBookingService_Success(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New().String()
	userID := "user-unit-1"
	idempKey := "idemp-unit-1"

	mockCache := &mockStockCache{
		bookTicketFn: func(ctx context.Context, eID, uID string, qty int) (int64, error) {
			return redis.ResultSuccess, nil
		},
	}
	mockRepo := &mockEventRepo{
		decrementAvailableTicketsFn: func(ctx context.Context, id uuid.UUID) (bool, error) {
			return true, nil
		},
	}
	mockProd := &mockProducer{}
	mockReconcile := &mockReconcileService{}

	bookingSvc := service.NewBookingService(mockCache, mockRepo, mockProd, mockReconcile)

	res, err := bookingSvc.BookTicket(ctx, service.BookTicketRequest{
		EventID:        eventID,
		UserID:         userID,
		IdempotencyKey: idempKey,
	})
	if err != nil {
		t.Fatalf("Expected booking to succeed, got: %v", err)
	}

	if res.Status != models.OrderStatusPendingPayment {
		t.Errorf("Expected status %s, got %s", models.OrderStatusPendingPayment, res.Status)
	}
	if res.OrderID == "" {
		t.Errorf("Expected valid orderID, got empty")
	}
}

func TestBookingService_SoldOut(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New().String()

	auditTriggered := false
	mockCache := &mockStockCache{
		bookTicketFn: func(ctx context.Context, eID, uID string, qty int) (int64, error) {
			return redis.ResultSoldOut, nil
		},
	}
	mockRepo := &mockEventRepo{}
	mockProd := &mockProducer{}
	mockReconcile := &mockReconcileService{
		triggerEmergencyAuditFn: func(eID string) {
			auditTriggered = true
		},
	}

	bookingSvc := service.NewBookingService(mockCache, mockRepo, mockProd, mockReconcile)

	_, err := bookingSvc.BookTicket(ctx, service.BookTicketRequest{
		EventID:        eventID,
		UserID:         "user-2",
		IdempotencyKey: "idemp-2",
	})
	if !errors.Is(err, service.ErrSoldOut) {
		t.Fatalf("Expected ErrSoldOut, got: %v", err)
	}
	if !auditTriggered {
		t.Errorf("Expected emergency audit to be triggered on sold out")
	}
}

func TestBookingService_AlreadyBooked(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New().String()

	mockCache := &mockStockCache{
		bookTicketFn: func(ctx context.Context, eID, uID string, qty int) (int64, error) {
			return redis.ResultAlreadyBooked, nil
		},
	}
	bookingSvc := service.NewBookingService(mockCache, &mockEventRepo{}, &mockProducer{}, &mockReconcileService{})

	_, err := bookingSvc.BookTicket(ctx, service.BookTicketRequest{
		EventID:        eventID,
		UserID:         "user-3",
		IdempotencyKey: "idemp-3",
	})
	if !errors.Is(err, service.ErrUserAlreadyBooked) {
		t.Fatalf("Expected ErrUserAlreadyBooked, got: %v", err)
	}
}

func TestBookingService_IdempotencyCacheHit(t *testing.T) {
	ctx := context.Background()
	eventID := uuid.New().String()
	existingOrderID := uuid.New().String()

	mockCache := &mockStockCache{
		checkIdempotencyFn: func(ctx context.Context, key string) (string, bool, error) {
			return existingOrderID, true, nil
		},
	}
	bookingSvc := service.NewBookingService(mockCache, &mockEventRepo{}, &mockProducer{}, &mockReconcileService{})

	res, err := bookingSvc.BookTicket(ctx, service.BookTicketRequest{
		EventID:        eventID,
		UserID:         "user-dup",
		IdempotencyKey: "idemp-dup",
	})
	if err != nil {
		t.Fatalf("Expected idempotency cache hit to return without error, got: %v", err)
	}
	if !res.IsCached || res.OrderID != existingOrderID {
		t.Errorf("Expected cached order ID %s, got %+v", existingOrderID, res)
	}
}
