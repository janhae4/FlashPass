package redis

import (
	"github.com/redis/go-redis/v9"
)

// Script return codes
const (
	ResultSuccess       int64 = 1
	ResultSoldOut       int64 = -1
	ResultAlreadyBooked int64 = -2
)

// Atomic Lua script for inventory decrement and 1-per-user check
// Strictly adheres to FlashPass ARCHITECTURE.md
const BookTicketLuaScript = `
-- KEYS[1]: flashpass:stock:{event_id}
-- KEYS[2]: flashpass:user_booked:{event_id}:{user_id}
-- ARGV[1]: user_id
-- ARGV[2]: quantity (e.g., 1)

if redis.call('EXISTS', KEYS[2]) == 1 then
    return -2 -- Already booked by this user
end

local current_stock = tonumber(redis.call('GET', KEYS[1]) or "0")
local quantity = tonumber(ARGV[2])

if current_stock >= quantity then
    redis.call('DECRBY', KEYS[1], quantity)
    redis.call('SET', KEYS[2], "1", 'EX', 3600)
    return 1 -- Success
else
    return -1 -- Sold out
end
`

// Atomic Lua script for inventory rollback (used when 10-minute payment window expires)
const RollbackStockLuaScript = `
-- KEYS[1]: flashpass:stock:{event_id}
-- KEYS[2]: flashpass:user_booked:{event_id}:{user_id}
-- ARGV[1]: quantity (e.g., 1)

local quantity = tonumber(ARGV[1])
redis.call('INCRBY', KEYS[1], quantity)
redis.call('DEL', KEYS[2])
return 1
`

var (
	bookTicketScript   = redis.NewScript(BookTicketLuaScript)
	rollbackStockScript = redis.NewScript(RollbackStockLuaScript)
)
