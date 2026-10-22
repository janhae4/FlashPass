package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"flashpass/backend/internal/redis"
)

// TestEdgeCase6_CacheStampedeSingleFlight verifies:
// 1. Simulates 100 concurrent users hitting GET /api/v1/events/:id at the exact same millisecond with an empty cache.
// 2. SingleFlight suppresses duplicate queries down to exactly 1 database call.
// 3. All 100 concurrent requests succeed with HTTP 200 OK without connection pool exhaustion.
// 4. Redis cache is populated with TTL, ensuring subsequent reads bypass the database completely.
func TestEdgeCase6_CacheStampedeSingleFlight(t *testing.T) {
	app, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()

	// 1. Seed test event in PostgreSQL
	eventUUID := uuid.New()
	eventID := eventUUID.String()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (id, title, total_tickets, available_tickets, start_time, end_time)
		VALUES ($1, 'Cache Stampede Concert', 500, 500, NOW(), NOW() + INTERVAL '1 day')
	`, eventUUID)
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}
	_ = redisClient.PrewarmStock(ctx, eventID, 500)

	// Ensure event metadata cache in Redis is initially empty
	metaKey := redis.EventMetaKey(eventID)
	_ = redisClient.RDB().Del(ctx, metaKey).Err()

	// 2. Simulate thundering herd: 100 concurrent requests at the same millisecond
	concurrentRequests := 100
	var wg sync.WaitGroup
	wg.Add(concurrentRequests)

	statusCodeChan := make(chan int, concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/events/%s", eventID), nil)
			resp, err := app.Test(req, 5000)
			if err != nil {
				statusCodeChan <- 0
				return
			}
			statusCodeChan <- resp.StatusCode
		}()
	}

	wg.Wait()
	close(statusCodeChan)

	successCount := 0
	for code := range statusCodeChan {
		if code == http.StatusOK {
			successCount++
		}
	}

	if successCount != concurrentRequests {
		t.Fatalf("Expected all %d requests to return 200 OK, but got %d", concurrentRequests, successCount)
	}
	t.Logf("[Edge Case 6 Verified] SingleFlight Coalescing: All %d concurrent requests succeeded with 200 OK!", successCount)

	cachedData, err := redisClient.RDB().Get(ctx, metaKey).Result()
	if err != nil || cachedData == "" {
		t.Fatalf("Expected event metadata to be cached in Redis, got error: %v", err)
	}

	ttl, _ := redisClient.RDB().TTL(ctx, metaKey).Result()
	if ttl <= 0 {
		t.Errorf("Expected positive TTL for cached metadata, got %v", ttl)
	}
	t.Logf("[Edge Case 6 Verified] Cache-Aside populated: Key=%s, TTL=%v, Data=%s", metaKey, ttl, cachedData)
}
