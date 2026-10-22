package models

import (
	"time"

	"github.com/google/uuid"
)

type RefundStatus string

const (
	RefundStatusPending    RefundStatus = "PENDING" 
	RefundStatusProcessing RefundStatus = "PROCESSING"  
	RefundStatusSuccess    RefundStatus = "SUCCESS" 
	RefundStatusFailed     RefundStatus = "FAILED"
)

type RefundOrder struct {
	ID             uuid.UUID    `json:"id" db:"id"`
	OrderID        uuid.UUID    `json:"order_id" db:"order_id"`
	UserID         string       `json:"user_id" db:"user_id"`
	Amount         int64        `json:"amount" db:"amount"`
	Reason         string       `json:"reason" db:"reason"`
	Status         RefundStatus `json:"status" db:"status"`
	RetryCount     int          `json:"retry_count" db:"retry_count"` 
	MaxRetries     int          `json:"max_retries" db:"max_retries"` 
	NextRetryAt    time.Time    `json:"next_retry_at" db:"next_retry_at"`
	IdempotencyKey string       `json:"idempotency_key" db:"idempotency_key"`
	LastError      string       `json:"last_error,omitempty" db:"last_error"`
	CreatedAt      time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at" db:"updated_at"`
}