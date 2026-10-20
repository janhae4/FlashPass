package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"flashpass/backend/internal/database"
	"flashpass/backend/internal/rabbitmq"
	"flashpass/backend/internal/redis"
)

type Handler struct {
	db       *database.DB
	redis    *redis.Client
	producer *rabbitmq.Producer
}

func NewHandler(db *database.DB, redis *redis.Client, producer *rabbitmq.Producer) *Handler {
	return &Handler{
		db:       db,
		redis:    redis,
		producer: producer,
	}
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

	ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
	defer cancel()

	var availableTickets int
	var title string
	err = h.db.Pool.QueryRow(ctx, "SELECT title, available_tickets FROM events WHERE id = $1", eventUUID).
		Scan(&title, &availableTickets)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Event not found in database",
		})
	}

	if err := h.redis.PrewarmStock(ctx, eventIDStr, availableTickets); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to prewarm Redis cache",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"event_id":  eventIDStr,
		"title":     title,
		"stock":     availableTickets,
		"message":   "Event stock successfully prewarmed in Redis",
	})
}

// BookTicketRequest represents the incoming JSON payload for ticket reservation
type BookTicketRequest struct {
	EventID        string `json:"event_id"`
	UserID         string `json:"user_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

// BookTicket handles flash sale ticket reservations with atomic Lua script and RabbitMQ ingestion
// POST /api/v1/tickets/book
func (h *Handler) BookTicket(c *fiber.Ctx) error {
	var req BookTicketRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body JSON",
		})
	}

	// Mandatory validations
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

	ctx := c.Context()

	// 1. Idempotency Check (returns cached token if duplicate within 30s)
	cachedOrderID, exists, err := h.redis.CheckIdempotency(ctx, req.IdempotencyKey)
	if err == nil && exists {
		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
			"order_id":        cachedOrderID,
			"event_id":        req.EventID,
			"user_id":         req.UserID,
			"status":          "PENDING_PAYMENT",
			"message":         "Duplicate request: returning existing reservation",
		})
	}

	// 2. Atomic Lua Script: Check stock & 1-ticket-per-user guard
	result, err := h.redis.BookTicket(ctx, req.EventID, req.UserID, 1)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal ticketing engine failure",
		})
	}

	switch result {
	case redis.ResultAlreadyBooked:
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": "User already booked a ticket for this event",
		})
	case redis.ResultSoldOut:
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": "Sold out",
		})
	case redis.ResultSuccess:
		orderID := uuid.New().String()
		expiresAt := time.Now().Add(10 * time.Minute)

		// Cache idempotency key for 30s
		_ = h.redis.SetIdempotency(ctx, req.IdempotencyKey, orderID, 30*time.Second)

		// Publish order.create event to RabbitMQ
		orderEvent := map[string]interface{}{
			"order_id":        orderID,
			"event_id":        req.EventID,
			"user_id":         req.UserID,
			"idempotency_key": req.IdempotencyKey,
			"expires_at":      expiresAt,
		}

		if err := h.producer.PublishOrderCreate(ctx, orderEvent); err != nil {
			// Rollback Redis stock if broker dispatch fails
			_ = h.redis.RollbackStock(ctx, req.EventID, req.UserID, 1)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to queue order reservation",
			})
		}

		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
			"order_id":   orderID,
			"event_id":   req.EventID,
			"user_id":    req.UserID,
			"status":     "PENDING_PAYMENT",
			"expires_at": expiresAt.Format(time.RFC3339),
		})
	default:
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Unknown reservation response",
		})
	}
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

	stock, err := h.redis.GetStock(c.Context(), eventID)
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

	ctx := c.Context()

	var status string
	var expiresAt time.Time
	err = h.db.Pool.QueryRow(ctx, "SELECT status, expires_at FROM orders WHERE id = $1", orderUUID).
		Scan(&status, &expiresAt)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Order not found",
		})
	}

	if status == "PAID" {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"order_id": orderIDStr,
			"status":   "PAID",
			"message":  "Order already paid",
		})
	}

	if status == "EXPIRED" || time.Now().After(expiresAt) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Order has expired and tickets were released",
		})
	}

	if status != "PENDING_PAYMENT" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Order cannot be paid in current status: " + status,
		})
	}

	// Update to PAID
	res, err := h.db.Pool.Exec(ctx, `
		UPDATE orders SET status = 'PAID', updated_at = NOW()
		WHERE id = $1 AND status = 'PENDING_PAYMENT'
	`, orderUUID)
	if err != nil || res.RowsAffected() == 0 {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update order payment status",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"order_id": orderIDStr,
		"status":   "PAID",
		"message":  "Payment successful",
	})
}

// HealthCheck provides liveness probe
// GET /health
func (h *Handler) HealthCheck(c *fiber.Ctx) error {
	return c.Status(http.StatusOK).JSON(fiber.Map{
		"status": "UP",
		"time":   time.Now().Format(time.RFC3339),
	})
}
