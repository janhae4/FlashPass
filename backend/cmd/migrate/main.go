package main

import (
	"context"
	"flag"
	"log"
	"os"

	"flashpass/backend/internal/config"
	"flashpass/backend/internal/database"
)

func main() {
	cmd := flag.String("cmd", "up", "Migration command: up or down")
	flag.Parse()

	if len(os.Args) > 1 && (os.Args[1] == "up" || os.Args[1] == "down") {
		*cmd = os.Args[1]
	}

	cfg := config.Load()
	ctx := context.Background()

	db, err := database.New(ctx, cfg)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer db.Close()

	migrator := database.NewMigrator(db)

	switch *cmd {
	case "up":
		log.Println("[Migration CLI] Running UP migrations...")
		if err := migrator.Up(ctx); err != nil {
			log.Fatalf("Migration UP failed: %v", err)
		}
		log.Println("[Migration CLI] UP migrations finished successfully.")
	case "down":
		log.Println("[Migration CLI] Running DOWN migration...")
		if err := migrator.Down(ctx); err != nil {
			log.Fatalf("Migration DOWN failed: %v", err)
		}
		log.Println("[Migration CLI] DOWN migration finished successfully.")
	default:
		log.Fatalf("Unknown command: %s. Use 'up' or 'down'.", *cmd)
	}
}
