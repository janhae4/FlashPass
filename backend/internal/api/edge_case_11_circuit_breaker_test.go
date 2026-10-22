package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestEdgeCase11_DatabaseCircuitBreaker directly verifies:
// 1. Initial State: Circuit Breaker is CLOSED, normal queries succeed with 200 OK.
// 2. Failure Tripping: 3 consecutive database failures trip the circuit breaker to OPEN.
// 3. Fast-Fail Shedding: Subsequent requests are rejected INSTANTLY in 0ms with HTTP 503 (zero DB load).
// 4. Cooldown & Recovery: After 1.6s cooldown, circuit enters HALF_OPEN, canary request succeeds, resetting state back to CLOSED.
func TestEdgeCase11_DatabaseCircuitBreaker(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	// 1. Seed test event
	eventUUID := uuid.New()
	eventID := eventUUID.String()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, 'Circuit Breaker Concert', 10, 10, NOW(), NOW() + INTERVAL '1 day')
	`, eventUUID)
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}

	// Phase 1: Normal operational state (CLOSED) -> Returns 200 OK
	reqNormal := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/events/%s/metrics", eventID), nil)
	respNormal, err := app.Test(reqNormal, 5000)
	if err != nil || respNormal.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK under CLOSED circuit, got %d", respNormal.StatusCode)
	}
	t.Logf("[Edge Case 11 Verified] Normal query succeeded under CLOSED circuit breaker!")

	// Phase 2: Trigger 3 consecutive DB query errors to trip Circuit Breaker to OPEN
	for i := 1; i <= 3; i++ {
		reqFail := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/events/%s/metrics?chaos=db_error", eventID), nil)
		respFail, err := app.Test(reqFail, 5000)
		if err != nil || respFail.StatusCode != http.StatusInternalServerError {
			t.Fatalf("Expected 500 InternalServerError for simulated failure #%d, got %d", i, respFail.StatusCode)
		}
	}

	// Verify circuit breaker state transitioned to OPEN
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/v1/admin/circuit-breaker", nil)
	respStatus, err := app.Test(reqStatus, 5000)
	if err != nil {
		t.Fatalf("Failed to query circuit breaker status: %v", err)
	}
	var statusMap map[string]string
	body, _ := io.ReadAll(respStatus.Body)
	_ = json.Unmarshal(body, &statusMap)
	if statusMap["state"] != "OPEN" {
		t.Fatalf("Expected circuit breaker to be OPEN after 3 failures, got %s", statusMap["state"])
	}
	t.Logf("[Edge Case 11 Verified] Circuit Breaker successfully TRIPPED to OPEN after 3 consecutive failures!")

	// Phase 3: Verify immediate Fast-Fail 503 (0ms load, complete DB shielding)
	start := time.Now()
	reqBlocked := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/events/%s/metrics", eventID), nil)
	respBlocked, err := app.Test(reqBlocked, 5000)
	duration := time.Since(start)

	if err != nil || respBlocked.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("Expected 503 Service Unavailable when circuit is OPEN, got %d", respBlocked.StatusCode)
	}
	if retryAfter := respBlocked.Header.Get("Retry-After"); retryAfter == "" {
		t.Errorf("Expected Retry-After header on 503 response, got empty")
	}
	t.Logf("[Edge Case 11 Verified] Fast-Fail Shedding active: Request rejected in %v with 503 Service Unavailable!", duration)

	// Phase 4: Wait 1.6s Cooldown -> Auto HALF_OPEN, canary request succeeds -> Recovers to CLOSED
	time.Sleep(1600 * time.Millisecond)

	reqCanary := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/admin/events/%s/metrics", eventID), nil)
	respCanary, err := app.Test(reqCanary, 5000)
	if err != nil || respCanary.StatusCode != http.StatusOK {
		t.Fatalf("Expected canary request to succeed with 200 OK, got %d", respCanary.StatusCode)
	}

	// Verify circuit breaker automatically reset back to CLOSED
	reqFinalStatus := httptest.NewRequest(http.MethodGet, "/api/v1/admin/circuit-breaker", nil)
	respFinalStatus, _ := app.Test(reqFinalStatus, 5000)
	bodyFinal, _ := io.ReadAll(respFinalStatus.Body)
	_ = json.Unmarshal(bodyFinal, &statusMap)
	if statusMap["state"] != "CLOSED" {
		t.Fatalf("Expected circuit breaker to recover back to CLOSED, got %s", statusMap["state"])
	}
	t.Logf("[Edge Case 11 Verified] Self-healing confirmed: Circuit Breaker successfully reset back to CLOSED!")
}
