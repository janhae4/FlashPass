package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"flashpass/backend/internal/api"
	"flashpass/backend/internal/config"
	"flashpass/backend/internal/consumer"
	"flashpass/backend/internal/database"
	"flashpass/backend/internal/rabbitmq"
	"flashpass/backend/internal/redis"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	db, err := database.New(ctx, cfg)
	if err != nil {
		log.Fatalf("[Server FATAL] Database connection failed: %v", err)
	}
	defer db.Close()

	migrator := database.NewMigrator(db)
	if err := migrator.Up(ctx); err != nil {
		log.Printf("[Server WARNING] Auto-migration check: %v", err)
	}

	redisClient, err := redis.New(ctx, cfg)
	if err != nil {
		log.Fatalf("[Server FATAL] Redis connection failed: %v", err)
	}
	defer redisClient.Close()

	producer, err := rabbitmq.NewProducer(cfg)
	if err != nil {
		log.Fatalf("[Server FATAL] RabbitMQ producer failed: %v", err)
	}
	defer producer.Close()

	consumerService := consumer.NewService(db, redisClient, producer)
	if err := consumerService.Start(ctx); err != nil {
		log.Printf("[Server WARNING] Failed to start background consumers: %v", err)
	}

	app := fiber.New(fiber.Config{
		ServerHeader: "FlashPass-Engine/1.0",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  30 * time.Second,
		Concurrency:  256 * 1024,
	})

	handler := api.NewHandler(db, redisClient, producer)
	api.SetupRouter(app, handler)

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		addr := fmt.Sprintf(":%s", cfg.Port)
		log.Printf("[Server] FlashPass API Engine listening on %s", addr)
		if err := app.Listen(addr); err != nil {
			log.Printf("[Server] Server stopped: %v", err)
		}
	}()

	<-stopChan
	log.Println("[Server] Gracefully shutting down FlashPass API Engine...")
	_ = app.Shutdown()
	log.Println("[Server] Server exited cleanly.")
}
