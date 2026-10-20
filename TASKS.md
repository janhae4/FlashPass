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
- [ ] 3.1 Implement Admin Pre-warm Endpoint `POST /api/v1/admin/events/:id/prewarm` (loads stock into Redis).
- [ ] 3.2 Implement Flash Sale Booking Endpoint `POST /api/v1/tickets/book`:
  - Validate input payload & idempotency key.
  - Execute Redis atomic Lua script.
  - Publish `order.create` event to RabbitMQ.
  - Return `202 Accepted` with reservation token.
- [ ] 3.3 Implement `GET /api/v1/events/:id/stock` for live inventory inspection.

## Phase 4: Asynchronous Processing & Stock Compensation
- [ ] 4.1 Implement RabbitMQ Order Consumer to write orders into PostgreSQL.
- [ ] 4.2 Publish delayed message (TTL 10 min) to `flashpass.order.delayed.exchange`.
- [ ] 4.3 Implement Timeout Worker: If order unpaid after 10 min, mark `EXPIRED` and increment Redis stock.
- [ ] 4.4 Implement Payment Mock Endpoint `POST /api/v1/orders/:id/pay` (transitions status to `PAID`).

## Phase 5: Interactive Frontend Client & Live Demo Dashboard (Next.js 15)
- [ ] 5.1 Initialize Next.js 15 frontend application with Tailwind CSS and Lucide icons.
- [ ] 5.2 Build Event Booking Page:
  - Event banner, ticket tier details, and countdown timer to sale opening.
  - Real-time inventory progress bar (synced via polling or SSE/WebSocket).
  - High-concurrency "Mua Vé Ngay" button with client-side UUID generation (`idempotency_key`).
- [ ] 5.3 Build Reservation Modal & Payment Flow:
  - 10-minute live countdown timer for pending payment.
  - "Thanh toán giả lập (Mock Pay)" button calling `POST /api/v1/orders/:id/pay`.
  - Visual feedback when ticket expires and is released back to stock.
- [ ] 5.4 Build Admin Live Monitoring Dashboard:
  - Gauge cards: Current Redis Stock, Queued Orders, Paid Orders, Expired Orders.
  - One-click "Pre-warm Stock" and "Restock Event" buttons for live interview presentations.

## Phase 6: Benchmark & Load Testing with k6
- [ ] 6.1 Write `load-test.js` targeting 20,000 virtual requests hitting 1,000 inventory stock.
- [ ] 6.2 Execute test and collect metrics: RPS, p95/p99 latency, HTTP 202 count vs HTTP 409 count.
- [ ] 6.3 Verify PostgreSQL count: Exactly 1,000 orders created (0 overselling).
- [ ] 6.4 Generate `BENCHMARK.md` report with graphs and summary for GitHub README.
