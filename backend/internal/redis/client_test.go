package redis

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"flashpass/backend/internal/config"
)

func TestRedisClientAndLuaScripts(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()

	client, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	testEventID := "test-event-" + uuid.New().String()[:8]
	initialStock := 5

	// 1. Test PrewarmStock & GetStock
	if err := client.PrewarmStock(ctx, testEventID, initialStock); err != nil {
		t.Fatalf("PrewarmStock failed: %v", err)
	}

	stock, err := client.GetStock(ctx, testEventID)
	if err != nil {
		t.Fatalf("GetStock failed: %v", err)
	}
	if stock != initialStock {
		t.Errorf("Expected stock %d, got %d", initialStock, stock)
	}

	// 2. Test BookTicket - Success
	user1 := "user-1-" + uuid.New().String()[:6]
	res, err := client.BookTicket(ctx, testEventID, user1, 1)
	if err != nil {
		t.Fatalf("BookTicket user1 failed: %v", err)
	}
	if res != ResultSuccess {
		t.Errorf("Expected ResultSuccess (1), got %d", res)
	}

	// Verify stock decremented to 4
	stock, err = client.GetStock(ctx, testEventID)
	if err != nil || stock != 4 {
		t.Errorf("Expected stock 4 after 1 booking, got %d (err: %v)", stock, err)
	}

	// 3. Test BookTicket - Duplicate booking for same user
	res, err = client.BookTicket(ctx, testEventID, user1, 1)
	if err != nil {
		t.Fatalf("Duplicate BookTicket failed with error: %v", err)
	}
	if res != ResultAlreadyBooked {
		t.Errorf("Expected ResultAlreadyBooked (-2), got %d", res)
	}

	// Verify stock is still 4
	stock, _ = client.GetStock(ctx, testEventID)
	if stock != 4 {
		t.Errorf("Expected stock 4 after duplicate attempt, got %d", stock)
	}

	// 4. Test BookTicket - Exhaust stock
	for i := 2; i <= 5; i++ {
		user := "user-" + uuid.New().String()[:6]
		r, err := client.BookTicket(ctx, testEventID, user, 1)
		if err != nil {
			t.Fatalf("BookTicket user %d failed: %v", i, err)
		}
		expected := ResultSuccess
		if i == 5 {
			expected = ResultSuccessHitZero
		}
		if r != expected {
			t.Errorf("Expected %d for user %d, got %d", expected, i, r)
		}
	}

	// Verify stock is now 0
	stock, _ = client.GetStock(ctx, testEventID)
	if stock != 0 {
		t.Errorf("Expected stock 0 after exhausting, got %d", stock)
	}

	// 5. Test BookTicket - Sold Out
	user6 := "user-6-" + uuid.New().String()[:6]
	res, err = client.BookTicket(ctx, testEventID, user6, 1)
	if err != nil {
		t.Fatalf("BookTicket when sold out failed with error: %v", err)
	}
	if res != ResultSoldOut {
		t.Errorf("Expected ResultSoldOut (-1), got %d", res)
	}

	// 6. Test RollbackStock (Compensation)
	// Rollback user1's reservation
	if err := client.RollbackStock(ctx, testEventID, user1, 1); err != nil {
		t.Fatalf("RollbackStock failed: %v", err)
	}

	// Stock should now be 1
	stock, _ = client.GetStock(ctx, testEventID)
	if stock != 1 {
		t.Errorf("Expected stock 1 after rollback, got %d", stock)
	}

	// User1 should now be able to book again since booked key was cleared
	res, err = client.BookTicket(ctx, testEventID, user1, 1)
	if err != nil {
		t.Fatalf("BookTicket user1 after rollback failed: %v", err)
	}
	if res != ResultSuccessHitZero {
		t.Errorf("Expected user1 to re-book successfully with ResultSuccessHitZero, got %d", res)
	}

	// 7. Test Idempotency Key
	idempotencyKey := "idemp-" + uuid.New().String()
	token, exists, err := client.CheckIdempotency(ctx, idempotencyKey)
	if err != nil {
		t.Fatalf("CheckIdempotency error: %v", err)
	}
	if exists {
		t.Errorf("Expected idempotency key not to exist initially")
	}

	orderID := uuid.New().String()
	if err := client.SetIdempotency(ctx, idempotencyKey, orderID, 2*time.Second); err != nil {
		t.Fatalf("SetIdempotency error: %v", err)
	}

	token, exists, err = client.CheckIdempotency(ctx, idempotencyKey)
	if err != nil || !exists || token != orderID {
		t.Errorf("Expected idempotency key to match %s, got %s (exists=%v, err=%v)", orderID, token, exists, err)
	}

	// 8. Test ResetEvent
	if err := client.ResetEvent(ctx, testEventID, 100); err != nil {
		t.Fatalf("ResetEvent failed: %v", err)
	}
	stock, _ = client.GetStock(ctx, testEventID)
	if stock != 100 {
		t.Errorf("Expected stock 100 after reset, got %d", stock)
	}
}
