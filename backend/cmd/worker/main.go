package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"flashpass/backend/internal/config"
	"flashpass/backend/internal/consumer"
	"flashpass/backend/internal/database"
	"flashpass/backend/internal/rabbitmq"
	"flashpass/backend/internal/redis"
)

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := database.New(ctx, cfg)
	if err != nil {
		log.Fatalf("[Worker FATAL] Database connection failed: %v", err)
	}
	defer db.Close()

	redisClient, err := redis.New(ctx, cfg)
	if err != nil {
		log.Fatalf("[Worker FATAL] Redis connection failed: %v", err)
	}
	defer redisClient.Close()

	producer, err := rabbitmq.NewProducer(cfg)
	if err != nil {
		log.Fatalf("[Worker FATAL] RabbitMQ producer failed: %v", err)
	}
	defer producer.Close()

	svc := consumer.NewService(db, redisClient, producer)
	if err := svc.Start(ctx); err != nil {
		log.Fatalf("[Worker FATAL] Consumer service failed to start: %v", err)
	}

	log.Println("[Worker] FlashPass Background Worker is actively listening for events...")

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
	<-stopChan

	log.Println("[Worker] Shutting down FlashPass Background Worker...")
	cancel()
	log.Println("[Worker] Exited cleanly.")
}
