package repository

import (
	"context"
	"fmt"

	"flashpass/backend/internal/database"
	"flashpass/backend/internal/models"
	"github.com/google/uuid"
)

type OrderRepository interface {
	Create(ctx context.Context, order *models.Order) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.Order, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, currentStatus, newStatus models.OrderStatus) (bool, error)
	GetMetricsByEventID(ctx context.Context, eventID uuid.UUID) (pending, paid, expired, total int, err error)
	DeleteByEventID(ctx context.Context, eventID uuid.UUID) error
	CountActiveOrdersByUser(ctx context.Context, eventID uuid.UUID, userID string) (int, error)
	ExpireIfPastDeadline(ctx context.Context, id uuid.UUID) (bool, error)
	CancelOrder(ctx context.Context, id uuid.UUID) (*models.Order, error)
	CreateTombstone(ctx context.Context, orderID, eventID uuid.UUID, userID, idempKey string) error
}

type orderRepo struct {
	db *database.DB
}

func NewOrderRepository(db *database.DB) OrderRepository {
	return &orderRepo{db: db}
}

func (r *orderRepo) Create(ctx context.Context, order *models.Order) error {
	query := `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE
		SET
			event_id = EXCLUDED.event_id,
			user_id = EXCLUDED.user_id,
			idempotency_key = EXCLUDED.idempotency_key,
			expires_at = EXCLUDED.expires_at,
			status = CASE
				WHEN orders.status = 'CANCELLED' THEN 'CANCELLED'
				WHEN orders.status = 'PAID' THEN 'PAID'
				ELSE EXCLUDED.status
			END,
			updated_at = NOW()
	`

	_, err := r.db.Pool.Exec(ctx, query,
		order.ID,
		order.EventID,
		order.UserID,
		order.IdempotencyKey,
		order.Status,
		order.ExpiresAt,
	)
	return err
}

func (r *orderRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.Order, error) {
	query := `
		SELECT id, event_id, user_id, idempotency_key, status, expires_at, created_at, updated_at
		FROM orders 
		WHERE id = $1
	`
	var o models.Order
	err := r.db.Pool.QueryRow(ctx, query, id).Scan(
		&o.ID,
		&o.EventID,
		&o.UserID,
		&o.IdempotencyKey,
		&o.Status,
		&o.ExpiresAt,
		&o.CreatedAt,
		&o.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get order by id: %w", err)
	}
	return &o, nil
}

func (r *orderRepo) UpdateStatus(ctx context.Context, id uuid.UUID, currentStatus, newStatus models.OrderStatus) (bool, error) {
	query := `
		UPDATE orders 
		SET status = $1, updated_at = NOW()
		WHERE id = $2 AND status = $3
	`
	res, err := r.db.Pool.Exec(ctx, query, newStatus, id, currentStatus)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() > 0, nil
}

func (r *orderRepo) GetMetricsByEventID(ctx context.Context, eventID uuid.UUID) (pending, paid, expired, total int, err error) {
	query := `
		SELECT status, count(*) 
		FROM orders 
		WHERE event_id = $1 
		GROUP BY status
	`
	rows, err := r.db.Pool.Query(ctx, query, eventID)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var st string
		var cnt int
		if err := rows.Scan(&st, &cnt); err == nil {
			total += cnt
			switch models.OrderStatus(st) {
			case models.OrderStatusPendingPayment:
				pending = cnt
			case models.OrderStatusPaid:
				paid = cnt
			case models.OrderStatusExpired:
				expired = cnt
			}
		}
	}
	return pending, paid, expired, total, nil
}

func (r *orderRepo) DeleteByEventID(ctx context.Context, eventID uuid.UUID) error {
	query := `DELETE FROM orders WHERE event_id = $1`
	_, err := r.db.Pool.Exec(ctx, query, eventID)
	return err
}

func (r *orderRepo) CountActiveOrdersByUser(ctx context.Context, eventID uuid.UUID, userID string) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM orders 
		WHERE event_id = $1 AND user_id = $2 AND status IN ($3, $4)
	`
	var count int
	err := r.db.Pool.QueryRow(ctx, query, eventID, userID, models.OrderStatusPendingPayment, models.OrderStatusPaid).Scan(&count)
	return count, err
}

func (r *orderRepo) ExpireIfPastDeadline(ctx context.Context, id uuid.UUID) (bool, error) {
	query := `
		UPDATE orders 
		SET status = 'EXPIRED', updated_at = NOW() 
		WHERE id = $1 AND status = 'PENDING_PAYMENT' AND expires_at <= NOW();
	`
	cmdTag, err := r.db.Pool.Exec(ctx, query, id)
	if err != nil {
		return false, fmt.Errorf("failed to expire order with db time guard: %w", err)
	}
	return cmdTag.RowsAffected() > 0, nil
}

func (r *orderRepo) CancelOrder(ctx context.Context, id uuid.UUID) (*models.Order, error) {
	query := `
		UPDATE orders
		SET status = 'CANCELLED', updated_at = NOW()
		WHERE id = $1 AND status = $2
		RETURNING id, event_id, user_id, idempotency_key, status, expires_at, created_at, updated_at;
	`
	var o models.Order
	err := r.db.Pool.QueryRow(ctx, query, id, models.OrderStatusPendingPayment).Scan(
		&o.ID, &o.EventID, &o.UserID, &o.IdempotencyKey, &o.Status, &o.ExpiresAt, &o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *orderRepo) CreateTombstone(ctx context.Context, orderID, eventID uuid.UUID, userID, idempKey string) error {
	query := `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'CANCELLED', NOW(), NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET status = 'CANCELLED', updated_at = NOW();
	`
	_, err := r.db.Pool.Exec(ctx, query, orderID, eventID, userID, idempKey)
	return err
}
