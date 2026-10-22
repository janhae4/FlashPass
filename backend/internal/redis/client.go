package redis

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"flashpass/backend/internal/config"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// StockCache abstracts Redis inventory and idempotency operations
type StockCache interface {
	BookTicket(ctx context.Context, eventID, userID string, quantity int) (int64, error)
	RollbackStock(ctx context.Context, eventID, userID string, quantity int) error
	ClearUserBooked(ctx context.Context, eventID, userID string) error
	PrewarmStock(ctx context.Context, eventID string, stock int) error
	GetStock(ctx context.Context, eventID string) (int, error)
	CheckIdempotency(ctx context.Context, idempotencyKey string) (string, bool, error)
	SetIdempotency(ctx context.Context, idempotencyKey string, orderID string, ttl time.Duration) error
	IsUserBooked(ctx context.Context, eventID, userID string) (bool, error)
	ResetEvent(ctx context.Context, eventID string, stock int) error
	IsMasterAlive(ctx context.Context) bool
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, key string) error
	AcquireLockWithWatchdog(ctx context.Context, key string, ttl time.Duration) (*WatchdogLock, bool, error)
	CheckRateLimit(ctx context.Context, identifier string, limit int, window time.Duration) (bool, int, error)
	Ping(ctx context.Context) error
	Close() error
	RDB() *redis.Client
}

type Client struct {
	rdb *redis.Client
	cfg *config.Config
}

var _ StockCache = (*Client)(nil)

func New(ctx context.Context, cfg *config.Config) (*Client, error) {
	sentinelAddr := fmt.Sprintf("%s:%s", cfg.RedisSentinelHost, cfg.RedisSentinelPort)
	masterHostPort := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
	replicaHostPort := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisReplicaPort)

	opts := &redis.FailoverOptions{
		MasterName: "mymaster",
		SentinelAddrs: []string{
			sentinelAddr,
		},
		Password:     cfg.RedisPassword,
		DB:           0,
		PoolSize:     cfg.RedisPoolSize,
		MinIdleConns: cfg.RedisMinIdleConns,
		DialTimeout:  cfg.RedisDialTimeout,
		ReadTimeout:  cfg.RedisReadTimeout,
		WriteTimeout: cfg.RedisWriteTimeout,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.HasPrefix(addr, "redis:") {
				addr = masterHostPort
			} else if strings.HasPrefix(addr, "redis_replica:") {
				addr = replicaHostPort
			}
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
		RouteRandomly: false,
	}
	rdb := redis.NewFailoverClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("failed to ping redis master: %w", err)
	}

	if err := bookTicketScript.Load(ctx, rdb).Err(); err != nil {
		log.Printf("[Redis WARNING] Failed to preload bookTicketScript on master: %v", err)
	}
	if err := rollbackStockScript.Load(ctx, rdb).Err(); err != nil {
		log.Printf("[Redis WARNING] Failed to preload rollbackStockScript on master: %v", err)
	}

	log.Printf("[Redis] Connected successfully to Master (:6379) and Replica (:6380)")

	return &Client{rdb: rdb, cfg: cfg}, nil
}

func (c *Client) RDB() *redis.Client {
	return c.rdb
}

func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Close gracefully closes the Redis client connection pool
func (c *Client) Close() error {
	if c.rdb != nil {
		log.Println("[Redis] Closing connection pool.")
		return c.rdb.Close()
	}
	return nil
}

// BookTicket executes the atomic inventory decrement script
// Returns:
//
//	 1: Success
//	-1: Sold out
//	-2: User already booked
func (c *Client) BookTicket(ctx context.Context, eventID, userID string, quantity int) (int64, error) {
	stockKey := StockKey(eventID)
	userKey := UserBookedKey(eventID, userID)

	keys := []string{stockKey, userKey}
	args := []interface{}{userID, quantity}

	res, err := bookTicketScript.Run(ctx, c.rdb, keys, args...).Int64()
	if err != nil {
		return 0, fmt.Errorf("failed to execute bookTicketScript: %w", err)
	}

	return res, nil
}

// RollbackStock executes atomic inventory increment and clears user booked flag
func (c *Client) RollbackStock(ctx context.Context, eventID, userID string, quantity int) error {
	stockKey := StockKey(eventID)
	userKey := UserBookedKey(eventID, userID)

	keys := []string{stockKey, userKey}
	args := []interface{}{quantity}

	_, err := rollbackStockScript.Run(ctx, c.rdb, keys, args...).Result()
	if err != nil {
		return fmt.Errorf("failed to execute rollbackStockScript: %w", err)
	}

	return nil
}

// ClearUserBooked removes the user's booking lock key in Redis
func (c *Client) ClearUserBooked(ctx context.Context, eventID, userID string) error {
	key := UserBookedKey(eventID, userID)
	return c.rdb.Del(ctx, key).Err()
}

// PrewarmStock loads event stock into Redis
func (c *Client) PrewarmStock(ctx context.Context, eventID string, stock int) error {
	key := StockKey(eventID)
	err := c.rdb.Set(ctx, key, stock, 0).Err()
	if err != nil {
		return fmt.Errorf("failed to prewarm stock for event %s: %w", eventID, err)
	}
	log.Printf("[Redis] Prewarmed event %s with %d tickets", eventID, stock)
	return nil
}

// GetStock returns the current live stock count from Redis
func (c *Client) GetStock(ctx context.Context, eventID string) (int, error) {
	key := StockKey(eventID)
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get stock for event %s: %w", eventID, err)
	}

	stock, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid stock value in redis: %w", err)
	}

	return stock, nil
}

// CheckIdempotency checks if an idempotency key was already processed
// Returns (reservationToken/orderID, exists, error)
func (c *Client) CheckIdempotency(ctx context.Context, idempotencyKey string) (string, bool, error) {
	key := IdempotencyKey(idempotencyKey)
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to check idempotency key: %w", err)
	}
	return val, true, nil
}

// SetIdempotency caches the order reservation token with TTL (default: 30 seconds)
func (c *Client) SetIdempotency(ctx context.Context, idempotencyKey string, orderID string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	key := IdempotencyKey(idempotencyKey)
	return c.rdb.Set(ctx, key, orderID, ttl).Err()
}

// IsUserBooked checks if user has an active booking for this event
func (c *Client) IsUserBooked(ctx context.Context, eventID, userID string) (bool, error) {
	key := UserBookedKey(eventID, userID)
	exists, err := c.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check user booked key: %w", err)
	}
	return exists == 1, nil
}

// ResetEvent clears Redis stock and all user booked keys for an event (useful for resets / load testing)
func (c *Client) ResetEvent(ctx context.Context, eventID string, stock int) error {
	// Set stock
	if err := c.PrewarmStock(ctx, eventID, stock); err != nil {
		return err
	}

	// Delete user booked keys matching pattern
	pattern := UserBookedPattern(eventID)
	iter := c.rdb.Scan(ctx, 0, pattern, 0).Iterator()
	var keysToDelete []string
	for iter.Next(ctx) {
		keysToDelete = append(keysToDelete, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("scan error during reset: %w", err)
	}

	if len(keysToDelete) > 0 {
		if err := c.rdb.Del(ctx, keysToDelete...).Err(); err != nil {
			return fmt.Errorf("failed to delete booked keys: %w", err)
		}
	}

	return nil
}

// IsMasterAlive checks if Redis master responds to PING within 500ms
func (c *Client) IsMasterAlive(ctx context.Context) bool {
	pingCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	return c.rdb.Ping(pingCtx).Err() == nil
}

// AcquireLock attempts to acquire a distributed lock with TTL
func (c *Client) AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return c.rdb.SetNX(ctx, key, "1", ttl).Result()
}

func (c *Client) ReleaseLock(ctx context.Context, key string) error {
	return c.rdb.Del(ctx, key).Err()
}

func (c *Client) CheckRateLimit(ctx context.Context, identifier string, limit int, window time.Duration) (bool, int, error) {
	key := RateLimitKey(identifier)
	now := time.Now().UnixMilli()
	windowMs := window.Milliseconds()
	member := fmt.Sprintf("%d:%s", now, uuid.New().String())
	ttlSeconds := int(window.Seconds()) + 1
	ttlSeconds = max(ttlSeconds, 1)
	res, err := slidingWindowRateLimitScript.Run(ctx, c.rdb, []string{key}, now, windowMs, limit, member, ttlSeconds).Slice()
	if err != nil {
		return false, 0, fmt.Errorf("Rate limit!")
	}
	if len(res) < 2 {
		return false, 0, fmt.Errorf("Unexpected Rate limit return")
	}

	allowed, _ := res[0].(int64)
	remaining, _ := res[1].(int64)
	return allowed == 1, int(remaining), nil
}

type WatchdogLock struct {
	Key    string
	Owner  string
	TTL    time.Duration
	cancel context.CancelFunc
	done   chan struct{}
	client *Client
}

// Unlock stops the watchdog heartbeat and releases the lock safely with owner verification
func (l *WatchdogLock) Unlock(ctx context.Context) error {
	if l.cancel != nil {
		l.cancel()
	}
	if l.done != nil {
		<-l.done
	}
	return l.client.releaseLockWithOwner(ctx, l.Key, l.Owner)
}

func (c *Client) releaseLockWithOwner(ctx context.Context, key, owner string) error {
	return releaseLockWithOwnerScript.Run(ctx, c.rdb, []string{key}, owner).Err()
}

// AcquireLockWithWatchdog acquires a distributed lock and spawns a background goroutine to auto-renew TTL
func (c *Client) AcquireLockWithWatchdog(ctx context.Context, key string, ttl time.Duration) (*WatchdogLock, bool, error) {
	owner := uuid.New().String()
	ttlSeconds := int(ttl.Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	ok, err := c.rdb.SetNX(ctx, key, owner, ttl).Result()
	if err != nil || !ok {
		return nil, false, err
	}

	watchdogCtx, cancel := context.WithCancel(context.Background())
	lock := &WatchdogLock{
		Key:    key,
		Owner:  owner,
		TTL:    ttl,
		cancel: cancel,
		done:   make(chan struct{}),
		client: c,
	}

	renewInterval := ttl / 3
	if renewInterval < 100*time.Millisecond {
		renewInterval = 100 * time.Millisecond
	}

	go func() {
		defer close(lock.done)
		ticker := time.NewTicker(renewInterval)
		defer ticker.Stop()

		for {
			select {
			case <-watchdogCtx.Done():
				return
			case <-ticker.C:
				_ = renewLockScript.Run(watchdogCtx, c.rdb, []string{key}, owner, ttlSeconds).Err()
			}
		}
	}()

	return lock, true, nil
}
