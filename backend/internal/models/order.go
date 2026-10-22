package models

import (
	"time"

	"github.com/google/uuid"
)

type OrderStatus string

const (
	OrderStatusPendingPayment OrderStatus = "PENDING_PAYMENT"
	OrderStatusPaid           OrderStatus = "PAID"
	OrderStatusExpired        OrderStatus = "EXPIRED"
	OrderStatusRefundPending  OrderStatus = "REFUND_PENDING"
	OrderStatusRefunded       OrderStatus = "REFUNDED"
	OrderStatusCancelled      OrderStatus = "CANCELLED"
)

type Order struct {
	ID             uuid.UUID   `json:"id" db:"id"`
	EventID        uuid.UUID   `json:"event_id" db:"event_id"`
	UserID         string      `json:"user_id" db:"user_id"`
	IdempotencyKey string      `json:"idempotency_key" db:"idempotency_key"`
	Status         OrderStatus `json:"status" db:"status"`
	ExpiresAt      time.Time   `json:"expires_at" db:"expires_at"`
	CreatedAt      time.Time   `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at" db:"updated_at"`
}
