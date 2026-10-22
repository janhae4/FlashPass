package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestAdminPrewarmAndStockInspection(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	// Create test event in DB with 50 tickets
	eventID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, $2, $3, $4, NOW(), NOW() + INTERVAL '7 days')
	`, eventID, "Prewarm Test Concert", 50, 50)
	if err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}
	defer func() {
		_, _ = db.Pool.Exec(ctx, "DELETE FROM events WHERE id = $1", eventID)
	}()

	// 1. Call Pre-warm Endpoint
	prewarmReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/events/"+eventID.String()+"/prewarm", nil)
	prewarmResp, err := app.Test(prewarmReq)
	if err != nil {
		t.Fatalf("Prewarm request failed: %v", err)
	}
	if prewarmResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(prewarmResp.Body)
		t.Fatalf("Expected prewarm status 200, got %d. Body: %s", prewarmResp.StatusCode, string(body))
	}

	// 2. Call Stock Endpoint
	stockReq := httptest.NewRequest(http.MethodGet, "/api/v1/events/"+eventID.String()+"/stock", nil)
	stockResp, err := app.Test(stockReq)
	if err != nil {
		t.Fatalf("Get stock request failed: %v", err)
	}
	if stockResp.StatusCode != http.StatusOK {
		t.Fatalf("Expected stock status 200, got %d", stockResp.StatusCode)
	}

	var stockData struct {
		EventID string `json:"event_id"`
		Stock   int    `json:"stock"`
	}
	_ = json.NewDecoder(stockResp.Body).Decode(&stockData)
	if stockData.Stock != 50 {
		t.Errorf("Expected stock 50, got %d", stockData.Stock)
	}
}
