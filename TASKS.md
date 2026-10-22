# TASKS.md: FlashPass Implementation Roadmap

> [!IMPORTANT]
> **CONTEXT DEPENDENCY CHAIN (MẠNG LƯỚI NGỮ CẢNH BẮT BUỘC):**
> - **Mục tiêu & Nghiệp vụ:** [`SPEC.md`](./SPEC.md)
> - **Bản vẽ kỹ thuật & Schema/Lua Script:** [`ARCHITECTURE.md`](./ARCHITECTURE.md)
> - **Quy tắc làm việc của Agent:** [`AGENTS.md`](./AGENTS.md)
> 
> **Quy tắc tự động cho Agent:** Trước khi thực thi bất kỳ task nào bên dưới, nếu chưa nạp ngữ cảnh từ `SPEC.md` và `ARCHITECTURE.md` trong phiên làm việc, Agent **BẮT BUỘC TỰ ĐỘNG ĐỌC CHÚNG TRƯỚC**. Sau khi hoàn thành mỗi task, Agent phải **tự động cập nhật `[x]`** vào task đó.

## Phase 1: Environment & Infrastructure Setup
- [x] 1.1 Create `docker-compose.yml` with PostgreSQL 16, Redis 7 (Alpine), and RabbitMQ (Management + Delayed Message Exchange).
- [x] 1.2 Create `.env.example` and `.env` with connection credentials.
- [x] 1.3 Verify containers healthcheck with `docker compose ps`.

## Phase 2: Database & Core Data Layer
- [x] 2.1 Set up database schema migrations for `events` and `orders`.
- [x] 2.2 Create database seed script with a sample Concert Event (1,000 tickets).
- [x] 2.3 Implement Redis client with Connection Pool and Lua Script loader.

## Phase 3: High-Concurrency Core API Service
- [x] 3.1 Implement Admin Pre-warm Endpoint `POST /api/v1/admin/events/:id/prewarm` (loads stock into Redis).
- [x] 3.2 Implement Flash Sale Booking Endpoint `POST /api/v1/tickets/book`:
  - [x] Validate input payload & idempotency key.
  - [x] Execute Redis atomic Lua script.
  - [x] Publish `order.create` event to RabbitMQ.
  - [x] Return `202 Accepted` with reservation token.
- [x] 3.3 Implement `GET /api/v1/events/:id/stock` for live inventory inspection.

## Phase 4: Asynchronous Processing & Stock Compensation
- [x] 4.1 Implement RabbitMQ Order Consumer to write orders into PostgreSQL.
- [x] 4.2 Publish delayed message (TTL 10 min) to `flashpass.order.delayed.exchange`.
- [x] 4.3 Implement Timeout Worker: If order unpaid after 10 min, mark `EXPIRED` and increment Redis stock.
- [x] 4.4 Implement Payment Mock Endpoint `POST /api/v1/orders/:id/pay` (transitions status to `PAID`).

## Phase 5: Interactive Frontend Client & Live Demo Dashboard (Next.js 15)
- [x] 5.1 Initialize Next.js 15 frontend application with Tailwind CSS and Lucide icons.
- [x] 5.2 Build Event Booking Page:
  - [x] Event banner, ticket tier details, and countdown timer to sale opening.
  - [x] Real-time inventory progress bar (synced via polling or SSE/WebSocket).
  - [x] High-concurrency "Mua Vé Ngay" button with client-side UUID generation (`idempotency_key`).
- [x] 5.3 Build Reservation Modal & Payment Flow:
  - [x] 10-minute live countdown timer for pending payment.
  - [x] "Thanh toán giả lập (Mock Pay)" button calling `POST /api/v1/orders/:id/pay`.
  - [x] Visual feedback when ticket expires and is released back to stock.
- [x] 5.4 Build Admin Live Monitoring Dashboard:
  - [x] Gauge cards: Current Redis Stock, Queued Orders, Paid Orders, Expired Orders.
  - [x] One-click "Pre-warm Stock" and "Restock Event" buttons for live interview presentations.

## Phase 6: Benchmark & Load Testing with k6
- [x] 6.1 Write `load-test.js` targeting 20,000 virtual requests hitting 1,000 inventory stock.
- [x] 6.2 Execute test and collect metrics: RPS, p95/p99 latency, HTTP 202 count vs HTTP 409 count.
- [x] 6.3 Verify PostgreSQL count: Exactly 1,000 orders created (0 overselling).
- [x] 6.4 Generate `BENCHMARK.md` report with graphs and summary for GitHub README.
- [x] 6.5 Update backend `POST /api/v1/admin/events/:id/reset` to support custom `tickets` parameter.
- [x] 6.6 Build Interactive In-Browser Benchmark Simulator in Next.js:
  - [x] Custom ticket capacity selection (e.g. 500, 1000, 2000) & surge request count (1,000 to 20,000).
  - [x] Live execution gauge: real-time RPS, latency percentiles (min, avg, p95, p99), 202 vs 409 counters.
  - [x] Zero Overselling dynamically validated in UI.
  - [x] Client-side Benchmark History stored in `localStorage` (no database pollution) with run-to-run comparison.
- [x] 6.7 Benchmark Visualization & Stress Limit Analysis:
  - [x] Build Python visualizer script `scripts/visualize_benchmark.py` with matplotlib for throughput & latency curves.
  - [x] Integrate rich visual graphs (Latency Distribution & Response Composition bars) into Next.js Benchmark Tab.
  - [x] Embed native Mermaid charts & Python visual graphs in `BENCHMARK.md`.
  - [x] Document breaking points & real-world distributed edge cases where overselling > 0% occurs.


## Phase 7: Distributed Edge Cases Simulation Lab & Session Idempotency (Frontend Only)
- [x] 7.1 Refactor Client Idempotency: Initialize session-bound `idempotency_key` via `sessionStorage` on page load, retaining across F5 reloads and retries.
- [x] 7.2 Create Modular Interactive Edge Cases Lab Components in `frontend/components/edge-cases/`:
  - [x] `EdgeCasesLab.tsx`: Interactive lab shell with scenario selector and visual architectural topology.
  - [x] `FailoverSimulation.tsx`: Redis Master-Replica Asynchronous Failover step-by-step simulator showing data loss and overselling > 0% vs `WAIT 1` mitigation.
  - [x] `TimeoutRaceSimulation.tsx`: 10-Minute Timeout Worker vs Late Payment Webhook race condition simulator with double payment vs Optimistic State Lock.
  - [x] `DualWriteSimulation.tsx`: Partial Dual-Write failure simulator (Redis decrement vs RabbitMQ publish crash) showing Ghost Inventory vs Transactional Outbox.
  - [x] `CrossSlotSimulation.tsx`: Redis Cluster Cross-Slot Sharding simulator showing `CROSSSLOT` error vs Hash Tag `{tag}` co-location.
- [x] 7.3 Integrate "🧪 Phòng Lab Lỗi Phân Tán (Edge Cases)" Tab into `frontend/app/page.tsx` navigation without modifying backend services.

## Phase 8: Real Docker-Engine Redis Failover & Dual-Node Replication
- [x] 8.1 Expand `docker-compose.yml` with `flashpass_redis_replica` container on port 6380 (`replicaof redis 6379`).
- [x] 8.2 Add dual Redis client connection pool in Go backend (`rdb` on 6379, `replicaRdb` on 6380).
- [x] 8.3 Implement admin endpoints:
  - [x] `POST /api/v1/admin/redis/master/toggle`: Pause/unpause Master container via `docker pause`.
  - [x] `POST /api/v1/admin/redis/master/status`: Query live container status.
  - [x] `POST /api/v1/admin/failover/init`: Reset Master/Replica inventory & replication.
  - [x] `POST /api/v1/admin/failover/book-master`: Client A books with optional synchronous `WAIT 1`.
  - [x] `POST /api/v1/admin/failover/crash-master`: Crash Master during sync.
  - [x] `POST /api/v1/admin/failover/promote-replica`: Instruct Sentinel promotion `REPLICAOF NO ONE`.
  - [x] `POST /api/v1/admin/failover/book-replica`: Client B books on promoted Master.
  - [x] `GET /api/v1/admin/failover/audit`: Audit real PostgreSQL database `orders` table.
- [x] 8.4 Connect `FailoverSimulation.tsx` to 100% real API endpoints with step-by-step navigation, live terminal logging, and PostgreSQL order auditing table.
- [x] 8.5 Build Automated Chaos Test Suite: Red Test (`failover_test.go` verifying overselling vulnerability without WAIT) and Green Test (`failover_with_wait_test.go` verifying zero overselling with WAIT 1).
- [x] 8.6 Implement Chaos Red Test for Edge Case 2 (`timeout_race_test.go` connecting directly to Fiber API `POST /api/v1/orders/:id/pay` and `POST /api/v1/tickets/book`).
- [x] 8.7 Implement Chaos Red Test for Edge Case 3 (`dual_write_test.go` connecting directly to Fiber API `POST /api/v1/tickets/book` proving Ghost Inventory).

## Phase 9: Distributed Edge Cases API Test Suite
- [x] 9.1 Implement `TestEdgeCase1_RedisFailover` in `backend/internal/api/api_test.go` directly targeting API endpoints (`/api/v1/admin/events/:id/prewarm`, `/api/v1/tickets/book`), verifying failover resilience, `Retry-After: 2` header, and DB guard zero-overselling.
- [x] 9.2 Implement `TestEdgeCase2_TimeoutRaceCondition` in `backend/internal/api/api_test.go` directly targeting API endpoints (`/api/v1/tickets/book`, `/api/v1/orders/:id/pay`), verifying rejection of late payment for expired reservations (HTTP 400), successful re-allocation to Buyer B, and zero overselling.
- [x] 9.3 Implement Auto-Refund Pipeline for Late Payment Webhooks:
  - [x] Add `OrderStatusRefundPending` and `OrderStatusRefunded` enums in `backend/internal/models/order.go`.
  - [x] Declare `flashpass.order.refund.queue` (routing key `order.refund`) in `backend/internal/rabbitmq/producer.go` and producer helper `PublishOrderRefund`.
  - [x] Update `PayOrder` in `backend/internal/api/handlers.go` to transition expired orders to `REFUND_PENDING` and publish `order.refund` to RabbitMQ instead of just returning 400.
  - [x] Implement `runRefundWorker` in `backend/internal/consumer/consumer.go` to process refunds and transition to `REFUNDED`.
  - [x] Update `TestEdgeCase2_TimeoutRaceCondition` to verify the complete Auto-Refund lifecycle via API and RabbitMQ.
- [x] 9.4 Implement Edge Case 3: Dual-Write Partial Failure (Ghost Inventory) & Stock Reconciliation:
  - [x] Implement Stock Reconciliation endpoint `POST /api/v1/admin/events/:id/reconcile` in `backend/internal/api/handlers.go`.
  - [x] Implement `TestEdgeCase3_DualWriteGhostInventory` in `backend/internal/api/api_test.go` directly targeting API endpoints (`/api/v1/tickets/book`, `/api/v1/events/:id/stock`, `/api/v1/admin/events/:id/reconcile`).
  - [x] Verify detection of ghost inventory (disparity between Redis stock and DB orders) and self-healing back to valid inventory capacity.
- [x] 9.5 Advanced Ghost Inventory Sub-Cases & Self-Healing:
  - [x] Two-Way Stock Clamping: Support both deficit (`discrepancy > 0`) and surplus (`discrepancy < 0`), clamping Redis stock down to prevent overselling.
  - [x] Fast-Path Hit-Zero Trigger (`ResultSuccessHitZero` & 5s SETNX Lock) to eliminate closing-seconds false sold-out polling blind spots.
  - [x] Orphaned User Lock Resolution: Align `user_booked` TTL to 630s and implement `POST /api/v1/admin/events/:id/users/:userId/unlock` with DB active order verification.
- [x] 9.6 Implement Edge Case 4: Redis Cluster Cross-Slot Sharding & Hash Tag Co-Location:
  - [x] Implement CRC16 16,384-slot calculator & Hash Tag extraction in `backend/internal/redis/keys.go`.
  - [x] Update all Redis key formats to Hash Tag `{event_id}` co-location format (`flashpass:{eventId}:stock`, `flashpass:{eventId}:user_booked:{userId}`, `flashpass:{eventId}:audit_lock`).
  - [x] Implement `TestEdgeCase4_CrossSlotHashTag` in `backend/internal/api/api_test.go` directly targeting API endpoints, verifying slot alignment across all event keys and atomic booking with zero CROSSSLOT errors.
- [x] 9.7 Implement Edge Case 5: RabbitMQ Poison Pill & Dead Letter Queue (DLQ):
  - [x] Declare `flashpass.dlx` Dead Letter Exchange and `flashpass.order.dlq` in `backend/internal/rabbitmq/producer.go`.
  - [x] Bind main `flashpass.order.create.queue` with `x-dead-letter-exchange` and `x-dead-letter-routing-key`.
  - [x] Update `handleOrderCreate` in `backend/internal/consumer/consumer.go` to quarantine poison pills on second failure and rollback Redis stock.
  - [x] Implement `TestEdgeCase5_RabbitMQDeadLetterQueue` in `backend/internal/api/api_test.go` verifying quarantine in DLQ, unblocking main queue, and zero ghost inventory.
- [x] 9.8 Implement Edge Case 6: Cache Stampede & SingleFlight Request Coalescing:
  - [x] Implement Cache-Aside metadata caching with Hash Tag `{event_id}` in `backend/internal/redis/keys.go`.
  - [x] Embed `singleflight.Group` in `backend/internal/api/handlers.go` to coalesce concurrent reads down to 1 query.
  - [x] Implement `TestEdgeCase6_CacheStampedeSingleFlight` in `backend/internal/api/api_test.go` verifying 100 concurrent requests without connection exhaustion.
- [x] 9.9 Implement Edge Case 7: Double Payment Webhook Concurrency & Distributed Payment Lock:
  - [x] Implement Distributed Lock `AcquireLock` / `ReleaseLock` in `backend/internal/redis/client.go` with 5s TTL.
  - [x] Refactor `PayOrder` in `backend/internal/service/order_service.go` with 100ms settle wait, winner-only lock release, and idempotent recheck fallback.
  - [x] Implement `TestEdgeCase7_DoublePaymentWebhookConcurrency` in `backend/internal/api/api_test.go` verifying 5 concurrent webhooks deduplicate with 200 OK and exactly 1 refund record.
- [x] 9.10 Implement Edge Case 8: Clock Drift / NTP Desynchronization & Payment Grace Window:
  - [x] Add `ClockDriftGraceWindow` (5s) in `backend/internal/service/order_service.go` to tolerate boundary submissions.
  - [x] Implement `ExpireIfPastDeadline(ctx, orderID)` in `backend/internal/repository/order_repository.go` using PostgreSQL authoritative `expires_at <= NOW()`.
  - [x] Update `handleOrderTimeout` in `backend/internal/consumer/consumer.go` to use `ExpireIfPastDeadline` to prevent premature expiration from fast worker clocks.
  - [x] Implement `TestEdgeCase8_ClockDriftGraceWindow` in `backend/internal/api/api_test.go` testing boundary payment within grace window and premature worker rejection.
- [x] 9.11 Implement Edge Case 9: Bot Traffic Burst & Distributed Sliding-Window Rate Limiting:
  - [x] Implement Sliding Window Log Lua script in `backend/internal/redis/lua.go` with Hash Tag `{identifier}`.
  - [x] Add `CheckRateLimit(ctx, identifier, limit, window)` helper in `backend/internal/redis/client.go`.
  - [x] Implement Rate Limiter Middleware or Booking Guard returning HTTP `429 Too Many Requests` with `Retry-After` header.
  - [x] Implement `TestEdgeCase9_SlidingWindowRateLimiter` in `backend/internal/api/api_test.go` verifying 5 burst requests allowed, subsequent spam requests rejected with 429, and recovery after window slides.
- [x] 9.12 Implement Edge Case 10: Out-of-Order Message Processing & Asynchronous Inversion:
  - [x] Implement `CancelOrder` in `backend/internal/service/order_service.go` and `POST /api/v1/orders/:id/cancel` in `backend/internal/api/handlers.go`.
  - [x] Handle Early Payment Inversion: Add in-flight retry debounce in `PayOrder` so early webhooks do not fail with false 404s while RabbitMQ worker persists order.
  - [x] Handle Early Cancellation Inversion: Implement Tombstone guard in `orderRepo.Create` and `orderRepo.CancelOrTombstone` preventing late order creation from resurrecting cancelled orders.
  - [x] Implement `TestEdgeCase10_OutOfOrderMessageProcessing` in `backend/internal/api/api_test.go` verifying early cancel tombstone, early payment retry, and state machine integrity.
- [x] 9.13 Implement Edge Case 11: Database Cascading Failure & Circuit Breaker Protection:
  - [x] Implement thread-safe Circuit Breaker state machine (Closed, Open, Half-Open) with cooldown recovery in `backend/internal/resilience/circuit_breaker.go`.
  - [x] Wrap database persistence operations with Circuit Breaker guard to fast-fail on database outage (HTTP 503).
  - [x] Implement `TestEdgeCase11_DatabaseCircuitBreaker` in `backend/internal/api/api_test.go` verifying trip to OPEN upon consecutive DB failures, fast-fail shedding without DB hammering, and automatic reset via canary request in HALF-OPEN.
- [x] 9.14 Implement Edge Case 12: Split-Brain Distributed Lock & Lock Heartbeat Watchdog:
  - [x] Implement Owner-Verified Distributed Lock (`SET key owner NX EX ttl`) and Lock Renewal Lua script (`if GET == owner then EXPIRE ttl`) in `backend/internal/redis/lua.go`.
  - [x] Implement `AcquireLockWithWatchdog` in `backend/internal/redis/client.go` with background heartbeat goroutine renewing TTL every `ttl/3`.
  - [x] Integrate owner-verified safe release preventing lock hijacking across slow/delayed payment processing.
  - [x] Implement `TestEdgeCase12_DistributedLockWatchdog` in `backend/internal/api/api_test.go` verifying TTL auto-renewal during long operations, split-brain prevention, and clean release.






## Phase 10: Backend Clean Code & Architecture Refactoring
- [x] 10.1 Thread-Safe RabbitMQ Producer: Guard channel publish with mutex to ensure safe concurrent publishing under high throughput.
- [x] 10.2 Domain Interfaces & Layering (Repository & Service Pattern):
  - [x] Implement `internal/repository` (`EventRepository`, `OrderRepository`, `RefundRepository`).
  - [x] Implement `internal/service` (`BookingService`, `OrderService`, `ReconcileService`).
  - [x] Refactor `internal/api/handlers.go` into a thin controller delegating to services.
- [x] 10.3 Background Worker & Graceful Shutdown Hardening:
  - [x] Add `sync.WaitGroup` to `consumer.Service` to drain in-flight messages cleanly on `SIGTERM`.
  - [x] Replace arbitrary `time.Sleep` and unmanaged goroutines with structured timeout context.
- [x] 10.4 Unify Stock Reconciliation Logic (DRY):
  - [x] Consolidate reconciliation queries and self-healing logic into `ReconcileService`.
  - [x] Use `ReconcileService` across both Admin API and the background Cron scheduler.
- [x] 10.5 Codebase Hygiene & Cleanup:
  - [x] Fix naming typos (`reconile` -> `reconcile`).
  - [x] Remove duplicate `backend/migrations` folder (keeping embedded `internal/database/migrations`).
  - [x] Replace all magic status strings with `models.OrderStatus...` and `models.RefundStatus...`.
  - [x] Remove unused fields (`failoverSynced`).
  - [x] Make Redis Sentinel and host configs dynamic and environment-driven.
- [x] 10.6 Regression Testing & Verification:
  - [x] Run full backend test suite (`go test -v ./...`) verifying all edge case tests, booking flow, and auto-refund pass cleanly.

## Phase 11: Production Documentation & Operational Runbooks
- [x] 11.1 Master `README.md`: Create top-tier enterprise README with architecture diagrams, 12 distributed edge cases matrix, KPI table, Docker quickstart, API overview, and k6 benchmark showcase.
- [x] 11.2 Comprehensive API Reference (`docs/API_REFERENCE.md`): Detailed OpenAPI/REST contract with request/response headers, status codes, payload schemas, and cURL examples for all endpoints.
- [x] 11.3 Production Operational Runbook & Disaster Recovery (`docs/RUNBOOK.md`): Day-2 operations guide covering metrics/alerting, Redis Sentinel failover procedure, RabbitMQ DLQ poison pill triage, Stock Auditor reconciliation, Circuit Breaker tuning, and incident response playbook.
- [x] 11.4 Distributed Systems Deep Dive (`docs/DISTRIBUTED_EDGE_CASES.md`): Exhaustive technical compendium covering all 12 edge cases, formal mathematical invariants, failure mechanics, and code references.
- [x] 11.5 Vietnamese Production Documentation Suite: Create `README.vi.md` (Bản tài liệu tiếng Việt hoàn chỉnh) and `docs/DISTRIBUTED_EDGE_CASES.vi.md` (Chuyên sâu 12 kịch bản lỗi phân tán tiếng Việt), plus language navigation links.


## Phase 12: Test Architecture & Test Pyramid Refactoring
- [x] 12.1 Extract Test Helpers & Fixtures (`internal/api/test_helpers_test.go`):
  - [x] Consolidate `setupTestApp(t)` and shared HTTP request/response parsing helpers.
  - [x] Eliminate ~600 lines of duplicate HTTP boilerplate and hardcoded DDL strings.
- [x] 12.2 Modularize Core API Integration Tests (`internal/api/`):
  - [x] `health_test.go`: Test HealthCheck endpoint.
  - [x] `prewarm_test.go`: Test Admin Prewarm & Stock inspection.
  - [x] `booking_flow_test.go`: Test Booking flow with structured Table-Driven Subtests.
  - [x] `payment_flow_test.go`: Test Payment lifecycle and Auto-Refund routing.
- [x] 12.3 Modularize 12 Distributed Edge Cases & Chaos Tests (`internal/api/`):
  - [x] `edge_case_1_failover_test.go`: Redis Master-Replica Asynchronous Failover (`docker pause`).
  - [x] `edge_case_2_timeout_race_test.go`: 10-Minute Timeout Worker vs Late Payment Webhook.
  - [x] `edge_case_3_ghost_inventory_test.go`: Dual-Write Partial Failure & Stock Reconciliation.
  - [x] `edge_case_4_cross_slot_test.go`: Redis Cluster Cross-Slot Sharding & Hash Tag `{event_id}`.
  - [x] `edge_case_5_dlq_poison_pill_test.go`: RabbitMQ Poison Pill & Dead Letter Queue (DLQ).
  - [x] `edge_case_6_cache_stampede_test.go`: Cache Stampede & SingleFlight Request Coalescing.
  - [x] `edge_case_7_double_payment_test.go`: Double Payment Webhook Concurrency & Distributed Lock.
  - [x] `edge_case_8_clock_drift_test.go`: Clock Drift / NTP Desynchronization & Payment Grace Window.
  - [x] `edge_case_9_rate_limiter_test.go`: Bot Traffic Burst & Distributed Sliding-Window Rate Limiting.
  - [x] `edge_case_10_out_of_order_test.go`: Out-of-Order Message Processing & Asynchronous Inversion.
  - [x] `edge_case_11_circuit_breaker_test.go`: Database Cascading Failure & Circuit Breaker.
  - [x] `edge_case_12_lock_watchdog_test.go`: Split-Brain Distributed Lock & Lock Heartbeat Watchdog.
- [x] 12.4 Deprecate & remove the monolithic 1,634-line `api_test.go`.
- [x] 12.5 Full Test Suite Verification: Run `go test -v ./...` verifying 100% of tests pass cleanly.

## Phase 13: Codebase Comment Standardization & Global Showcase Polish
- [x] 13.1 Audit all Go source files across `backend/` for non-ASCII / Vietnamese comments and commented-out dead code.
- [x] 13.2 Standardize and translate all comments across edge-case scenarios (`edge_case_5` through `edge_case_12`) into professional, idiomatic technical English.
- [x] 13.3 Preserve Godoc exported documentation, Lua script argument mappings, and distributed invariant explanations.
- [x] 13.4 Verify zero dead code and verify clean test execution (`go test -count=1 ./...`) with 100% pass rate.

## Phase 14: Enterprise Production Hardening (P1, P2 & P3 Specification Suite)
- [x] 14.1 Security & Cryptographic Invariants (`SPEC.md`, `ARCHITECTURE.md`, `docs/API_REFERENCE.md`):
  - [x] Zero-Trust Buyer Identity extraction via verified JWT Bearer token claims (`sub`).
  - [x] Payment Webhook HMAC-SHA256 signature verification (`X-Signature: t={ts},v1={hash}`) with 300s replay tolerance.
  - [x] Admin plane isolation and chaos route ingress protection.
- [x] 14.2 OpenTelemetry Distributed Tracing Blueprint (`ARCHITECTURE.md`, `SPEC.md`):
  - [x] W3C TraceContext propagation across HTTP Gateway -> Redis -> RabbitMQ headers -> Worker -> PostgreSQL.
  - [x] Mermaid distributed tracing lifecycle sequence diagram.
- [x] 14.3 SRE Metrics & Prometheus Alerting Suite (`ARCHITECTURE.md`, `docs/RUNBOOK.md`):
  - [x] Formal Prometheus metrics catalog (Counters, Histograms with explicit buckets, Gauges).
  - [x] PromQL trigger rules for SEV-1, SEV-2, and SEV-3 alert conditions.
  - [x] Alertmanager YAML configuration blueprint (`alerts.yaml`).
- [x] 14.4 High-Volume Data Lifecycle, Partitioning & Disaster Recovery (`ARCHITECTURE.md`, `docs/RUNBOOK.md`):
  - [x] PostgreSQL Range Partitioning by Month for `orders_partitioned` and index maintenance.
  - [x] Cold-storage archival pipeline to S3 Parquet / ClickHouse for records older than 30 days.
  - [x] Disaster Recovery SLAs: RPO < 1s, RTO < 30s with SOP-04 failover runbook.
  - [x] SOP-05 database partition maintenance procedure.
- [x] 14.5 Machine-Readable OpenAPI 3.1 Specification (`docs/openapi.yaml`):
  - [x] Complete OpenAPI 3.1.0 contract covering all public, order, and admin endpoints.
  - [x] Type definitions for schemas (`Event`, `Order`, `RefundOrder`, `BookTicketRequest`, etc.) and security schemes.
- [x] 14.6 Full Documentation Synchronization Audit:
  - [x] Complete resolution of all P0, P1, P2, and P3 documentation gaps.
  - [x] Upgrade overall documentation audit scorecard to 10.0 / 10 (Enterprise Production Standard).
