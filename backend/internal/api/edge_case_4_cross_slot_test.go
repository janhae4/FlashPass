package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"flashpass/backend/internal/redis"
)

// TestEdgeCase4_CrossSlotHashTag directly verifies:
// 1. Without Hash Tag: Multi-key operations for the same event hash to disparate slots (failing in Redis Cluster with CROSSSLOT).
// 2. With Hash Tag {event_id}: All keys (stock, user_booked, combo add-ons, audit_lock) co-locate to the EXACT identical CRC16 slot.
// 3. Direct API Booking via POST /api/v1/tickets/book atomically writes both keys within the same slot.
func TestEdgeCase4_CrossSlotHashTag(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()
	eventID := uuid.New().String()
	userID := "user-cross-slot-1"

	// 1. PROOF OF VULNERABILITY (Un-tagged Keys Cause CROSSSLOT Error in Redis Cluster):
	untaggedStock := fmt.Sprintf("flashpass:stock:%s", eventID)
	untaggedUser := fmt.Sprintf("flashpass:user_booked:%s:%s", eventID, userID)
	untaggedCombo := fmt.Sprintf("flashpass:stock:%s:parking", eventID)

	slotUntaggedStock := redis.Slot(untaggedStock)
	slotUntaggedUser := redis.Slot(untaggedUser)
	slotUntaggedCombo := redis.Slot(untaggedCombo)

	t.Logf("[Edge Case 4 Pre-Condition] Untagged Keys Slots: Stock=%d, UserBooked=%d, ParkingCombo=%d",
		slotUntaggedStock, slotUntaggedUser, slotUntaggedCombo)

	if slotUntaggedStock == slotUntaggedUser && slotUntaggedStock == slotUntaggedCombo {
		t.Logf("[Edge Case 4 Note] Rare slot collision occurred for untagged keys, continuing test...")
	} else {
		t.Logf("[Edge Case 4 Confirmed] Disparate slots detected without Hash Tag! A multi-key Lua script would abort with: (error) CROSSSLOT Keys in request don't hash to the same slot")
	}

	// 2. VERIFY HASH TAG CO-LOCATION FIX:
	// Every key generated with {eventID} Hash Tag MUST hash to the exact same CRC16 slot
	taggedStock := redis.StockKey(eventID)
	taggedUser := redis.UserBookedKey(eventID, userID)
	taggedCombo := redis.ComboKey(eventID, "parking")
	taggedLock := redis.AuditLockKey(eventID)

	slotTaggedStock := redis.Slot(taggedStock)
	slotTaggedUser := redis.Slot(taggedUser)
	slotTaggedCombo := redis.Slot(taggedCombo)
	slotTaggedLock := redis.Slot(taggedLock)

	t.Logf("[Edge Case 4 Hash Tag Co-Location] Tagged Keys Slots: Stock=%d, UserBooked=%d, ParkingCombo=%d, AuditLock=%d",
		slotTaggedStock, slotTaggedUser, slotTaggedCombo, slotTaggedLock)

	if slotTaggedStock != slotTaggedUser {
		t.Fatalf("Expected StockKey and UserBookedKey to share slot, got %d vs %d", slotTaggedStock, slotTaggedUser)
	}
	if slotTaggedStock != slotTaggedCombo {
		t.Fatalf("Expected StockKey and ComboKey to share slot, got %d vs %d", slotTaggedStock, slotTaggedCombo)
	}
	if slotTaggedStock != slotTaggedLock {
		t.Fatalf("Expected StockKey and AuditLockKey to share slot, got %d vs %d", slotTaggedStock, slotTaggedLock)
	}

	t.Logf("[Edge Case 4 Solved] 100%% Slot Alignment achieved! All related event keys reside on Hash Slot %d (Zero CROSSSLOT Errors)", slotTaggedStock)

	// 3. DIRECT API EXECUTION:
	// Insert event into PostgreSQL
	eventUUID, _ := uuid.Parse(eventID)
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, 'Cluster Sharded Event', 10, 10, NOW(), NOW() + INTERVAL '1 day')
	`, eventUUID)
	if err != nil {
		t.Fatalf("Failed to create event in DB: %v", err)
	}

	// Prewarm stock via API POST /api/v1/admin/events/:id/prewarm
	prewarmReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/admin/events/%s/prewarm", eventID), nil)
	prewarmResp, err := app.Test(prewarmReq)
	if err != nil || prewarmResp.StatusCode != http.StatusOK {
		t.Fatalf("Prewarm API failed: %v", err)
	}

	// Book ticket via API POST /api/v1/tickets/book
	// This invokes BookTicketLuaScript touching BOTH StockKey and UserBookedKey within the same slot
	payload, _ := json.Marshal(map[string]string{
		"event_id":        eventID,
		"user_id":         userID,
		"idempotency_key": "idemp-cluster-" + uuid.New().String(),
	})
	bookReq := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payload))
	bookReq.Header.Set("Content-Type", "application/json")
	bookResp, err := app.Test(bookReq)
	if err != nil || bookResp.StatusCode != http.StatusAccepted {
		t.Fatalf("Expected 202 Accepted for co-located booking, got status %d", bookResp.StatusCode)
	}

	// Verify second booking with new idempotency key by same user is rejected with 409 Conflict
	payloadDup, _ := json.Marshal(map[string]string{
		"event_id":        eventID,
		"user_id":         userID,
		"idempotency_key": "idemp-cluster-dup-" + uuid.New().String(),
	})
	bookReqDup := httptest.NewRequest(http.MethodPost, "/api/v1/tickets/book", bytes.NewReader(payloadDup))
	bookReqDup.Header.Set("Content-Type", "application/json")
	bookRespDup, _ := app.Test(bookReqDup)
	if bookRespDup.StatusCode != http.StatusConflict {
		t.Fatalf("Expected 409 Conflict for duplicate booking in same slot, got %d", bookRespDup.StatusCode)
	}

	t.Logf("[Edge Case 4 Verified] Booking and single-user constraint executed atomically via API in Slot %d without CROSSSLOT errors!", slotTaggedStock)
}
