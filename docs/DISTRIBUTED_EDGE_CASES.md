# Distributed Edge Cases Engineering Compendium: The 12 FlashPass Scenarios

> **Author:** FlashPass Core Distributed Systems Team  
> **Topic:** High-Concurrency Flash Sale Consistency, Invariants & Disaster Resilience  
> **Test Suite:** `backend/internal/api/edge_case_*_test.go` (12/12 Modular Automated Chaos & Integration Tests)  

🌐 **Language / Ngôn ngữ:** **English** • [Tiếng Việt](./DISTRIBUTED_EDGE_CASES.vi.md)

---

## 📌 Introduction & Invariant Foundations

In ultra-high concurrency distributed systems, traditional ACID transactional isolation cannot be achieved across separate network-partitioned services (Redis, RabbitMQ, PostgreSQL) without sacrificing the low-latency throughput required during flash sales (> 20,000 RPS).

FlashPass enforces the following immutable system invariants:
1. **$\text{Invariant}_{\text{Overselling}} = 0.00\%$:** Under no failure mode or network partition may total active orders exceed initial inventory capacity:
   $$\text{Orders}_{\text{PAID}} + \text{Orders}_{\text{PENDING\_PAYMENT}} \le \text{TotalTickets}$$
2. **$\text{Invariant}_{\text{Idempotency}} = 1$:** Identical request payloads with matching `idempotency_key` produce identical side-effects:
   $$\forall k, \quad f(k) = f(f(k))$$
3. **$\text{Invariant}_{\text{Liveness}} < 2\text{ms}$:** Out-of-stock requests must fail-fast directly in memory without consuming database worker threads.

---

## Scenario 1: Redis Master Failover & Asynchronous Replication Gap

### 1. The Distributed Failure Mechanics
Redis uses asynchronous master-to-replica replication for performance. When a master node processes an atomic Lua inventory decrement and confirms HTTP `202 Accepted` to Client A, the decrement is queued in the replication backlog. If the Master hardware or kernel crashes before that packet transmits over the network:
- Sentinel detects Master failure and promotes the Replica (`REPLICAOF NO ONE`).
- The newly promoted Replica still possesses the old, non-decremented inventory count.
- Client B connects to the new Master and successfully purchases the exact same ticket.
- **Result:** $100\% + N\%$ Overselling in production.

### 2. The FlashPass Solution
FlashPass hardens failover using a hybrid multi-layer defense:
1. **Synchronous `WAIT` Primitive Option:** For ultra-critical VIP tiers, the booking engine can mandate synchronous replica acknowledgement before returning 202:
   ```go
   rdb.Wait(ctx, 1, 500*time.Millisecond)
   ```
2. **PostgreSQL Unique Guard Rail:** The database maintains an absolute constraint:
   $$\sum \text{Orders}_{\text{PAID}} \le \text{TotalTickets}$$
   Any late order that attempts to persist beyond capacity is trapped by an atomic `SELECT ... FOR UPDATE` verification in the fulfillment worker, preventing physical ticket issuance.
3. **Verification:** `TestEdgeCase1_RedisFailover` simulates Master container pause, Sentinel promotion, and verifies 0.00% overselling.

---

## Scenario 2: Timeout Worker vs Late Webhook Race Condition

### 2. The Distributed Failure Mechanics
Consider a 10-minute reservation window:
- **T = 10m00.000s:** Order A expires unpaid. The Delayed Timeout Worker executes:
  - Updates Order A status to `EXPIRED`.
  - Increments Redis stock: `INCRBY stock 1`.
- **T = 10m00.100s:** Buyer B purchases the newly freed ticket and receives an order confirmation.
- **T = 10m01.000s:** Payment Gateway webhook for Order A arrives late (delayed by network transit or 3D-Secure bank OTP delays).
- **Catastrophe:** If the payment handler blindly marks Order A as `PAID`, both Buyer A and Buyer B hold paid receipts for the exact same seat.

### 2. The FlashPass Solution
FlashPass introduces an **Optimistic State Machine Guard** paired with an **Automated Refund Pipeline**:
1. When `/orders/:id/pay` is invoked, it evaluates the order state inside a distributed lock.
2. If `order.Status == EXPIRED` or `time.Now() > order.ExpiresAt + GraceWindow`:
   - It strictly **rejects fulfillment** of the ticket. Buyer B retains the valid seat.
   - It records a refund in `refund_orders` table with status `REFUND_PENDING`.
   - Publishes an `order.refund` event to RabbitMQ `flashpass.order.refund.queue`.
   - Returns HTTP `200 OK` with payload:
     ```json
     { "status": "REFUND_PENDING", "message": "Order expired. Ticket was released. Automatic refund initiated." }
     ```
3. Dedicated Refund Workers consume the refund queue and invoke the payment provider's refund API with exponential backoff.
4. **Verification:** `TestEdgeCase2_TimeoutRaceCondition`.

---

## Scenario 3: Dual-Write Partial Failure & Ghost Inventory Self-Healing

### 1. The Distributed Failure Mechanics
In a distributed two-phase action:
1. Step 1: Redis Lua decrements inventory from 100 to 99.
2. Step 2: API publishes `order.create` to RabbitMQ.
If the API container crashes (OOM, network blip, panic) immediately between Step 1 and Step 2, the message is lost. Redis believes 99 tickets remain, but PostgreSQL will never receive the order.
- **Result (Ghost Inventory):**
  $$\text{Redis Stock} + \text{Active Orders} < \text{Total Tickets}$$
  Tickets vanish into thin air, and the event falsely marks "Sold Out" while tickets remain unsold.

### 2. The FlashPass Solution
FlashPass deploys a background **Stock Auditor (`ReconcileService`)**:
1. Calculates theoretical expected stock:
   $$\text{ExpectedStock} = \text{TotalTickets} - (\text{Orders}_{\text{PENDING}} + \text{Orders}_{\text{PAID}})$$
2. Compares `ExpectedStock` against actual Redis stock:
   $$\Delta = \text{ExpectedStock} - \text{RedisStock}$$
3. **Two-Way Self-Healing Clamping:**
   - If $\Delta > 0$ (Ghost Inventory detected): Redis stock is incremented by $\Delta$, restoring vanished tickets.
   - If $\Delta < 0$ (Over-allocation detected): Redis stock is decremented down to prevent overselling.
4. **Hit-Zero Fast Path:** When stock hits `0`, a 5-second `SETNX` lock is engaged to run immediate reconcile, preventing closing-seconds false sold-outs.
5. **Orphaned User Lock Cleanup:** Checks if user has `flashpass:{eventId}:user_booked:{userId}` without an active DB order, releasing the lock so legitimate users can re-attempt.
6. **Verification:** `TestEdgeCase3_DualWriteGhostInventory`.

---

## Scenario 4: Redis Cluster Cross-Slot Partitioning & Hash Tag Co-Location

### 1. The Distributed Failure Mechanics
Redis Cluster distributes 16,384 hash slots across multiple nodes:
$$\text{Slot} = \text{CRC16}(\text{key}) \pmod{16384}$$
When executing a multi-key Lua script checking stock (`flashpass:stock:evt-100`) and recording user reservation (`flashpass:user_booked:evt-100:user-5`), Redis computes different CRC16 hashes for each key.
If those keys map to different nodes in the cluster, Redis aborts with a fatal runtime error:
```
CROSSSLOT Keys in request don't hash to the same slot
```

### 2. The FlashPass Solution
FlashPass mandates **Hash Tag Co-Location** using `{...}` syntax:
- Stock Key: `flashpass:{evt-100}:stock`
- User Key: `flashpass:{evt-100}:user_booked:{user-5}`
- Audit Lock: `flashpass:{evt-100}:audit_lock`
- Metadata Cache: `flashpass:{evt-100}:meta`

Because Redis Cluster hashes only the text enclosed in `{...}`, all keys for a given event hash strictly to `CRC16("evt-100") \pmod{16384}`, guaranteeing 100% co-location on the same cluster shard without cross-slot violations.
- **Verification:** `TestEdgeCase4_CrossSlotHashTag` asserts that all related keys resolve to identical slot IDs.

---

## Scenario 5: RabbitMQ Poison Pill & Dead Letter Queue (DLQ) Quarantine

### 1. The Distributed Failure Mechanics
A malformed message (corrupted JSON, invalid foreign key UUID, unexpected null byte) arrives on `flashpass.order.create.queue`.
When the consumer fails to parse or insert the order into PostgreSQL:
- If the worker sends `BasicNack(requeue=true)`, RabbitMQ immediately re-delivers the poison pill.
- The consumer enters an infinite 100% CPU death loop, clogging the channel and preventing valid orders behind it from being processed.

### 2. The FlashPass Solution
1. **Dead Letter Exchange Architecture:**
   - Dead Letter Exchange: `flashpass.dlx` (Direct).
   - Dead Letter Queue: `flashpass.order.dlq`.
   - Main queue configured with `x-dead-letter-exchange: flashpass.dlx`.
2. **Two-Strike Quarantine Policy:**
   - Consumers track retry counts via message headers.
   - Upon the 2nd unrecoverable failure, the message is rejected with `requeue=false`, causing RabbitMQ to automatically route it to the DLQ.
3. **Automatic Inventory Compensation:**
   When a message is quarantined into the DLQ, FlashPass atomically increments Redis stock by 1, ensuring no ticket is lost to corrupted payloads.
4. **Verification:** `TestEdgeCase5_RabbitMQDeadLetterQueue`.

---

## Scenario 6: Cache Stampede & SingleFlight Request Coalescing

### 1. The Distributed Failure Mechanics
During a high-concurrency drop, `GET /api/v1/events/:id` is requested by 10,000 users per second.
When the Redis cache key `flashpass:{id}:meta` expires:
- All 10,000 concurrent requests detect a cache miss simultaneously.
- All 10,000 requests bypass the cache and query PostgreSQL simultaneously.
- **Result:** Connection pool exhaustion (`max_connections = 50`), DB CPU spikes to 100%, cascading API failure.

### 2. The FlashPass Solution
FlashPass integrates Go's `golang.org/x/sync/singleflight`:
```go
val, err, _ := s.singleFlightGroup.Do(cacheKey, func() (interface{}, error) {
    // Exactly ONE goroutine queries the database
    event, err := s.eventRepo.GetByID(ctx, eventID)
    if err != nil {
        return nil, err
    }
    // Write back to Redis cache
    _ = s.stockCache.SetEventMeta(ctx, eventIDStr, event, 10*time.Minute)
    return event, nil
})
```
- While 10,000 requests arrive concurrently, `singleflight` coalesces them into **exactly 1 database query**.
- The single result is broadcasted to all 9,999 waiting goroutines.
- **Verification:** `TestEdgeCase6_CacheStampedeSingleFlight` launches 100 concurrent requests on an empty cache and proves database queries == 1.

---

## Scenario 7: Double Payment Webhook Concurrency & Distributed Lock

### 1. The Distributed Failure Mechanics
Payment gateways (Stripe, VNPay, PayPal) use automated retry workers. If a network acknowledgement is delayed:
- Webhook 1 arrives on Node A at $T = 0\text{ms}$.
- Webhook 2 arrives on Node B at $T = 2\text{ms}$.
- Both nodes query the database simultaneously, see `status = PENDING_PAYMENT`, and concurrently process credit, double-issuing receipts or firing duplicate notifications.

### 2. The FlashPass Solution
1. **Distributed Lock (`WatchdogLock`):**
   ```
   SET flashpass:lock:pay:{order_id} {owner_uuid} NX EX 5
   ```
   Only the winning thread executes payment processing. Losing threads wait 100ms for the transaction to commit.
2. **Idempotent Fallback Verification:**
   The update statement checks rows affected:
   ```sql
   UPDATE orders SET status = 'PAID' WHERE id = $1 AND status = 'PENDING_PAYMENT'
   ```
   If affected rows == 0, the code re-checks order status. If already `PAID`, it immediately returns HTTP `200 OK` ("Order already paid") rather than throwing an error, halting gateway retry storms.
3. **Verification:** `TestEdgeCase7_DoublePaymentWebhookConcurrency` fires 5 parallel webhooks for the same order and confirms exactly 1 update and 0 duplicate refunds.

---

## Scenario 8: NTP Clock Drift / Desynchronization & Payment Grace Window

### 1. The Distributed Failure Mechanics
In a distributed cloud deployment across multiple VMs or containers:
- Worker Node clock is skewed 3 seconds ahead of the API Node clock due to NTP desynchronization or VM hypervisor pause.
- A user submits payment at $T = 9\text{m}58\text{s}$.
- The Worker Node, running at $T = 10\text{m}01\text{s}$, aggressively marks the order as expired, leading to erroneous order cancellations and customer chargebacks.

### 2. The FlashPass Solution
1. **Clock Drift Grace Window ($\Delta = 5\text{s}$):**
   FlashPass provides a 5-second buffer during payment evaluation:
   $$\text{PaymentPermitted} \iff \text{Now} \le \text{ExpiresAt} + 5\text{s}$$
2. **Database-Authoritative Clock:**
   Worker expiration queries do not trust local worker system time. Expiration SQL requires PostgreSQL's authoritative server clock:
   ```sql
   UPDATE orders SET status = 'EXPIRED' 
   WHERE id = $1 AND status = 'PENDING_PAYMENT' AND expires_at <= NOW()
   ```
3. **Verification:** `TestEdgeCase8_ClockDriftGraceWindow` verifies that payments submitted 2 seconds past the raw deadline succeed under the grace window, and worker cancellation fails until PostgreSQL server clock agrees.

---

## Scenario 9: Bot Traffic Burst & Distributed Sliding-Window Rate Limiting

### 1. The Distributed Failure Mechanics
Scalper bot networks flood the booking endpoint with thousands of requests per second per IP or account, starving legitimate human buyers and exhausting Redis bandwidth.

### 2. The FlashPass Solution
FlashPass implements a **Distributed Sliding-Window Log Rate Limiter** via atomic Redis Lua script:
```lua
-- KEYS[1]: flashpass:{identifier}:ratelimit
-- ARGV[1]: now_ms, ARGV[2]: window_ms, ARGV[3]: max_limit, ARGV[4]: member_id
local key = KEYS[1]
local clear_before = tonumber(ARGV[1]) - tonumber(ARGV[2])
redis.call('ZREMRANGEBYSCORE', key, '-inf', clear_before)
local current_requests = redis.call('ZCARD', key)
if current_requests < tonumber(ARGV[3]) then
    redis.call('ZADD', key, ARGV[1], ARGV[4])
    redis.call('PEXPIRE', key, ARGV[2])
    return 1 -- Allowed
else
    return 0 -- Throttled
end
```
- Completely smooths traffic without the reset boundary vulnerabilities of Fixed Window limiters.
- Returns HTTP `429 Too Many Requests` with `Retry-After: 2` header, dropping bots before any database, queue, or ticket inventory logic is triggered.
- **Verification:** `TestEdgeCase9_SlidingWindowRateLimiter`.

---

## Scenario 10: Out-of-Order Message Processing & State Machine Inversion

### 1. The Distributed Failure Mechanics
Under asynchronous RabbitMQ consumption:
- **Sub-Case 10.1 (Early Payment Inversion):** A lightning-fast webhook `POST /orders/:id/pay` arrives before the background `order.create` worker has finished inserting the initial row in PostgreSQL. If the payment handler fails with `404 Not Found`, the customer is charged but the order is never confirmed.
- **Sub-Case 10.2 (Early Cancel Tombstone):** A user cancels an order before the order persistence worker has run. If the order creation runs later, it might "resurrect" the cancelled order back to `PENDING_PAYMENT`.

### 2. The FlashPass Solution
1. **Debounce Settle Wait for Early Payments:**
   If `GetByID` returns `ErrNotFound`, `PayOrder` pauses for 100ms and retries once, allowing in-flight persistence transactions to commit.
2. **Tombstone Upsert Pattern for Cancellations:**
   When an early cancel arrives, FlashPass inserts a tombstone with status `CANCELLED`.
   The `orderRepo.Create` query uses PostgreSQL `ON CONFLICT` with status preservation:
   ```sql
   INSERT INTO orders (id, event_id, user_id, idempotency_key, status, expires_at)
   VALUES ($1, $2, $3, $4, $5, $6)
   ON CONFLICT (id) DO UPDATE 
   SET status = CASE 
       WHEN orders.status = 'CANCELLED' THEN 'CANCELLED'
       ELSE EXCLUDED.status 
   END;
   ```
3. **Verification:** `TestEdgeCase10_OutOfOrderMessageProcessing`.

---

## Scenario 11: Database Cascading Failure & Circuit Breaker Protection

### 1. The Distributed Failure Mechanics
When PostgreSQL experiences disk I/O latency, lock contention, or network packet drops:
- API worker goroutines waiting on `pgxpool.Acquire()` stall.
- The connection pool exhausts (`50/50 connections active`).
- New incoming HTTP requests queue up in memory, memory consumption balloons, and the entire Go API crashes (OOM), taking down healthy Redis routes (`/health`, `/stock`) as well.

### 2. The FlashPass Solution
FlashPass protects the database tier with a custom, thread-safe **Circuit Breaker** (`backend/internal/resilience/circuit_breaker.go`):
```
           +-------------------------+
           |         CLOSED          |  <-- Normal operations
           +-------------------------+
             |                     ^
   3 Consecutive Failures          | 1 Canary Succeeded
             v                     |
           +-------------------------+
           |          OPEN           |  <-- Fast-fails in 0ms (HTTP 503)
           +-------------------------+
             |
       1.5s Cooldown Expired
             v
           +-------------------------+
           |        HALF-OPEN        |  <-- Admits 1 Canary Probe
           +-------------------------+
```
- Downstream database calls are wrapped in the circuit breaker.
- Once tripped, subsequent requests fail immediately in **0ms** returning HTTP `503 Service Unavailable` with `Retry-After: 2`, allowing PostgreSQL breathing room to recover without being hammered.
- **Verification:** `TestEdgeCase11_DatabaseCircuitBreaker`.

---

## Scenario 12: Split-Brain Distributed Lock & Lock Heartbeat Watchdog

### 1. The Distributed Failure Mechanics
Distributed locks typically rely on a fixed Time-To-Live (e.g. `SET lock 5s NX`).
If an external API call (payment gateway, credit check, slow disk write) takes 7 seconds:
- At $T = 5\text{s}$, Redis expires the lock key.
- Thread B arrives, finds the key vacant, and acquires the lock.
- Thread A and Thread B now execute simultaneously within the critical section (Split-Brain execution).
- At $T = 7\text{s}$, Thread A finishes and executes `DEL lock`, inadvertently deleting Thread B's lock!

### 2. The FlashPass Solution
FlashPass implements the **Redisson Watchdog Pattern** in Go (`AcquireLockWithWatchdog`):
1. **Unique Owner Token:** Every lock acquisition generates a cryptographically random UUID token (`SET key token NX EX ttl`).
2. **Heartbeat Goroutine:** A background watchdog goroutine periodically issues an atomic Lua renewal script every $\text{TTL}/3$:
   ```lua
   if redis.call('GET', KEYS[1]) == ARGV[1] then
       return redis.call('EXPIRE', KEYS[1], ARGV[2])
   else
       return 0
   end
   ```
3. **Safe Owner-Verified Release:** Only the holder of the owner token is permitted to delete the lock:
   ```lua
   if redis.call('GET', KEYS[1]) == ARGV[1] then
       return redis.call('DEL', KEYS[1])
   else
       return 0
   end
   ```
- Thread B can never acquire the lock while Thread A is running, and delayed threads can never hijack or prematurely delete locks owned by others.
- **Verification:** `TestEdgeCase12_DistributedLockWatchdog`.
