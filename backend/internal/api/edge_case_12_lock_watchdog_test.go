package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestEdgeCase12_DistributedLockWatchdog directly verifies:
// 1. Lock Auto-Renewal: Task takes 2.2s (2.2x longer than initial 1s TTL), Watchdog automatically renews lease.
// 2. Split-Brain Prevention: Competitor cannot acquire lock midway at 1.8s (proves lock did NOT expire).
// 3. Clean Release: Owner token release unlocks key, allowing competitor to acquire immediately after.
func TestEdgeCase12_DistributedLockWatchdog(t *testing.T) {
	_, db, redisClient, producer := setupTestApp(t)
	defer db.Close()
	defer redisClient.Close()
	defer producer.Close()

	ctx := context.Background()
	lockKey := fmt.Sprintf("flashpass:lock:watchdog-test-%s", uuid.New().String())

	// 1. Goroutine A acquires lock with initial short TTL (1 second)
	lockA, acquired, err := redisClient.AcquireLockWithWatchdog(ctx, lockKey, 1*time.Second)
	if err != nil || !acquired {
		t.Fatalf("Thread A failed to acquire lock: %v", err)
	}

	// 2. Goroutine A simulates slow task taking 2.2s (longer than initial 1s TTL)
	// Watchdog background heartbeat automatically renews lease every 333ms.
	time.Sleep(1800 * time.Millisecond) // 1.8s elapsed (without watchdog, lock would have expired at 1.0s)

	// 3. Goroutine B attempts to acquire lock at 1.8s
	acquiredB, _ := redisClient.AcquireLock(ctx, lockKey, 1*time.Second)
	if acquiredB {
		t.Fatalf("Split-Brain detected! Thread B acquired lock while Thread A was still actively executing!")
	}
	t.Logf("[Edge Case 12 Verified] Watchdog successfully kept lock alive at 1.8s! Split-Brain prevented!")

	// 4. Goroutine A finishes task (at 2.2s) and releases lock with owner verification
	time.Sleep(400 * time.Millisecond)
	if err := lockA.Unlock(ctx); err != nil {
		t.Fatalf("Failed to unlock: %v", err)
	}

	// 5. Immediately after Goroutine A unlocks, Goroutine B retries and succeeds
	acquiredFinal, err := redisClient.AcquireLock(ctx, lockKey, 1*time.Second)
	if err != nil || !acquiredFinal {
		t.Fatalf("Thread B failed to acquire lock after Thread A unlocked!")
	}
	_ = redisClient.ReleaseLock(ctx, lockKey)
	t.Logf("[Edge Case 12 Verified] Safe owner-verified unlock confirmed: Clean handoff to next worker!")
}
