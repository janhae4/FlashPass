-- Table: refund_orders
CREATE TABLE IF NOT EXISTS refund_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    user_id VARCHAR(64) NOT NULL,
    amount BIGINT NOT NULL DEFAULT 0,
    reason VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    retry_count INT NOT NULL DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 5,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    idempotency_key VARCHAR(128) UNIQUE NOT NULL,
    last_error TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_refund_orders_retry ON refund_orders(status, next_retry_at);
CREATE INDEX IF NOT EXISTS idx_refund_orders_order_id ON refund_orders(order_id);
