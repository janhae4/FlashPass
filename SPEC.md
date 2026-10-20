# SPEC.md: FlashPass High-Concurrency Ticketing Engine

> **Project Links:** [Architecture Blueprint](./ARCHITECTURE.md) • [Execution Tasks](./TASKS.md) • [Agent Rules](./AGENTS.md)

## 1. System Objective
Build an ultra-low-latency, high-concurrency ticket reservation engine capable of handling **20,000+ requests/second (RPS)** during flash sale events without database collapse, duplicate ticket purchases, or inventory overselling.

## 2. Key Performance Indicators (KPIs)
- **Zero Overselling (Tỷ lệ bán âm 0.00%):** If inventory is 1,000 tickets, exactly 1,000 reservations are created.
- **Latency (Độ trễ):** `p95 < 25ms`, `p99 < 50ms` at 20,000 RPS.
- **Fail-Fast Mechanism:** Reject out-of-stock requests in `< 2ms` directly in memory without touching PostgreSQL.
- **Idempotency Guarantee:** Duplicate requests from the same user within 30 seconds are rejected with cached response.
- **Reservation Expiration Window:** Reserved tickets automatically expire and return to inventory pool after 10 minutes if unpaid.

## 3. Core Business Workflows
1. **Flash Sale Pre-warming:** Administrator pre-loads event stock and metadata from PostgreSQL into Redis before sale begins.
2. **Flash Ticket Booking:**
   - Client sends `POST /api/v1/tickets/book` with `event_id`, `user_id`, `idempotency_key`.
   - Gateway verifies rate limit and idempotency.
   - Atomic Lua Script checks & decrements stock in Redis.
   - If success: A reservation token is generated, order payload is published to RabbitMQ `order_exchange`. Client receives `202 Accepted` with reservation details.
   - If out of stock: Client immediately receives `409 Conflict (Sold Out)`.
3. **Asynchronous Order Creation:**
   - RabbitMQ worker consumes order event and persists the order into PostgreSQL with status `PENDING_PAYMENT`.
   - Worker schedules a delayed message (10 minutes TTL) in RabbitMQ Delayed Exchange.
4. **Order Expiration & Stock Rollback:**
   - After 10 minutes, delayed consumer checks order payment status.
   - If still `PENDING_PAYMENT`: Marks order `EXPIRED`, executes Redis atomic increment to release stock back to pool.

## 4. Frontend & User Experience (Next.js 15)
1. **Interactive Event Booking Interface:**
   - Event hero banner, live countdown timer to sale opening.
   - Real-time remaining stock progress bar updated via WebSocket or Server-Sent Events (SSE).
   - Instant "Mua Vé Ngay" button generating unique client-side `idempotency_key`.
   - Reservation Success Modal with live **10-minute Payment Countdown Timer** and mock "Thanh Toán Ngay" button.
   - Fail-fast sold-out state with clear error feedback.
2. **Real-time Admin Monitoring Dashboard:**
   - Live metrics: In-memory stock vs PostgreSQL persistent orders, incoming RPS counter, payment conversion rate.
   - One-click "Pre-warm Cache" and "Reset/Restock Event" controls for interview demonstrations.
