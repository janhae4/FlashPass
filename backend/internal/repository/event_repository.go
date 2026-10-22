package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"flashpass/backend/internal/database"
	"flashpass/backend/internal/models"
)

type EventRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*models.Event, error)
	UpdateAvailableTickets(ctx context.Context, id uuid.UUID, tickets int) error
	DecrementAvailableTickets(ctx context.Context, id uuid.UUID) (bool, error)
	IncrementAvailableTickets(ctx context.Context, id uuid.UUID, quantity int) error
	ResetEvent(ctx context.Context, id uuid.UUID, totalTickets int) error
	GetEventWithActiveCount(ctx context.Context, id uuid.UUID) (totalTickets int, activeOrders int, err error)
	GetAllEventsWithActiveCounts(ctx context.Context) ([]EventWithActiveCount, error)
}

type EventWithActiveCount struct {
	EventID      uuid.UUID
	TotalTickets int
	ActiveOrders int
}

type eventRepo struct {
	db *database.DB
}

func NewEventRepository(db *database.DB) EventRepository {
	return &eventRepo{db: db}
}

func (r *eventRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.Event, error) {
	query := `
		SELECT id, title, total_tickets, available_tickets, start_time, end_time, created_at
		FROM events 
		WHERE id = $1
	`
	var e models.Event
	err := r.db.Pool.QueryRow(ctx, query, id).Scan(
		&e.ID,
		&e.Title,
		&e.TotalTickets,
		&e.AvailableTickets,
		&e.StartTime,
		&e.EndTime,
		&e.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get event by id: %w", err)
	}
	return &e, nil
}

func (r *eventRepo) UpdateAvailableTickets(ctx context.Context, id uuid.UUID, tickets int) error {
	query := `UPDATE events SET available_tickets = $1 WHERE id = $2`
	_, err := r.db.Pool.Exec(ctx, query, tickets, id)
	return err
}

func (r *eventRepo) DecrementAvailableTickets(ctx context.Context, id uuid.UUID) (bool, error) {
	query := `
		UPDATE events 
		SET available_tickets = available_tickets - 1 
		WHERE id = $1 AND available_tickets >= 1
	`
	tag, err := r.db.Pool.Exec(ctx, query, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *eventRepo) IncrementAvailableTickets(ctx context.Context, id uuid.UUID, quantity int) error {
	query := `UPDATE events SET available_tickets = available_tickets + $1 WHERE id = $2`
	_, err := r.db.Pool.Exec(ctx, query, quantity, id)
	return err
}

func (r *eventRepo) ResetEvent(ctx context.Context, id uuid.UUID, totalTickets int) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "UPDATE events SET total_tickets = $1, available_tickets = $1 WHERE id = $2", totalTickets, id); err != nil {
		return fmt.Errorf("failed to reset event tickets: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM orders WHERE event_id = $1", id); err != nil {
		return fmt.Errorf("failed to delete event orders: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *eventRepo) GetEventWithActiveCount(ctx context.Context, id uuid.UUID) (int, int, error) {
	query := `
		SELECT 
			e.total_tickets,
			(SELECT COUNT(*) FROM orders WHERE event_id = e.id AND status IN ($1, $2)) AS active_orders
		FROM events e
		WHERE e.id = $3
	`
	var totalTickets, activeOrders int
	err := r.db.Pool.QueryRow(ctx, query, models.OrderStatusPendingPayment, models.OrderStatusPaid, id).
		Scan(&totalTickets, &activeOrders)
	if err != nil {
		return 0, 0, err
	}
	return totalTickets, activeOrders, nil
}

func (r *eventRepo) GetAllEventsWithActiveCounts(ctx context.Context) ([]EventWithActiveCount, error) {
	query := `
		SELECT 
			e.id, 
			e.total_tickets,
			COUNT(o.id) FILTER (WHERE o.status IN ($1, $2)) AS active_orders
		FROM events e
		LEFT JOIN orders o ON e.id = o.event_id
		GROUP BY e.id, e.total_tickets
	`
	rows, err := r.db.Pool.Query(ctx, query, models.OrderStatusPendingPayment, models.OrderStatusPaid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []EventWithActiveCount
	for rows.Next() {
		var item EventWithActiveCount
		if err := rows.Scan(&item.EventID, &item.TotalTickets, &item.ActiveOrders); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, nil
}
