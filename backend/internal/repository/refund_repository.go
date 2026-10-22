package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"flashpass/backend/internal/database"
	"flashpass/backend/internal/models"
)

type RefundRepository interface {
	CreateLatePaymentRefund(ctx context.Context, orderID uuid.UUID, refundID uuid.UUID, idempKey string, amount int64) error
	UpdateRefundSuccess(ctx context.Context, orderID uuid.UUID) (bool, error)
	MarkRetry(ctx context.Context, orderID uuid.UUID, lastError string) error
	GetPendingForRetry(ctx context.Context, limit int) ([]*models.RefundOrder, error)
}

type refundRepo struct {
	db *database.DB
}

func NewRefundRepository(db *database.DB) RefundRepository {
	return &refundRepo{db: db}
}

func (r *refundRepo) CreateLatePaymentRefund(ctx context.Context, orderID uuid.UUID, refundID uuid.UUID, idempKey string, amount int64) error {
	query := `
		WITH updated AS (
			UPDATE orders SET status = $1, updated_at = NOW() 
			WHERE id = $2 AND status != $3
			RETURNING id, user_id
		)
		INSERT INTO refund_orders (id, order_id, user_id, amount, reason, status, idempotency_key)
		SELECT $4, u.id, u.user_id, $5, $6, $7, $8
		FROM updated u
		ON CONFLICT (idempotency_key) DO NOTHING;
	`
	_, err := r.db.Pool.Exec(ctx, query,
		models.OrderStatusRefundPending,
		orderID,
		models.OrderStatusPaid,
		refundID,
		amount,
		"LATE_PAYMENT_EXPIRED",
		models.RefundStatusPending,
		idempKey,
	)
	return err
}

func (r *refundRepo) UpdateRefundSuccess(ctx context.Context, orderID uuid.UUID) (bool, error) {
	query := `
		WITH updated_order AS (
			UPDATE orders SET status = $1, updated_at = NOW() 
			WHERE id = $2 
			RETURNING id
		) 
		UPDATE refund_orders 
		SET status = $3, updated_at = NOW() 
		WHERE order_id = (SELECT id FROM updated_order);
	`
	res, err := r.db.Pool.Exec(ctx, query,
		models.OrderStatusRefunded,
		orderID,
		models.RefundStatusSuccess,
	)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() > 0, nil
}

func (r *refundRepo) MarkRetry(ctx context.Context, orderID uuid.UUID, lastError string) error {
	query := `
		UPDATE refund_orders 
		SET retry_count = retry_count + 1, 
		    next_retry_at = NOW() + (INTERVAL '1 minute' * POWER(2, retry_count)),
		    last_error = $1
		WHERE order_id = $2;
	`
	_, err := r.db.Pool.Exec(ctx, query, lastError, orderID)
	return err
}

func (r *refundRepo) GetPendingForRetry(ctx context.Context, limit int) ([]*models.RefundOrder, error) {
	query := `
		SELECT id, order_id, user_id, amount, reason, status, retry_count, max_retries, next_retry_at, idempotency_key
		FROM refund_orders
		WHERE status = $1 AND next_retry_at <= NOW() AND retry_count < max_retries
		LIMIT $2;
	`
	rows, err := r.db.Pool.Query(ctx, query, models.RefundStatusPending, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending refunds: %w", err)
	}
	defer rows.Close()

	var list []*models.RefundOrder
	for rows.Next() {
		var o models.RefundOrder
		if err := rows.Scan(
			&o.ID,
			&o.OrderID,
			&o.UserID,
			&o.Amount,
			&o.Reason,
			&o.Status,
			&o.RetryCount,
			&o.MaxRetries,
			&o.NextRetryAt,
			&o.IdempotencyKey,
		); err != nil {
			return nil, err
		}
		list = append(list, &o)
	}
	return list, nil
}
