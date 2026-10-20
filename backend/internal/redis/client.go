package redis

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"flashpass/backend/internal/config"
)

type Client struct {
	rdb *redis.Client
}

// New creates a new Redis client with explicit connection pooling and preloaded Lua scripts
func New(ctx context.Context, cfg *config.Config) (*Client, error) {
	var opts *redis.Options

	if cfg.RedisURL != "" {
		parsedOpts, err := redis.ParseURL(cfg.RedisURL)
		if err == nil {
			opts = parsedOpts
		}
	}

	if opts == nil {
		opts = &redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       0,
		}
	}

	// Explicit connection pool settings for high throughput (20k+ RPS)
	opts.PoolSize = cfg.RedisPoolSize
	opts.MinIdleConns = cfg.RedisMinIdleConns
	opts.DialTimeout = cfg.RedisDialTimeout
	opts.ReadTimeout = cfg.RedisReadTimeout
	opts.WriteTimeout = cfg.RedisWriteTimeout

	rdb := redis.NewClient(opts)

	// Verify connection
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	// Pre-load Lua scripts to cache SHA1 hashes in Redis memory
	if err := bookTicketScript.Load(ctx, rdb).Err(); err != nil {
		log.Printf("[Redis WARNING] Failed to preload bookTicketScript: %v", err)
	}
	if err := rollbackStockScript.Load(ctx, rdb).Err(); err != nil {
		log.Printf("[Redis WARNING] Failed to preload rollbackStockScript: %v", err)
	}

	log.Printf("[Redis] Connected successfully to Redis (pool_size=%d, min_idle=%d)",
		opts.PoolSize, opts.MinIdleConns)

	return &Client{rdb: rdb}, nil
}

// RDB returns the underlying go-redis client
func (c *Client) RDB() *redis.Client {
	return c.rdb
}

// Ping checks if Redis is responsive
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
//   1: Success
//  -1: Sold out
//  -2: User already booked
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
	pattern := fmt.Sprintf("%s%s:*", PrefixUserBooked, eventID)
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
