package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"flashpass/backend/internal/database"
	"flashpass/backend/internal/models"
	"flashpass/backend/internal/rabbitmq"
	"flashpass/backend/internal/redis"
	"flashpass/backend/internal/repository"
	"flashpass/backend/internal/resilience"
	"flashpass/backend/internal/service"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
)

type Handler struct {
	bookingService   service.BookingService
	orderService     service.OrderService
	reconcileService service.ReconcileService
	stockCache       redis.StockCache
	sf               singleflight.Group
	cb               *resilience.CircuitBreaker
}

type EventMetaData struct {
	Title     string    `json:"title"`
	Total     int       `json:"total_tickets"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

func NewHandler(db *database.DB, redisClient redis.StockCache, producer rabbitmq.EventProducer) *Handler {
	eventRepo := repository.NewEventRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	refundRepo := repository.NewRefundRepository(db)

	reconcileSvc := service.NewReconcileService(eventRepo, redisClient)
	bookingSvc := service.NewBookingService(redisClient, eventRepo, producer, reconcileSvc)
	orderSvc := service.NewOrderService(orderRepo, eventRepo, refundRepo, redisClient, producer)

	return &Handler{
		bookingService:   bookingSvc,
		orderService:     orderSvc,
		reconcileService: reconcileSvc,
		stockCache:       redisClient,
		cb:               resilience.NewCircuitBreaker(3, 1500*time.Millisecond),
	}
}

func NewHandlerWithServices(
	bookingSvc service.BookingService,
	orderSvc service.OrderService,
	reconcileSvc service.ReconcileService,
	stockCache redis.StockCache,
) *Handler {
	return &Handler{
		bookingService:   bookingSvc,
		orderService:     orderSvc,
		reconcileService: reconcileSvc,
		stockCache:       stockCache,
		cb:               resilience.NewCircuitBreaker(3, 1500*time.Millisecond),
	}
}

// HealthCheck provides liveness probe
// GET /health
func (h *Handler) HealthCheck(c *fiber.Ctx) error {
	return c.Status(http.StatusOK).JSON(fiber.Map{
		"status": "UP",
		"time":   time.Now().Format(time.RFC3339),
	})
}

// PrewarmEvent loads an event's available tickets from PostgreSQL into Redis stock cache
// POST /api/v1/admin/events/:id/prewarm
func (h *Handler) PrewarmEvent(c *fiber.Ctx) error {
	eventIDStr := c.Params("id")
	eventUUID, err := uuid.Parse(eventIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid event UUID format",
		})
	}

	title, stock, err := h.orderService.PrewarmEvent(c.Context(), eventUUID)
	if err != nil {
		if errors.Is(err, service.ErrEventNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Event not found in database",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to prewarm Redis cache",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"event_id": eventIDStr,
		"title":    title,
		"stock":    stock,
		"message":  "Event stock successfully prewarmed in Redis",
	})
}

// BookTicket handles flash sale ticket reservations with atomic Lua script and RabbitMQ ingestion
// POST /api/v1/tickets/book
func (h *Handler) BookTicket(c *fiber.Ctx) error {
	var req service.BookTicketRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body JSON",
		})
	}

	if req.EventID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "event_id is required",
		})
	}
	if _, err := uuid.Parse(req.EventID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "event_id must be a valid UUID",
		})
	}
	if req.UserID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "user_id is required",
		})
	}
	if req.IdempotencyKey == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "idempotency_key is required",
		})
	}

	allowed, remaining, err := h.stockCache.CheckRateLimit(c.Context(), req.UserID, 5, 2*time.Second)

	if err == nil && !allowed {
		c.Set("Retry-After", "2")
		c.Set("X-RateLimit-Limit", "5")
		c.Set("X-RateLimit-Remaining", "0")
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"error":       "Too many requests, rate lite exceeded",
			"retry_after": 2,
		})
	}
	c.Set("X-RateLimit-Limit", "5")
	c.Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

	res, err := h.bookingService.BookTicket(c.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUserAlreadyBooked):
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "User already booked a ticket for this event",
			})
		case errors.Is(err, service.ErrSoldOut):
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "Sold out",
			})
		case errors.Is(err, service.ErrQueueFailed):
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to queue order reservation",
			})
		default:
			c.Set("Retry-After", "2")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":       "Please retry again after 2 seconds",
				"retry-after": 2,
			})
		}
	}

	if res.IsCached {
		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
			"order_id": res.OrderID,
			"event_id": res.EventID,
			"user_id":  res.UserID,
			"status":   res.Status,
			"message":  res.Message,
		})
	}

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"order_id":   res.OrderID,
		"event_id":   res.EventID,
		"user_id":    res.UserID,
		"status":     res.Status,
		"expires_at": res.ExpiresAt,
	})
}

// GetStock returns real-time inventory from Redis
// GET /api/v1/events/:id/stock
func (h *Handler) GetStock(c *fiber.Ctx) error {
	eventID := c.Params("id")
	if eventID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "event id is required",
		})
	}

	stock, err := h.stockCache.GetStock(c.Context(), eventID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to read stock from cache",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"event_id": eventID,
		"stock":    stock,
	})
}

// GetEvent returns event metadata and live stock
// GET /api/v1/events/:id
func (h *Handler) GetEvent(c *fiber.Ctx) error {
	eventIDStr := c.Params("id")
	eventUUID, err := uuid.Parse(eventIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid event UUID"})
	}

	ctx := context.Background()
	metaKey := redis.EventMetaKey(eventIDStr)

	var meta EventMetaData
	cachedJSON, err := h.stockCache.RDB().Get(ctx, metaKey).Result()
	if err == nil {
		if err := json.Unmarshal([]byte(cachedJSON), &meta); err == nil {
			liveStock, _ := h.stockCache.GetStock(ctx, eventIDStr)
			return c.Status(fiber.StatusOK).JSON(fiber.Map{
				"id":                eventIDStr,
				"title":             meta.Title,
				"total_tickets":     meta.Total,
				"available_tickets": liveStock,
				"start_time":        meta.StartTime.Format(time.RFC3339),
				"end_time":          meta.EndTime.Format(time.RFC3339),
			})
		}
	}

	val, err, _ := h.sf.Do(eventIDStr, func() (interface{}, error) {
		details, err := h.orderService.GetEventDetails(ctx, eventUUID)
		if err != nil {
			return nil, err
		}
		m := EventMetaData{
			Title:     details.Title,
			Total:     details.TotalTickets,
			StartTime: details.StartTime,
			EndTime:   details.EndTime,
		}

		if data, err := json.Marshal(m); err == nil {
			_ = h.stockCache.RDB().Set(context.Background(), metaKey, string(data), 5*time.Minute).Err()
		}
		return details, nil
	})

	if err != nil {
		if errors.Is(err, service.ErrEventNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Event not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	details := val.(*service.EventDetails)
	liveStock, _ := h.stockCache.GetStock(ctx, eventIDStr)
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"id":                details.ID,
		"title":             details.Title,
		"total_tickets":     details.TotalTickets,
		"available_tickets": liveStock,
		"start_time":        details.StartTime.Format(time.RFC3339),
		"end_time":          details.EndTime.Format(time.RFC3339),
	})
}

// GetAdminMetrics returns live stats (Redis stock vs PostgreSQL orders) protected by Circuit Breaker
// GET /api/v1/admin/events/:id/metrics
func (h *Handler) GetAdminMetrics(c *fiber.Ctx) error {
	eventIDStr := c.Params("id")
	eventUUID, err := uuid.Parse(eventIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid event UUID"})
	}

	var metrics *service.EventMetrics
	cbErr := h.cb.Execute(func() error {
		if c.Query("chaos") == "db_error" {
			return errors.New("simulated database connection error (connection pool exhausted)")
		}
		var qErr error
		metrics, qErr = h.orderService.GetEventMetrics(c.Context(), eventUUID)
		return qErr
	})

	if cbErr != nil {
		if errors.Is(cbErr, resilience.ErrCircuitOpen) {
			// Circuit is OPEN -> Fast-Fail immediately in 0ms with HTTP 503
			c.Set("Retry-After", "2")
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error":       "Database circuit breaker is OPEN: service degraded",
				"retry_after": 2,
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch metrics"})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"event_id":       metrics.EventID,
		"stock":          metrics.Stock,
		"pending_orders": metrics.PendingOrders,
		"paid_orders":    metrics.PaidOrders,
		"expired_orders": metrics.ExpiredOrders,
		"total_orders":   metrics.TotalOrders,
	})
}

// GetCircuitBreakerStatus returns current circuit breaker state
// GET /api/v1/admin/circuit-breaker
func (h *Handler) GetCircuitBreakerStatus(c *fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"state": h.cb.State(),
	})
}

// ResetEvent cleans orders and restores Redis stock
// POST /api/v1/admin/events/:id/reset
func (h *Handler) ResetEvent(c *fiber.Ctx) error {
	eventIDStr := c.Params("id")
	eventUUID, err := uuid.Parse(eventIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid event UUID"})
	}

	var req struct {
		Tickets int `json:"tickets"`
	}
	_ = c.BodyParser(&req)
	customTickets := req.Tickets
	if customTickets <= 0 {
		customTickets = c.QueryInt("tickets", 0)
	}

	totalTickets, err := h.orderService.ResetEvent(c.Context(), eventUUID, customTickets)
	if err != nil {
		if errors.Is(err, service.ErrEventNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Event not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"event_id": eventIDStr,
		"stock":    totalTickets,
		"message":  "Event and inventory reset successfully",
	})
}

// PayOrder handles mock payment for a reserved order
// POST /api/v1/orders/:id/pay
func (h *Handler) PayOrder(c *fiber.Ctx) error {
	orderIDStr := c.Params("id")
	orderUUID, err := uuid.Parse(orderIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid order UUID format",
		})
	}

	res, err := h.orderService.PayOrder(c.Context(), orderUUID)
	fmt.Print(res, err)
	if err != nil {
		if errors.Is(err, service.ErrOrderNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Order not found",
			})
		}
		if errors.Is(err, service.ErrInvalidOrderStatus) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	if res.Status == models.OrderStatusRefundPending {
		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
			"order_id": res.OrderID,
			"status":   res.Status,
			"message":  res.Message,
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"order_id": res.OrderID,
		"status":   res.Status,
		"message":  res.Message,
	})
}

// ReconcileEvent audits and self-heals Ghost Inventory disparity between PostgreSQL and Redis
// POST /api/v1/admin/events/:id/reconcile
func (h *Handler) ReconcileEvent(c *fiber.Ctx) error {
	eventIDStr := c.Params("id")
	eventUUID, err := uuid.Parse(eventIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid event UUID"})
	}

	res, err := h.reconcileService.ReconcileEvent(c.Context(), eventUUID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"event_id":       res.EventID,
		"total_tickets":  res.TotalTickets,
		"active_orders":  res.ActiveOrders,
		"expected_stock": res.ExpectedStock,
		"actual_stock":   res.ActualStock,
		"discrepancy":    res.Discrepancy,
		"healed":         res.Healed,
		"message":        "Stock reconciliation complete",
	})
}

// UnlockUser clears orphaned user booking lock if the user has no active orders
// POST /api/v1/admin/events/:id/users/:userId/unlock
func (h *Handler) UnlockUser(c *fiber.Ctx) error {
	eventIDStr := c.Params("id")
	userID := c.Params("userId")
	if eventIDStr == "" || userID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "event id and user id are required"})
	}

	err := h.orderService.UnlockUser(c.Context(), eventIDStr, userID)
	if err != nil {
		if errors.Is(err, service.ErrUserHasActiveOrder) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "User already has an active or paid order, cannot unlock",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"event_id": eventIDStr,
		"user_id":  userID,
		"unlocked": true,
		"message":  "Orphaned user booking lock cleared successfully",
	})
}

// PerformReconciliation provides a backward-compatible adapter method
func (h *Handler) PerformReconciliation(ctx context.Context, eventUUID uuid.UUID) (totalTickets, activeOrders, expectedStock, actualStock, discrepancy int, healed bool, err error) {
	res, err := h.reconcileService.ReconcileEvent(ctx, eventUUID)
	if err != nil {
		if res != nil {
			return res.TotalTickets, res.ActiveOrders, res.ExpectedStock, 0, 0, false, err
		}
		return 0, 0, 0, 0, 0, false, err
	}
	return res.TotalTickets, res.ActiveOrders, res.ExpectedStock, res.ActualStock, res.Discrepancy, res.Healed, nil
}

type CancelOrderRequest struct {
	EventID string `json:"event_id"`
	UserID  string `json:"user_id"`
}

// CancelOrder handles user reservation cancellation
// POST /api/v1/orders/:id/cancel
func (h *Handler) CancelOrder(c *fiber.Ctx) error {
	orderIDStr := c.Params("id")
	orderUUID, err := uuid.Parse(orderIDStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid order UUID"})
	}
	var req CancelOrderRequest
	_ = c.BodyParser(&req)
	eventUUID, _ := uuid.Parse(req.EventID)
	if err := h.orderService.CancelOrder(c.Context(), orderUUID, eventUUID, req.UserID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"order_id": orderIDStr,
		"status":   models.OrderStatusCancelled,
		"message":  "Order successfully cancelled and ticket released",
	})
}
