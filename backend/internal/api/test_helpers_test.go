package api

import (
	"context"
	"testing"

	"github.com/gofiber/fiber/v2"
	"flashpass/backend/internal/config"
	"flashpass/backend/internal/database"
	"flashpass/backend/internal/rabbitmq"
	"flashpass/backend/internal/redis"
)

// setupTestApp initializes test connections and builds a Fiber test app.
func setupTestApp(t *testing.T) (*fiber.App, *database.DB, *redis.Client, *rabbitmq.Producer) {
	t.Helper()
	cfg := config.Load()
	ctx := context.Background()

	db, err := database.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	migrator := database.NewMigrator(db)
	if err := migrator.Up(ctx); err != nil {
		t.Logf("Migration check (non-fatal): %v", err)
	}

	redisClient, err := redis.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to connect to Redis: %v", err)
	}

	producer, err := rabbitmq.NewProducer(cfg)
	if err != nil {
		t.Fatalf("Failed to create RabbitMQ producer: %v", err)
	}

	app := fiber.New()
	handler := NewHandler(db, redisClient, producer)
	SetupRouter(app, handler)

	return app, db, redisClient, producer
}
