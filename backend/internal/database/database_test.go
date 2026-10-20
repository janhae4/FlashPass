package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"flashpass/backend/internal/config"
	"flashpass/backend/internal/models"
)

func TestDatabaseAndMigrations(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()

	db, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	migrator := NewMigrator(db)

	// Test Up migrations
	if err := migrator.Up(ctx); err != nil {
		t.Fatalf("Migrator Up failed: %v", err)
	}

	// Verify applied migrations
	applied, err := migrator.GetAppliedMigrations(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrations failed: %v", err)
	}
	if len(applied) == 0 {
		t.Fatalf("Expected applied migrations to not be empty")
	}

	// Test inserting an event
	eventID := uuid.New()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, eventID, "Test Event DB", 100, 100, time.Now(), time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}

	// Test inserting an order with idempotency key
	orderID := uuid.New()
	idempotencyKey := "test-key-" + uuid.New().String()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, orderID, eventID, "user-test", idempotencyKey, models.OrderStatusPendingPayment, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("Failed to insert test order: %v", err)
	}

	// Test unique constraint on idempotency_key
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, uuid.New(), eventID, "user-test-2", idempotencyKey, models.OrderStatusPendingPayment, time.Now().Add(10*time.Minute))
	if err == nil {
		t.Fatalf("Expected duplicate idempotency_key insert to fail, but it succeeded")
	}

	// Clean up test rows
	_, _ = db.Pool.Exec(ctx, "DELETE FROM orders WHERE id = $1", orderID)
	_, _ = db.Pool.Exec(ctx, "DELETE FROM events WHERE id = $1", eventID)
}
