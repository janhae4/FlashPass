package redis

import (
	"github.com/redis/go-redis/v9"
)

// Script return codes
const (
	ResultSuccess        int64 = 1
	ResultSuccessHitZero int64 = 2
	ResultSoldOut        int64 = -1
	ResultAlreadyBooked  int64 = -2
)

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
    local remaining = redis.call('DECRBY', KEYS[1], quantity)
    redis.call('SET', KEYS[2], "1", 'EX', 630) -- Align TTL to 10m30s reservation timeout
    if remaining == 0 then
        return 2 -- Success AND Hit Zero (last ticket reserved)
    end
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

const SlidingWindowRateLimitLuaScript = `
-- KEYS[1]: flashpass:ratelimit:{identifier}
-- ARGV[1]: current timestamp in milliseconds
-- ARGV[2]: window size in milliseconds
-- ARGV[3]: max requests limit
-- ARGV[4]: unique member id (now_ms:uuid)
-- ARGV[5]: key ttl in seconds

local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]
local ttl = tonumber(ARGV[5])

local clearBefore = now - window
redis.call('ZREMRANGEBYSCORE', key, '-inf', clearBefore)

local currentCount = redis.call('ZCARD', key)
if currentCount < limit then
    redis.call('ZADD', key, now, member)
    redis.call('EXPIRE', key, ttl)
    return {1, limit - currentCount - 1} -- 1 = allowed, remaining count
else
    return {0, 0} -- 0 = denied
end
`

// Atomic renewal: Only renew lock if caller is confirmed owner
const RenewLockLuaScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('EXPIRE', KEYS[1], tonumber(ARGV[2]))
else
    return 0
end
`

// Atomic release: Only release lock if caller is confirmed owner (prevents lock hijacking)
const ReleaseLockWithOwnerLuaScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
else
    return 0
end
`

var (
	bookTicketScript             = redis.NewScript(BookTicketLuaScript)
	rollbackStockScript          = redis.NewScript(RollbackStockLuaScript)
	slidingWindowRateLimitScript = redis.NewScript(SlidingWindowRateLimitLuaScript)
	renewLockScript              = redis.NewScript(RenewLockLuaScript)
	releaseLockWithOwnerScript   = redis.NewScript(ReleaseLockWithOwnerLuaScript)
)
