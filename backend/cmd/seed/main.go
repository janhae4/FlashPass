package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/google/uuid"
	"flashpass/backend/internal/config"
	"flashpass/backend/internal/database"
)

// Default sample event UUID for deterministic integration and load testing
const SampleEventID = "e9b8849b-7e61-4648-936c-941a329fa9c1"
const SampleEventTitle = "Taylor Swift: The Eras Tour - FlashPass VIP Experience"
const DefaultTotalTickets = 1000

func main() {
	reset := flag.Bool("reset", false, "Reset orders and restore event ticket inventory to 1,000")
	totalTickets := flag.Int("tickets", DefaultTotalTickets, "Number of tickets to seed for the event")
	flag.Parse()

	cfg := config.Load()
	ctx := context.Background()

	db, err := database.New(ctx, cfg)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer db.Close()

	// Ensure migrations are applied first
	migrator := database.NewMigrator(db)
	if err := migrator.Up(ctx); err != nil {
		log.Fatalf("Failed to run migrations before seed: %v", err)
	}

	eventUUID, err := uuid.Parse(SampleEventID)
	if err != nil {
		log.Fatalf("Invalid sample event UUID: %v", err)
	}

	now := time.Now()
	startTime := now.Add(-1 * time.Hour) // Started 1 hour ago (live sale)
	endTime := now.Add(7 * 24 * time.Hour) // Valid for 7 days

	if *reset {
		log.Printf("[Seed] Reset mode enabled: clearing existing orders for event %s...", SampleEventID)
		_, err := db.Pool.Exec(ctx, "DELETE FROM orders WHERE event_id = $1", eventUUID)
		if err != nil {
			log.Fatalf("Failed to clear existing orders: %v", err)
		}
	}

	// Upsert event
	query := `
	INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, NOW())
	ON CONFLICT (id) DO UPDATE 
	SET title = EXCLUDED.title,
	    total_tickets = EXCLUDED.total_tickets,
	    available_tickets = EXCLUDED.available_tickets,
	    start_time = EXCLUDED.start_time,
	    end_time = EXCLUDED.end_time;
	`

	_, err = db.Pool.Exec(ctx, query,
		eventUUID,
		SampleEventTitle,
		*totalTickets,
		*totalTickets,
		startTime,
		endTime,
	)
	if err != nil {
		log.Fatalf("Failed to seed event: %v", err)
	}

	var count int
	var title string
	var total, available int
	err = db.Pool.QueryRow(ctx, "SELECT title, total_tickets, available_tickets FROM events WHERE id = $1", eventUUID).
		Scan(&title, &total, &available)
	if err != nil {
		log.Fatalf("Failed to verify seeded event: %v", err)
	}

	_ = db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM events").Scan(&count)

	log.Println("================================================================================")
	log.Printf("[Seed SUCCESS] Sample Concert Event Seeded Successfully!")
	log.Printf("  - Event ID:           %s", SampleEventID)
	log.Printf("  - Title:              %s", title)
	log.Printf("  - Total Tickets:      %d", total)
	log.Printf("  - Available Tickets:  %d", available)
	log.Printf("  - Start Time:         %s", startTime.Format(time.RFC3339))
	log.Printf("  - End Time:           %s", endTime.Format(time.RFC3339))
	log.Printf("  - Total Events in DB: %d", count)
	log.Println("================================================================================")
}
