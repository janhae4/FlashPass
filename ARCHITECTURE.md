# ARCHITECTURE.md: FlashPass Technical Blueprint

> **Project Links:** [System Spec](./SPEC.md) • [Execution Tasks](./TASKS.md) • [Agent Rules](./AGENTS.md)

## 1. System Components
- **API Server / Ingestion Layer:** Go (Fiber / Gin) or NestJS (Fastify) exposing HTTP endpoints.
- **Frontend Client & Live Dashboard:** Next.js 15 (App Router), Tailwind CSS, Lucide Icons, Server-Sent Events (SSE) or WebSockets for real-time inventory updates.
- **In-Memory Cache & Atomic Engine:** Redis (v7.x) with Redis Lua scripts for concurrency control.
- **Message Broker:** RabbitMQ (v3.13+) with `rabbitmq_delayed_message_exchange` plugin.
- **Persistent Storage:** PostgreSQL (v16+) storing `events`, `users`, `orders`, `tickets`.
- **Infrastructure:** Docker Compose multi-container setup.
- **Testing & Benchmarking Suite:** k6 load testing scripts.

## 2. Redis Key Design & Lua Script
### Key Namespaces:
- `flashpass:stock:{event_id}` (Integer) -> Remaining ticket count in RAM.
- `flashpass:idempotency:{key}` (String, TTL: 30s) -> Stores user reservation token.
- `flashpass:user_booked:{event_id}:{user_id}` (Boolean, TTL: 1h) -> Enforce 1 ticket per user limit.

### Atomic Inventory Decrement (Lua Script):
```lua
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
```

## 3. Database Schema (PostgreSQL)
```sql
CREATE TABLE events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(255) NOT NULL,
    total_tickets INT NOT NULL,
    available_tickets INT NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events(id),
    user_id VARCHAR(64) NOT NULL,
    idempotency_key VARCHAR(128) UNIQUE NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING_PAYMENT', -- PENDING_PAYMENT, PAID, EXPIRED, CANCELLED
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_orders_event_status ON orders(event_id, status);
CREATE INDEX idx_orders_idempotency ON orders(idempotency_key);
```

## 4. RabbitMQ Queue & Exchange Topology
- **Exchange:** `flashpass.order.exchange` (Topic Exchange)
  - **Queue:** `flashpass.order.create.queue` (Routing Key: `order.create`) -> Order persistence worker.
- **Delayed Exchange:** `flashpass.order.delayed.exchange` (`x-delayed-message`)
  - **Queue:** `flashpass.order.timeout.queue` -> Listens for 10-minute expiration check.

## 5. In-Browser Benchmark Architecture & API Extensions
### Dynamic Reset Endpoint:
- `POST /api/v1/admin/events/:id/reset`
  - Optional JSON Body: `{ "tickets": 500 }` (defaults to event's initial total tickets if omitted).
  - Truncates existing orders for the event in PostgreSQL.
  - Flushes Redis booking tags `flashpass:user_booked:{event_id}:*` and sets `flashpass:stock:{event_id}` to specified tickets count.

### Frontend Benchmark Simulator (Client-side Engine):
- Dispatches concurrent asynchronous promises in configurable batches (e.g., 20 - 100 concurrent workers).
- Accumulates response latencies in real-time array to compute `min`, `avg`, `p90`, `p95`, `p99`.
- Tracks `tickets_accepted_202`, `tickets_sold_out_409`, `tickets_error_5xx`.
- Stores run summaries in browser `localStorage` (`flashpass_benchmark_history`) without polluting PostgreSQL.
