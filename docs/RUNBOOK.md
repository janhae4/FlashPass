# FlashPass Production Operational Runbook & Incident Playbook

> **Target Audience:** Site Reliability Engineers (SRE), DevOps, Backend On-Call Engineers  
> **System:** FlashPass Distributed Ticketing Engine (v1.0 Production)  
> **Classification:** Confidential - Internal Engineering Operations  

---

## 📌 Table of Contents

- [1. Operational Architecture & SLA](#1-operational-architecture--sla)
- [2. Monitoring & Alerting Matrix](#2-monitoring--alerting-matrix)
- [3. Standard Operating Procedures (SOP)](#3-standard-operating-procedures-sop)
  - [SOP-01: Pre-Event Checklist & Cache Pre-Warming](#sop-01-pre-event-checklist--cache-pre-warming)
  - [SOP-02: Scaling Background Consumers](#sop-02-scaling-background-consumers)
  - [SOP-03: Graceful Maintenance & Zero-Downtime Deployment](#sop-03-graceful-maintenance--zero-downtime-deployment)
- [4. Emergency Incident Playbooks (SEV-1 / SEV-2)](#4-emergency-incident-playbooks-sev-1--sev-2)
  - [PLAYBOOK-01: Redis Master Failure & Sentinel Promotion](#playbook-01-redis-master-failure--sentinel-promotion)
  - [PLAYBOOK-02: RabbitMQ Poison Pill & Dead Letter Queue (DLQ) Quarantine](#playbook-02-rabbitmq-poison-pill--dead-letter-queue-dlq-quarantine)
  - [PLAYBOOK-03: Database Cascading Failure & Circuit Breaker Recovery](#playbook-03-database-cascading-failure--circuit-breaker-recovery)
  - [PLAYBOOK-04: Ghost Inventory Discrepancy & Stock Auditor Alert](#playbook-04-ghost-inventory-discrepancy--stock-auditor-alert)
  - [PLAYBOOK-05: Bot Traffic Surge & IP Rate-Limit Throttling](#playbook-05-bot-traffic-surge--ip-rate-limit-throttling)
- [5. Diagnostic Commands Cheatsheet](#5-diagnostic-commands-cheatsheet)

---

## 1. Operational Architecture & SLA

FlashPass runs a decoupled architecture designed for high availability:
- **Stateless Ingestion Tier:** Go Fiber API nodes running behind an L4/L7 load balancer (NGINX / AWS ALB).
- **In-Memory Cache & Gating Tier:** Redis 7 Master-Replica with Redis Sentinel on port `26379`.
- **Asynchronous Message Queue:** RabbitMQ cluster with mirrored queues and dead-letter exchange (`flashpass.dlx`).
- **Persistent Storage:** PostgreSQL 16 with Read Replicas and PgBouncer connection pooling.

### Production SLAs:
- **Availability:** 99.99% during ticket flash sales.
- **Overselling Tolerance:** Strictly **0.00%**.
- **Ingestion Latency:** p95 < 25ms, p99 < 50ms.
- **Fail-Fast Latency:** < 2ms for out-of-stock requests.

---

## 2. Monitoring & Alerting Matrix

| Severity | Alert Rule | PromQL Trigger Expression | Immediate Action |
| :---: | :--- | :--- | :--- |
| **SEV-1** | `RedisMasterDown` | `redis_up{role="master"} == 0` for 3s | Sentinel promotes replica; verify API reconnect. |
| **SEV-1** | `DatabaseCircuitBreakerOpen` | `flashpass_circuit_breaker_state{name="postgres"} == 1` | Investigate PostgreSQL CPU, locks, and connection pool. |
| **SEV-1** | `InventoryOversoldDetected` | `flashpass_active_orders > flashpass_event_total_tickets` | **CRITICAL:** Halt sales, initiate audit script. |
| **SEV-2** | `RabbitMQDLQAccumulation` | `flashpass_rabbitmq_queue_depth{queue="flashpass.order.dlq"} > 5` | Inspect poison pill payloads; debug database errors. |
| **SEV-2** | `StockDiscrepancyHigh` | `abs(flashpass_stock_discrepancy) > 10` for 1m | Run `StockAuditor` reconciliation endpoint. |
| **SEV-3** | `RateLimitSpike` | `rate(flashpass_bookings_total{status="rate_limited"}[1m]) / rate(flashpass_bookings_total[1m]) > 0.15` | Investigate client IP subnet for bot attacks. |
| **SEV-3** | `HighQueueLatency` | `flashpass_rabbitmq_queue_depth{queue="flashpass.order.create.queue"} > 5000` for 60s | Scale worker consumer replica pods. |

### Prometheus Alertmanager Rules Configuration (`alerts.yaml`)
```yaml
groups:
  - name: flashpass_alerts
    rules:
      - alert: DatabaseCircuitBreakerOpen
        expr: flashpass_circuit_breaker_state{name="postgres"} == 1
        for: 0s
        labels:
          severity: critical
        annotations:
          summary: "Database Circuit Breaker is OPEN"
          description: "Fiber API has fast-failed database writes. PostgreSQL may be saturated."

      - alert: InventoryOversoldDetected
        expr: flashpass_active_orders > on(event_id) flashpass_event_total_tickets
        for: 0s
        labels:
          severity: page
        annotations:
          summary: "Zero-Overselling Invariant Violated!"
          description: "Active orders in DB exceed total event tickets. Immediate manual intervention required."
```

---

## 3. Standard Operating Procedures (SOP)

### SOP-01: Pre-Event Checklist & Cache Pre-Warming
*To be executed 30 minutes before any scheduled flash sale opening.*

1. **Verify Infrastructure Health:**
   ```bash
   # Check container status
   docker compose ps
   
   # Verify Redis Sentinel sees Master and 1 Replica
   docker exec -it flashpass_redis_sentinel redis-cli -p 26379 sentinel master mymaster
   ```
2. **Execute Pre-Warming via Admin API:**
   ```bash
   curl -X POST http://localhost:8080/api/v1/admin/events/{EVENT_ID}/prewarm \
     -H "Authorization: Bearer $ADMIN_SECRET"
   ```
3. **Verify Redis Stock in RAM:**
   ```bash
   docker exec -it flashpass_redis redis-cli GET "flashpass:{EVENT_ID}:stock"
   ```
4. **Purge Test Idempotency Keys:** Ensure no residual test booking locks exist in Redis.

---

### SOP-02: Scaling Background Consumers
*Executed when RabbitMQ `flashpass.order.create.queue` depth exceeds 2,000 in-flight messages.*

1. **Check Queue Depth in RabbitMQ:**
   ```bash
   docker exec -it flashpass_rabbitmq rabbitmqctl list_queues name messages consumers
   ```
2. **Scale Worker Processes:**
   If running on Docker Compose or Kubernetes, increase worker replica count:
   ```bash
   # Docker Compose scale
   docker compose up -d --scale worker=4
   
   # Or in Kubernetes
   kubectl scale deployment flashpass-worker --replicas=8
   ```
3. **Verify PostgreSQL Connection Pool Headroom:** Ensure `max_connections` in PostgreSQL is not exceeded when scaling workers.

---

### SOP-03: Graceful Maintenance & Zero-Downtime Deployment
FlashPass background consumers support graceful shutdown via `sync.WaitGroup` and `SIGTERM` signals:

1. Send `SIGTERM` to worker instances.
2. The consumer stops accepting new deliveries from RabbitMQ and finishes processing in-flight database transactions.
3. Once in-flight messages are committed, the process exits cleanly (status 0).
4. Update container images and restart services.

---

### SOP-04: Disaster Recovery (DR) Failover Procedure (RTO < 30s, RPO < 1s)
*Executed during catastrophic loss of the primary availability zone or database host.*

1. **Verify Replica Lag & Promote PostgreSQL Standby:**
   ```bash
   # Check WAL replication lag on standby
   psql -U postgres -c "SELECT pg_last_wal_receive_lsn() - pg_last_wal_replay_lsn() AS bytes_lag;"
   
   # Promote Standby to Primary (if Patroni auto-election didn't trigger)
   pg_ctl promote -D /var/lib/postgresql/data
   ```
2. **Promote Redis Sentinel Replica:**
   ```bash
   docker exec -it flashpass_redis_sentinel redis-cli -p 26379 SENTINEL failover mymaster
   ```
3. **Verify API Connection Redirect:**
   The Go Fiber API connects via Sentinel and `pgxpool`, automatically establishing connections to the newly promoted primary nodes within 500ms.
4. **Run Stock Auditor Healing:**
   Immediately execute `POST /api/v1/admin/events/{EVENT_ID}/reconcile` to restore any in-flight discrepancies.

---

### SOP-05: Database Partition Maintenance & Cold Archival
*Automated maintenance job run on the 1st of every month.*

1. **Pre-Create Next Month's Orders Partition:**
   ```sql
   CREATE TABLE IF NOT EXISTS orders_2026_11 PARTITION OF orders_partitioned
       FOR VALUES FROM ('2026-11-01 00:00:00+00') TO ('2026-12-01 00:00:00+00');
   ```
2. **Archive Expired/Paid Partitions Older Than 90 Days to S3:**
   ```bash
   # Export old partition to Parquet format
   COPY (SELECT * FROM orders_2026_07 WHERE status IN ('PAID', 'EXPIRED', 'REFUNDED')) 
   TO PROGRAM 'aws s3 cp - s3://flashpass-cold-storage/orders/2026_07.parquet' (FORMAT PARQUET);
   
   # Detach and drop old partition from OLTP database
   ALTER TABLE orders_partitioned DETACH PARTITION orders_2026_07;
   DROP TABLE orders_2026_07;
   ```

---

## 4. Emergency Incident Playbooks (SEV-1 / SEV-2)

### PLAYBOOK-01: Redis Master Failure & Sentinel Promotion
**Symptoms:** Redis connection timeouts, alerts on `RedisMasterDown`.

1. **Step 1: Check Sentinel Status:**
   ```bash
   docker exec -it flashpass_redis_sentinel redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster
   ```
2. **Step 2: Force Manual Failover (if auto-failover stalled):**
   ```bash
   docker exec -it flashpass_redis_sentinel redis-cli -p 26379 SENTINEL failover mymaster
   ```
3. **Step 3: Verify Go API Reconnection:**
   The backend Redis client pool automatically polls Sentinel and redirects writes to the newly promoted Master without service restarts.
4. **Step 4: Execute Post-Failover Stock Audit:**
   Run Stock Reconciliation to correct any unsynced asynchronous writes:
   ```bash
   curl -X POST http://localhost:8080/api/v1/admin/events/{EVENT_ID}/reconcile
   ```

---

### PLAYBOOK-02: RabbitMQ Poison Pill & Dead Letter Queue (DLQ) Quarantine
**Symptoms:** `flashpass.order.dlq` has accumulating unacknowledged messages.

1. **Step 1: Inspect DLQ Messages in RabbitMQ:**
   Visit the RabbitMQ Management UI (`http://localhost:15672`, credentials `guest/guest`) or query CLI:
   ```bash
   docker exec -it flashpass_rabbitmq rabbitmqadmin get queue=flashpass.order.dlq count=5
   ```
2. **Step 2: Analyze Failure Reason:**
   FlashPass automatically logs poison pills with headers detailing failure causes (e.g., malformed UUID, database constraint violation).
3. **Step 3: Stock Rollback Verification:**
   FlashPass automatically increments Redis stock when quarantine occurs, preventing ghost inventory. Verify via:
   ```bash
   curl http://localhost:8080/api/v1/events/{EVENT_ID}/stock
   ```
4. **Step 4: Purge or Reprocess DLQ:**
   Once code bugs are patched, replay messages back to `flashpass.order.create.queue` or purge dead payloads.

---

### PLAYBOOK-03: Database Cascading Failure & Circuit Breaker Recovery
**Symptoms:** HTTP 503 returned on order persistence endpoints; Circuit Breaker state is `OPEN`.

1. **Step 1: Verify PostgreSQL Health:**
   ```bash
   docker exec -it flashpass_postgres psql -U postgres -d flashpass -c "SELECT count(*), state FROM pg_stat_activity GROUP BY state;"
   ```
2. **Step 2: Terminate Stuck / Long-Running Queries:**
   ```sql
   SELECT pg_terminate_backend(pid) 
   FROM pg_stat_activity 
   WHERE state = 'active' AND query_start < NOW() - INTERVAL '30 seconds';
   ```
3. **Step 3: Monitor Circuit Breaker Recovery:**
   FlashPass enters `HALF-OPEN` after a 1.5s cooldown and sends a single canary request.
   - If the canary succeeds $\to$ Circuit resets to `CLOSED`.
   - If the canary fails $\to$ Trips back to `OPEN` for another cooldown cycle without hammering the database.

---

### PLAYBOOK-04: Ghost Inventory Discrepancy & Stock Auditor Alert
**Symptoms:** `Redis Stock + Pending Orders + Paid Orders < Total Event Tickets`.

1. **Step 1: Check Current Database vs Redis Inventory:**
   ```sql
   SELECT 
     (SELECT total_tickets FROM events WHERE id = '7f000001-92be-1fa2-8192-be2069cb0000') AS total,
     COUNT(*) FILTER (WHERE status IN ('PENDING_PAYMENT', 'PAID')) AS active_orders
   FROM orders 
   WHERE event_id = '7f000001-92be-1fa2-8192-be2069cb0000';
   ```
2. **Step 2: Trigger Manual Self-Healing Reconcile:**
   ```bash
   curl -X POST http://localhost:8080/api/v1/admin/events/{EVENT_ID}/reconcile
   ```
   *The response will detail the old Redis stock, the new reconciled stock, and the exact discrepancy recovered.*
3. **Step 3: Release Stuck User Locks (if any user is unable to re-book):**
   ```bash
   curl -X POST http://localhost:8080/api/v1/admin/events/{EVENT_ID}/users/{USER_ID}/unlock
   ```

---

### PLAYBOOK-05: Bot Traffic Surge & IP Rate-Limit Throttling
**Symptoms:** HTTP 429 responses spiking; server CPU elevated.

1. **Step 1: Inspect Redis Rate-Limit Keys:**
   ```bash
   docker exec -it flashpass_redis redis-cli KEYS "flashpass:ratelimit:*"
   ```
2. **Step 2: Block Malicious IP Subnet at Gateway:**
   Add IP blacklist rules in Cloudflare or NGINX ingress:
   ```nginx
   deny 198.51.100.0/24;
   ```
3. **Step 3: Temporarily Tune Rate Limit Window:**
   Update `RATE_LIMIT_BURST` or window parameters in configuration if legitimate flash sale traffic is falsely throttled.

---

## 5. Diagnostic Commands Cheatsheet

```bash
# 1. Quick Cluster Status Check
docker compose ps

# 2. Redis Live Operations Monitor
docker exec -it flashpass_redis redis-cli MONITOR

# 3. Redis Sentinel Current Master
docker exec -it flashpass_redis_sentinel redis-cli -p 26379 SENTINEL masters

# 4. RabbitMQ Queue Message Count
docker exec -it flashpass_rabbitmq rabbitmqctl list_queues name messages_ready messages_unacknowledged

# 5. PostgreSQL Active Locks
docker exec -it flashpass_postgres psql -U postgres -d flashpass -c "SELECT pid, locktype, mode, granted FROM pg_locks WHERE NOT granted;"

# 6. Execute Edge Case Automated Test Suite
cd backend && go test -v -run "TestEdgeCase" ./internal/api
```
