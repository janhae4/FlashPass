# SPEC.md: FlashPass High-Concurrency Ticketing Engine

> **Project Links:** [Master README](./README.md) • [Architecture Blueprint](./ARCHITECTURE.md) • [Execution Tasks](./TASKS.md) • [Agent Rules](./AGENTS.md) • [API Reference](./docs/API_REFERENCE.md) • [Operational Runbook](./docs/RUNBOOK.md) • [Edge Cases Compendium](./docs/DISTRIBUTED_EDGE_CASES.md)

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
5. **Late Payment Webhook & Automated Refund (Auto-Refund):**
   - If Payment Gateway webhook calls `POST /api/v1/orders/:id/pay` after the reservation has expired (`EXPIRED` or past 10 minutes):
   - The engine protects inventory: it strictly refuses to fulfill the ticket or transition to `PAID`.
   - The engine transitions order status to `REFUND_PENDING` and publishes an `order.refund` event to RabbitMQ `flashpass.order.refund.queue`.
   - Dedicated Refund Worker consumes the event, invokes payment gateway refund API with automatic exponential retry, and transitions the order to `REFUNDED`.
   - Returns `200 OK` (or `202 Accepted`) to payment webhook with `{ "status": "REFUND_PENDING", "message": "Order expired. Ticket was released. Automatic refund initiated." }`.
6. **Dual-Write Partial Failure & Periodic Stock Reconciliation (Stock Auditor):**
   - When Redis stock is decremented but RabbitMQ dispatch or process crashes before order persistence:
   - A disparity occurs: `Redis Stock + Pending Orders + Paid Orders < Total Event Tickets` (Ghost Inventory).
   - The Stock Auditor reconciles `expectedStock = total_tickets - activeOrders`.
   - If actual Redis stock is less than expectedStock, it detects ghost inventory and restores Redis stock to `expectedStock`, healing the system automatically without downtime.

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
3. **Interactive In-Browser Benchmark Simulator (k6 Visual Demo):**
   - Live browser-based simulation replacing CLI k6 execution for presentations.
   - Configurable parameters: Stock Capacity (e.g., 500, 1,000, 2,000 tickets), Total Requests (e.g., 2,000 - 20,000), and Concurrency level.
   - Concurrent async pipeline dispatching bookings with unique idempotency keys.
   - Real-time performance tracking: RPS counter, Latency percentiles (min, avg, p95, p99), Success 202 vs Fail-Fast 409 counters, and Error 5xx.
   - Zero Overselling Invariant verification: Dynamically asserts accepted reservations == initial inventory pool.
   - Benchmark History stored strictly in browser `localStorage` (zero database pollution) with run-by-run comparison.


## 5. Stress Testing Capacity Limits & Distributed Overselling Edge Cases
1. **System Bottleneck Ceilings:**
   - **Redis Single-Core Event Loop:** Max ~60,000 - 80,000 Lua script executions/sec.
   - **Go Fiber HTTP Ingestion:** Max ~100,000+ RPS on local socket loopback.
   - **RabbitMQ Ingestion:** Max ~30,000 msgs/sec before memory alarm.
   - **PostgreSQL Connection Pool:** Max 50 connections, ~2,000-3,000 writes/sec (safely decoupled behind RabbitMQ).
   - **Browser Concurrency Barrier:** Chrome/Edge hard-limits concurrent HTTP/1.1 sockets to 6 per domain. Virtual browser tests must stay within 25–100 VUs to prevent socket exhaustion. High-volume testing (> 1,000 VUs) strictly belongs to CLI tools (k6/wrk).
2. **Distributed Overselling Edge Cases (> 0% Oversell in Production):**
   - **Asynchronous Redis Failover:** Master crashes before async replication of Lua stock decrement reaches Replica; promoted Replica double-sells remaining stock.
   - **Payment Webhook vs Timeout Release Race:** Order expires at T=10m00s triggering `INCRBY stock 1`, but Payment Gateway webhook arrives at T=10m01s confirming payment while stock was already re-allocated.
   - **Dual-Write Partial Failure (Ghost Inventory):** Stock decremented in Redis but network drops before message is published to RabbitMQ.
   - **Cross-Slot Sharding:** Inventory split across multiple Redis Cluster shards lacks single-script atomicity without two-phase commit.
   - **RabbitMQ Poison Pill & Death Loop:** Corrupted message or foreign key violation causing infinite Nack loops; isolated via Dead Letter Exchange (DLX) and Redis stock rollback.


## 6. Frontend Distributed Edge Cases Simulation Lab Specification
1. **Session-Based Idempotency Protocol:**
   - On page mount or ticket booking initialization, check `sessionStorage` for existing key: `flashpass_booking_session_{eventId}_{userId}`.
   - If present, reuse the active session idempotency key.
   - If absent, generate a new UUIDv4 and persist it to `sessionStorage`.
   - Clear session key only upon completed payment or explicit user cancellation.
2. **Interactive Distributed Edge Cases Lab (Tab UI):**
   - **Scenario 1 - Master-Replica Asynchronous Failover (Real Docker API Engine):**
     - Direct interaction with real Docker containers: `flashpass_redis` (:6379) and `flashpass_redis_replica` (:6380).
     - Step 0 (`init`): Resets Master & Replica inventory to 1 ticket and re-establishes replication.
     - Step 1 (`book-master`): Buyer A purchases ticket, invoking real atomic Lua script.
     - Step 2: Visualization of asynchronous replication stream in flight (vs synchronous `WAIT 1`).
     - Step 3 (`crash-master`): Real Docker freeze (`docker pause flashpass_redis`).
     - Step 4 (`promote-replica`): Promotion of Replica to Master (`REPLICAOF NO ONE`).
     - Step 5 (`book-replica` & `audit`): Buyer B books on promoted Master (returns 202 oversold, or 409 sold out under WAIT 1). Real-time PostgreSQL audit queries `orders` table to definitively prove 101% overselling vs 0.00% zero overselling.
   - **Scenario 2 - Timeout Worker vs Late Payment Webhook:**
     - Simulates Order A expiring at T=10m00s, triggering worker `INCRBY stock 1`.
     - Buyer B purchases released ticket at T=10m00.5s.
     - Payment Gateway webhook arrives at T=10m01s marking Order A as PAID.
     - Both Buyer A and Buyer B hold paid receipts for the exact same seat.
     - Toggle: "Optimistic State Machine & Auto Refund" to demonstrate refunding Buyer A.
   - **Scenario 3 - Dual-Write Partial Failure (Ghost Inventory):**
     - Redis Lua decrements stock successfully.
     - Go process crashes before `amqp.Publish` reaches RabbitMQ.
     - Demonstrates inventory disappearing with 0 orders in PostgreSQL.
     - Toggle: "Transactional Outbox & Stock Auditor" to self-heal.
      - Background Cronjob (`runReconileStockScheduler`): Inspects RabbitMQ Queue Depth; if queue is empty, executes Instant Heal to resolve False Sold-Out at the end of the event; if queue has in-flight orders, engages 2-cycle debounce.
   - **Scenario 4 - Cross-Slot Sharding in Redis Cluster:**
     - Simulates purchasing bundled tickets (VIP Ticket at Slot 1024 + Parking Pass at Slot 8420).
     - Redis rejects with `CROSSSLOT Keys in request don't hash to the same slot`.
     - Toggle: "Hash Tag `{tag}` Co-location" resolving slot partitioning.


## 7. Backend Clean Code, Concurrency & Layering Architecture
1. **Thread-Safe AMQP Ingestion:**
   - Multi-goroutine concurrent publishing to RabbitMQ must be strictly thread-safe. A dedicated mutex lock protects `amqp091.Channel.PublishWithContext` calls from concurrent write frame corruption under 20,000+ RPS.
2. **Layered Separation of Concerns (Hexagonal / Clean Architecture):**
   - **Transport Layer (`internal/api`):** Thin Fiber handlers for HTTP serialization and status mapping.
   - **Service Layer (`internal/service`):** Core orchestration of business use cases (`BookingService`, `OrderService`, `ReconcileService`).
   - **Repository Layer (`internal/repository`):** Database persistence abstraction (`EventRepository`, `OrderRepository`, `RefundRepository`) using transactional patterns (`pgx.Tx`).
   - **Domain Models & Interfaces:** Decoupled interfaces allowing independent unit testing with mocks.
3. **Graceful Worker Message Draining:**
   - Background consumers must utilize `sync.WaitGroup` to ensure in-flight message processing completes before terminating the worker process upon SIGTERM/SIGINT.
4. **Unified Stock Reconciliation:**
   - Centralize stock auditor formulas and self-healing algorithms in a single service shared between the Admin API and the background queue-depth-aware cronjob.

## 8. High-Concurrency Distributed Edge Cases (Edge Cases 7, 8, 9, 10, 11 & 12)
1. **Edge Case 7 - Double Payment Webhook Concurrency & Idempotent Guard:**
   - **Objective:** Prevent concurrent duplicate webhooks (e.g., from VNPay/Stripe automatic network retries) from triggering multiple state transitions or duplicate refunds.
   - **Distributed Lock:** Acquire `flashpass:lock:pay:{order_id}` with 5-second TTL. Only the winning thread acquires the lock; losing threads wait 100ms and read the committed DB state.
   - **Idempotent Fallback:** If a database update yields zero affected rows, re-verify if the order is already `PAID` and return `200 OK` rather than `500 Internal Server Error`, halting gateway retry storms.
   - **Financial Leakage Prevention:** Expired orders receiving multiple concurrent late webhooks produce exactly one record in `refund_orders` and publish a single refund event to RabbitMQ.

2. **Edge Case 8 - Clock Drift / NTP Desynchronization & Payment Grace Window:**
   - **Objective:** Protect order lifecycles against clock desynchronization across distributed API servers, background workers, and PostgreSQL nodes caused by NTP skew, VM clock drift, or network transit delay.
   - **Clock Skew Tolerance Window ($\Delta = 5\text{s}$):** Orders submitted for payment right at the expiration boundary ($T_{\text{expire}} + \delta$) are granted a 5-second grace window, preventing false expiration and customer frustration.
   - **Database-Authoritative Expiration:** Worker processes must not rely exclusively on local application timers to expire orders; expiration transitions are strictly guarded by PostgreSQL's authoritative clock (`expires_at <= NOW()`).

3. **Edge Case 9 - Bot Traffic Burst & Distributed Sliding-Window Rate Limiting:**
   - **Objective:** Shield the high-concurrency booking engine against bot swarms, scalper scripts, and DDoS bursts spamming `POST /api/v1/tickets/book`.
   - **Sliding-Window Log Algorithm:** Implemented in Redis Lua using a Sorted Set (`ZSET`) with millisecond timestamps as scores. Evicts timestamps older than `now - window` (`ZREMRANGEBYSCORE`), checks active requests via `ZCARD`, and admits new requests atomically (`ZADD`).
   - **Edge Protection:** Rejects excessive bursts immediately at the HTTP transport middleware with HTTP `429 Too Many Requests`, returning `Retry-After` headers and JSON payload, completely offloading pressure from PostgreSQL, RabbitMQ, and the core inventory reservation Lua engine.

4. **Edge Case 10 - Out-of-Order Message Processing & Asynchronous Inversion:**
   - **Objective:** Prevent state corruption and zombie orders when asynchronous messages or HTTP actions are processed out of order due to concurrent RabbitMQ consumers or network reordering.
   - **Sub-Case 10.1 (Early Payment Inversion):** Payment webhook (`POST /orders/:id/pay`) arrives before RabbitMQ consumer commits the initial order insert in PostgreSQL. A short asynchronous retry debounce allows in-flight order persistence to settle without raising false 404s.
   - **Sub-Case 10.2 (Early Cancellation Tombstone):** Cancellation arrives while order creation is still queued. System creates an idempotent `CANCELLED` tombstone; when the late `order.create` worker executes, it is prevented from resurrecting the cancelled order back to `PENDING_PAYMENT`.

5. **Edge Case 11 - Database Cascading Failure & Circuit Breaker Protection:**
   - **Objective:** Protect the entire backend from catastrophic cascading failure and thread/goroutine memory starvation during sudden database lock contention, connection pool exhaustion (`max_conns = 50`), or temporary PostgreSQL outages.
   - **Circuit Breaker State Machine:**
     - **CLOSED:** Normal operations; tracks consecutive database query errors.
     - **OPEN (Tripped):** When error threshold (e.g. 3 consecutive failures) is reached, immediately fast-fails downstream requests with HTTP `503 Service Unavailable` in 0ms, stopping load from worsening the DB outage.
     - **HALF-OPEN (Canary Probe):** After a cooldown period (e.g. 1.5s), admits a single canary probe to test database health. If the canary succeeds, circuit resets to `CLOSED`; if it fails, circuit trips back to `OPEN` for another cycle.

6. **Edge Case 12 - Split-Brain Distributed Lock & Lock Heartbeat Watchdog:**
   - **Objective:** Eliminate distributed split-brain concurrency and lock hijacking caused by lock expiration during long-running tasks, garbage collection pauses, or payment gateway network spikes.
   - **Heartbeat Lease Renewal (Watchdog Pattern):** When a distributed lock is acquired with an initial TTL, a background watchdog goroutine periodically renews the lock lease every $\text{TTL}/3$ using an atomic Lua script (`if GET key == owner then EXPIRE key ttl end`).
   - **Safe Owner-Verified Release:** Only the original lock owner possessing the unique lock token is permitted to release the key via Lua script (`if GET key == owner then DEL key end`), preventing delayed threads from deleting locks acquired by subsequent callers.






## 9. Test Architecture & Test Pyramid Specification
1. **Tier 1: Isolated Unit Tests (`internal/service/*_test.go`):**
   - Pure in-memory unit tests using mocks/stubs for `StockCache`, `EventProducer`, and repositories.
   - Zero external Docker dependencies, sub-second execution (< 0.5s), 100% deterministic assertion of business branches.
2. **Tier 2: API Integration Tests (`internal/api/*_test.go`):**
   - Table-Driven HTTP tests validating routing, input parsing, status code contracts, and header responses.
   - Reusable test fixtures and HTTP dispatch helpers to prevent boilerplate code duplication.
3. **Tier 3: Distributed Edge Cases & Chaos Tests (`internal/api/edge_case_*_test.go`):**
   - Dedicated, modular scenario tests for each of the 12 distributed failure modes.
   - Isolated teardown fixtures preventing test state leakage across queues and databases.

## 10. Security, Authentication & Cryptographic Invariants
1. **Buyer Identity & Zero Client Trust Invariant:**
   - Client requests to `POST /api/v1/tickets/book` must not permit arbitrary client-supplied `user_id` values.
   - The authenticated identity MUST be derived strictly from a cryptographically verified JWT bearer token (`Authorization: Bearer <token>`) or signed session cookie validated by the API Gateway middleware:
     $$\text{UserID} = \text{JWT.Claims}["\text{sub}"]$$
   - Eliminates user impersonation attacks and prevents malicious actors from consuming other users' 1-ticket-per-user limits.
2. **Payment Webhook Cryptographic Verification (HMAC-SHA256):**
   - Webhook calls to `POST /api/v1/orders/:id/pay` from third-party payment providers (Stripe, VNPay, ZaloPay) must be authenticated via an HMAC-SHA256 signature header:
     $$\text{Signature} = \text{HMAC-SHA256}(K_{\text{webhook}}, \text{timestamp} \,\|\, "." \,\|\, \text{raw\_body})$$
   - **Replay Attack Mitigation:** The header must include a millisecond timestamp `t`. If $|t_{\text{current}} - t_{\text{request}}| > 300\text{s}$ (5 minutes), the webhook is rejected with HTTP `401 Unauthorized`.
   - Halts rogue forged payment confirmations and protects against denial-of-service auto-refund triggers.
3. **Admin Surface Isolation & Principle of Least Privilege:**
   - All management endpoints under `/api/v1/admin/*` require `Authorization: Bearer <ADMIN_SECRET>`.
   - Dangerous chaos-injection endpoints (`/api/v1/admin/redis/master/toggle`, `/api/v1/admin/failover/*`) must be blocked at the outer reverse proxy / API gateway level from public ingress, restricted exclusively to internal VPC subnets.

## 11. Distributed Observability & Telemetry Invariants
1. **End-to-End W3C TraceContext Propagation:**
   - Every inbound HTTP request is assigned a W3C `traceparent` header (`00-{trace_id}-{span_id}-{flags}`).
   - The trace context is propagated across network boundaries:
     $$\text{HTTP Request} \longrightarrow \text{Redis Lua} \longrightarrow \text{RabbitMQ Header} \longrightarrow \text{Worker Consumer} \longrightarrow \text{PostgreSQL}$$
   - Enables distributed tracing across asynchronous queues with zero loss of execution correlation.
2. **SRE Telemetry & SLI/SLO Invariants:**
   - Core SLI: $\ge 99.99\%$ of out-of-stock requests must return HTTP `409 Conflict` within $< 2\text{ms}$.
   - Core SLI: $\ge 99.90\%$ of successful bookings must complete in-memory decrement and enqueue to RabbitMQ within $< 25\text{ms}$ (p95).
   - Core SLI: Overselling rate must be rigorously $0.00\%$ under all concurrency conditions.

## 12. Data Governance, High-Volume Partitioning & Retention Invariants
1. **Active Data Retention Window:**
   - The primary OLTP database (`PostgreSQL`) retains active flash sale records in high-performance hot partitions.
   - Orders in terminal states (`EXPIRED`, `REFUNDED`, `CANCELLED`) older than 30 days are automatically migrated to analytical cold storage (S3 Parquet / ClickHouse) to maintain lean B-Tree indexes.
2. **Redis Memory Protection & Eviction Invariant:**
   - Redis cluster instances storing ticket inventory and locks must be configured with `maxmemory-policy noeviction`. Under no memory pressure condition may active inventory keys be evicted asynchronously by Redis LRU/LFU algorithms.
   - All transient tracking keys (rate-limiting ZSETs, idempotency tokens) must have explicit TTLs ($\le 630\text{s}$) to prevent memory leaks during multi-million request flash events.
