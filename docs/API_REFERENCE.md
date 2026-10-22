# FlashPass API Reference Specification (v1.0 Production)

> **Base URL:** `http://localhost:8080` (or `https://api.flashpass.io` in production)  
> **Protocol:** HTTP/1.1 & HTTP/2 (TLS 1.3)  
> **Format:** JSON (`application/json; charset=utf-8`)  
> **Character Encoding:** UTF-8  

---

## 📌 Standard HTTP Headers & Status Codes

### Request Headers
| Header | Type | Description | Mandatory |
| :--- | :--- | :--- | :---: |
| `Content-Type` | String | Must be `application/json` for write requests. | Yes (POST/PUT) |
| `Idempotency-Key` | UUIDv4 | Unique client request identifier preventing duplicate submissions. | Yes for `/tickets/book` |
| `Authorization` | String | `Bearer <JWT_TOKEN>` for authenticated buyer endpoints; `Bearer <ADMIN_SECRET>` for admin operations. | Conditional |
| `X-Signature` | String | Format: `t={timestamp_ms},v1={hmac_sha256}`. Mandatory for payment webhooks. | Yes for `/orders/:id/pay` |
| `traceparent` | String | W3C distributed trace identifier (`00-{trace_id}-{span_id}-{flags}`). | Recommended |
| `X-Forwarded-For` | IP | Client IP address used by Distributed Sliding-Window Rate Limiter. | Recommended |

### Response Headers
| Header | Description |
| :--- | :--- |
| `X-Response-Time` | Internal request processing duration (e.g. `1.2ms`). |
| `Retry-After` | Cooldown duration in seconds when HTTP 429 or 503 is returned. |
| `Content-Type` | `application/json; charset=utf-8` (or `text/event-stream` for SSE). |

### Standard HTTP Status Codes
| Code | Reason Phrase | Semantic Meaning in FlashPass |
| :--- | :--- | :--- |
| `200 OK` | OK | Query succeeded or payment processed successfully. |
| `202 Accepted` | Accepted | Ticket reservation accepted in RAM and enqueued for async persistence. |
| `400 Bad Request` | Bad Request | Invalid JSON payload, missing fields, or invalid UUID. |
| `401 Unauthorized` | Unauthorized | Missing/invalid JWT token, or Webhook HMAC signature invalid/expired. |
| `403 Forbidden` | Forbidden | Insufficient permissions to access administrative plane. |
| `404 Not Found` | Not Found | Event or Order ID does not exist. |
| `409 Conflict` | Conflict | **Fail-Fast:** Event is Sold Out or user has already reserved a ticket. |
| `429 Too Many Requests` | Too Many Requests | Rate limit exceeded. Bot/burst traffic throttled via Redis ZSET. |
| `503 Service Unavailable` | Service Unavailable | Circuit Breaker tripped (OPEN) due to downstream DB saturation. |

---

## 1. Public Health & Real-Time Monitoring Endpoints

### 1.1 Health Check (Liveness Probe)
Inspects API server availability for Kubernetes Liveness & Readiness probes.

- **Method:** `GET`
- **Path:** `/health`
- **Response:** `200 OK`
```json
{
  "status": "ok",
  "timestamp": "2026-10-22T13:45:00Z"
}
```

---

### 1.2 Get Event Metadata (SingleFlight Protected)
Fetches event configuration and details. Employs `singleflight.Group` to prevent cache stampedes on database cold misses.

- **Method:** `GET`
- **Path:** `/api/v1/events/:id`
- **URL Parameters:**
  - `id` (UUID): Unique event identifier.
- **Response:** `200 OK`
```json
{
  "id": "7f000001-92be-1fa2-8192-be2069cb0000",
  "title": "Concert Flash Sale - VIP Access",
  "total_tickets": 1000,
  "available_tickets": 420,
  "start_time": "2026-10-22T20:00:00Z",
  "end_time": "2026-10-22T23:59:59Z",
  "created_at": "2026-10-22T10:00:00Z"
}
```

---

### 1.3 Get Real-Time Available Stock (In-Memory)
Reads current available ticket inventory directly from Redis memory in `< 1ms`.

- **Method:** `GET`
- **Path:** `/api/v1/events/:id/stock`
- **URL Parameters:**
  - `id` (UUID): Unique event identifier.
- **Response:** `200 OK`
```json
{
  "event_id": "7f000001-92be-1fa2-8192-be2069cb0000",
  "stock": 420
}
```

---

### 1.4 Real-Time Stock Event Stream (Server-Sent Events)
Establishes a persistent SSE connection streaming live stock changes directly to frontend dashboards.

- **Method:** `GET`
- **Path:** `/api/v1/events/:id/stream`
- **Headers:** `Accept: text/event-stream`
- **Stream Output:**
```
event: stock_update
data: {"event_id":"7f000001-92be-1fa2-8192-be2069cb0000","stock":419,"timestamp":1729604700}

event: stock_update
data: {"event_id":"7f000001-92be-1fa2-8192-be2069cb0000","stock":418,"timestamp":1729604701}
```

---

## 2. High-Concurrency Flash Sale Reservation

### 2.1 Book Ticket (Primary Ingestion Endpoint)
Performs atomic check-and-decrement in Redis via Lua script. If successful, enqueues an asynchronous persistence job to RabbitMQ.

- **Method:** `POST`
- **Path:** `/api/v1/tickets/book`
- **Headers:**
  - `Content-Type: application/json`
  - `Authorization: Bearer <JWT_TOKEN>` (Authenticates user identity; in production, `user_id` is extracted strictly from the token's `sub` claim)
  - `Idempotency-Key: <UUIDv4>` (Unique per tab/session)
  - `traceparent: 00-{trace_id}-{span_id}-01` (Optional W3C TraceContext)
- **Request Body:**
```json
{
  "event_id": "7f000001-92be-1fa2-8192-be2069cb0000",
  "user_id": "user-884920",
  "idempotency_key": "c71e2efc-7729-4a92-9382-7b0e41cf77d0"
}
```

#### Responses:

##### `202 Accepted` — Reservation Successful
Returned when in-memory inventory was successfully decremented and the order is in-flight:
```json
{
  "order_id": "0192be23-5e92-70b1-9f22-48a04bce9431",
  "status": "PENDING_PAYMENT",
  "expires_in": 600,
  "message": "Reservation successful. Complete payment within 10 minutes."
}
```

##### `401 Unauthorized` — Missing / Invalid Auth Token
Returned if JWT Bearer token is absent, malformed, or expired:
```json
{
  "error": "Unauthorized: missing or invalid authentication token",
  "code": "AUTH_REQUIRED"
}
```

##### `409 Conflict` — Event Sold Out (Fail-Fast)
Returned in `< 1ms` directly from RAM without hitting DB:
```json
{
  "error": "Event is sold out",
  "code": "SOLD_OUT"
}
```

##### `409 Conflict` — User Already Booked
Enforces the 1-ticket-per-user constraint:
```json
{
  "error": "You have already reserved a ticket for this event",
  "code": "ALREADY_BOOKED"
}
```

##### `429 Too Many Requests` — Rate Limit Exceeded
Triggered when request frequency exceeds sliding window threshold (e.g., > 5 req/sec):
- **Response Headers:** `Retry-After: 2`
```json
{
  "error": "Too many requests. Please slow down and try again.",
  "retry_after": 2
}
```

##### `503 Service Unavailable` — Circuit Breaker Tripped
Returned in 0ms when database health degradation trips the circuit breaker:
- **Response Headers:** `Retry-After: 2`
```json
{
  "error": "Database service is temporarily unavailable. Please retry shortly.",
  "circuit_breaker": "OPEN"
}
```

---

## 3. Order Lifecycle & Payment Webhooks

### 3.1 Get Order Details
Queries order status, payment expiration timestamp, and ticket assignment.

- **Method:** `GET`
- **Path:** `/api/v1/orders/:id`
- **Response:** `200 OK`
```json
{
  "id": "0192be23-5e92-70b1-9f22-48a04bce9431",
  "event_id": "7f000001-92be-1fa2-8192-be2069cb0000",
  "user_id": "user-884920",
  "idempotency_key": "c71e2efc-7729-4a92-9382-7b0e41cf77d0",
  "status": "PENDING_PAYMENT",
  "expires_at": "2026-10-22T20:10:00Z",
  "created_at": "2026-10-22T20:00:00Z"
}
```

---

### 3.2 Pay Order (Payment Confirmation Webhook)
Called by payment gateways (Stripe, VNPay, ZaloPay) or mock checkout forms. Protected by Distributed Lock (`WatchdogLock`) to eliminate duplicate webhook execution, and cryptographically verified via HMAC-SHA256 signatures.

- **Method:** `POST`
- **Path:** `/api/v1/orders/:id/pay`
- **Headers:**
  - `Content-Type: application/json`
  - `X-Signature: t=1729605000000,v1=5d41402abc4b2a76b9719d911017c592...` (HMAC-SHA256 signature generated over canonical payload `timestamp.raw_body`)
- **URL Parameters:**
  - `id` (UUID): Order ID to confirm payment for.
- **Request Body (Optional metadata from Gateway):**
```json
{
  "gateway_transaction_id": "txn_9948201",
  "amount": 100.00,
  "currency": "USD"
}
```

#### Responses:

##### `200 OK` — Payment Confirmed
```json
{
  "order_id": "0192be23-5e92-70b1-9f22-48a04bce9431",
  "status": "PAID",
  "message": "Payment successful"
}
```

##### `200 OK` — Order Expired (Automatic Refund Triggered)
Returned when payment arrives past the 10-minute window and the ticket was re-released:
```json
{
  "order_id": "0192be23-5e92-70b1-9f22-48a04bce9431",
  "status": "REFUND_PENDING",
  "message": "Order has expired and ticket was released. Auto-refund initiated."
}
```

##### `401 Unauthorized` — Webhook Signature Mismatch or Replay Attack
Returned if HMAC signature fails constant-time validation or the timestamp is $> 300\text{s}$ skewed:
```json
{
  "error": "Invalid webhook cryptographic signature or expired timestamp",
  "code": "WEBHOOK_VERIFICATION_FAILED"
}
```

##### `404 Not Found` — Order Not Found
Returned if order does not exist even after internal debounce retry:
```json
{
  "error": "Order not found"
}
```

---

### 3.3 Cancel Order
Allows client to voluntarily surrender reservation, immediately returning stock to pool.

- **Method:** `POST`
- **Path:** `/api/v1/orders/:id/cancel`
- **Response:** `200 OK`
```json
{
  "order_id": "0192be23-5e92-70b1-9f22-48a04bce9431",
  "status": "CANCELLED",
  "message": "Order cancelled and ticket returned to pool"
}
```

---

## 4. Administrative & Self-Healing APIs

### 4.1 Pre-Warm Event Cache
Loads persistent inventory count from PostgreSQL into Redis RAM before sale opening.

- **Method:** `POST`
- **Path:** `/api/v1/admin/events/:id/prewarm`
- **Response:** `200 OK`
```json
{
  "event_id": "7f000001-92be-1fa2-8192-be2069cb0000",
  "stock": 1000,
  "message": "Event cache pre-warmed successfully"
}
```

---

### 4.2 Stock Auditor Reconcile (Ghost Inventory Healing)
Audits in-memory stock vs database active orders (`total - active_orders`), repairing discrepancies caused by network drops or process crashes.

- **Method:** `POST`
- **Path:** `/api/v1/admin/events/:id/reconcile`
- **Response:** `200 OK`
```json
{
  "event_id": "7f000001-92be-1fa2-8192-be2069cb0000",
  "old_redis_stock": 400,
  "reconciled_stock": 420,
  "discrepancy": 20,
  "message": "Stock reconciled successfully (ghost inventory healed)"
}
```

---

### 4.3 Unlock Orphaned User Reservation
Manually releases an orphaned `user_booked` lock if a reservation failed mid-flight without an active order.

- **Method:** `POST`
- **Path:** `/api/v1/admin/events/:id/users/:userId/unlock`
- **Response:** `200 OK`
```json
{
  "event_id": "7f000001-92be-1fa2-8192-be2069cb0000",
  "user_id": "user-884920",
  "message": "User lock successfully released"
}
```

---

### 4.4 Reset & Restock Event
Resets orders, cleans Redis keys, and updates inventory capacity for testing or demonstration.

- **Method:** `POST`
- **Path:** `/api/v1/admin/events/:id/reset`
- **Request Body (Optional):**
```json
{
  "tickets": 1000
}
```
- **Response:** `200 OK`
```json
{
  "message": "Event reset successfully",
  "total_tickets": 1000,
  "stock": 1000
}
```

---

## 5. Redis Sentinel & Chaos Engineering Endpoints

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/admin/redis/master/toggle` | Pauses/unpauses Redis Master Docker container (`docker pause/unpause`). |
| `POST` | `/api/v1/admin/redis/master/status` | Returns whether Master container is running or paused. |
| `POST` | `/api/v1/admin/failover/init` | Resets dual-node replication to 1 ticket on Master & Replica. |
| `POST` | `/api/v1/admin/failover/book-master` | Executes booking on Master with optional synchronous `WAIT 1`. |
| `POST` | `/api/v1/admin/failover/crash-master` | Simulates sudden master crash mid-replication. |
| `POST` | `/api/v1/admin/failover/promote-replica` | Instructs Replica to promote itself (`REPLICAOF NO ONE`). |
| `POST` | `/api/v1/admin/failover/book-replica` | Attempts booking on promoted Replica to prove overselling. |
| `GET` | `/api/v1/admin/failover/audit` | Audits real PostgreSQL `orders` table to definitively prove oversell rate. |

---

## 6. Complete cURL Examples

### Flash Sale Booking
```bash
curl -X POST http://localhost:8080/api/v1/tickets/book \
  -H "Content-Type: application/json" \
  -d '{
    "event_id": "7f000001-92be-1fa2-8192-be2069cb0000",
    "user_id": "buyer-001",
    "idempotency_key": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d"
  }'
```

### Confirming Payment
```bash
curl -X POST http://localhost:8080/api/v1/orders/0192be23-5e92-70b1-9f22-48a04bce9431/pay \
  -H "Content-Type: application/json"
```

### Self-Healing Stock Reconciliation
```bash
curl -X POST http://localhost:8080/api/v1/admin/events/7f000001-92be-1fa2-8192-be2069cb0000/reconcile \
  -H "Authorization: Bearer $ADMIN_SECRET"
```

---

## 7. Machine-Readable OpenAPI 3.1 Specification

The complete machine-readable contract for FlashPass is maintained in:
👉 [`docs/openapi.yaml`](./openapi.yaml)

You can import this file directly into **Swagger UI**, **Postman**, or generate client SDKs via `openapi-generator-cli`:
```bash
# Generate TypeScript Axios SDK for Frontend
openapi-generator-cli generate -i docs/openapi.yaml -g typescript-axios -o frontend/src/api-client

# Generate Go API Client
openapi-generator-cli generate -i docs/openapi.yaml -g go -o sdk/go
```
