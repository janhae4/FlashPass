package redis

import "fmt"

const (
	PrefixStock      = "flashpass:stock:"
	PrefixIdempotent = "flashpass:idempotency:"
	PrefixUserBooked = "flashpass:user_booked:"
)

// StockKey returns the Redis key for remaining event stock
func StockKey(eventID string) string {
	return fmt.Sprintf("%s%s", PrefixStock, eventID)
}

// IdempotencyKey returns the Redis key for request idempotency tracking
func IdempotencyKey(key string) string {
	return fmt.Sprintf("%s%s", PrefixIdempotent, key)
}

// UserBookedKey returns the Redis key to enforce 1 ticket per user
func UserBookedKey(eventID, userID string) string {
	return fmt.Sprintf("%s%s:%s", PrefixUserBooked, eventID, userID)
}
