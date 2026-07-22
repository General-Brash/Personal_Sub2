-- Independent mall switch and concurrency-safe per-user product purchase limits.

INSERT INTO settings (key, value, updated_at)
VALUES (
    'mall_enabled',
    COALESCE((SELECT value FROM settings WHERE key = 'payment_enabled' LIMIT 1), 'false'),
    NOW()
)
ON CONFLICT (key) DO NOTHING;

ALTER TABLE currency_products
    ADD COLUMN IF NOT EXISTS daily_purchase_limit INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_purchase_limit INT NOT NULL DEFAULT 0;

ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS daily_purchase_limit INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_purchase_limit INT NOT NULL DEFAULT 0;

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS daily_purchase_limit_snapshot INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_purchase_limit_snapshot INT NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'currency_products_purchase_limits_check') THEN
        ALTER TABLE currency_products ADD CONSTRAINT currency_products_purchase_limits_check
            CHECK (daily_purchase_limit >= 0 AND total_purchase_limit >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'subscription_plans_purchase_limits_check') THEN
        ALTER TABLE subscription_plans ADD CONSTRAINT subscription_plans_purchase_limits_check
            CHECK (daily_purchase_limit >= 0 AND total_purchase_limit >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'payment_orders_purchase_limit_snapshots_check') THEN
        ALTER TABLE payment_orders ADD CONSTRAINT payment_orders_purchase_limit_snapshots_check
            CHECK (daily_purchase_limit_snapshot >= 0 AND total_purchase_limit_snapshot >= 0);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS payment_purchase_counters (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    product_type VARCHAR(20) NOT NULL,
    product_id BIGINT NOT NULL,
    period_type VARCHAR(10) NOT NULL,
    period_start DATE NOT NULL,
    reserved_count INT NOT NULL DEFAULT 0,
    consumed_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT payment_purchase_counters_product_type_check CHECK (product_type IN ('currency', 'subscription')),
    CONSTRAINT payment_purchase_counters_period_type_check CHECK (period_type IN ('daily', 'total')),
    CONSTRAINT payment_purchase_counters_counts_check CHECK (reserved_count >= 0 AND consumed_count >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS payment_purchase_counters_scope_key
    ON payment_purchase_counters (user_id, product_type, product_id, period_type, period_start);
CREATE INDEX IF NOT EXISTS payment_purchase_counters_user_period_idx
    ON payment_purchase_counters (user_id, period_type, period_start);

CREATE TABLE IF NOT EXISTS payment_purchase_reservations (
    id BIGSERIAL PRIMARY KEY,
    order_id BIGINT NOT NULL UNIQUE,
    user_id BIGINT NOT NULL,
    product_type VARCHAR(20) NOT NULL,
    product_id BIGINT NOT NULL,
    daily_period_start DATE NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'reserved',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT payment_purchase_reservations_product_type_check CHECK (product_type IN ('currency', 'subscription')),
    CONSTRAINT payment_purchase_reservations_status_check CHECK (status IN ('reserved', 'consumed', 'released'))
);

CREATE INDEX IF NOT EXISTS payment_purchase_reservations_lookup_idx
    ON payment_purchase_reservations (user_id, product_type, product_id, status);

-- Preserve pre-upgrade product purchases so enabling a limit later cannot reset history.
INSERT INTO payment_purchase_reservations (
    order_id, user_id, product_type, product_id, daily_period_start, status, created_at, updated_at
)
SELECT
    id,
    user_id,
    CASE WHEN currency_product_id IS NOT NULL THEN 'currency' ELSE 'subscription' END,
    COALESCE(currency_product_id, plan_id),
    (COALESCE(paid_at, created_at) AT TIME ZONE 'Asia/Shanghai')::date,
    CASE
        WHEN status = 'PENDING' THEN 'reserved'
        WHEN paid_at IS NOT NULL AND status <> 'REFUNDED' THEN 'consumed'
        ELSE 'released'
    END,
    created_at,
    updated_at
FROM payment_orders
WHERE currency_product_id IS NOT NULL
   OR (order_type = 'subscription' AND plan_id IS NOT NULL)
ON CONFLICT (order_id) DO NOTHING;

INSERT INTO payment_purchase_counters (
    user_id, product_type, product_id, period_type, period_start,
    reserved_count, consumed_count, created_at, updated_at
)
SELECT
    user_id,
    product_type,
    product_id,
    'daily',
    daily_period_start,
    COUNT(*) FILTER (WHERE status = 'reserved'),
    COUNT(*) FILTER (WHERE status = 'consumed'),
    NOW(),
    NOW()
FROM payment_purchase_reservations
WHERE status <> 'released'
GROUP BY user_id, product_type, product_id, daily_period_start
ON CONFLICT (user_id, product_type, product_id, period_type, period_start) DO NOTHING;

INSERT INTO payment_purchase_counters (
    user_id, product_type, product_id, period_type, period_start,
    reserved_count, consumed_count, created_at, updated_at
)
SELECT
    user_id,
    product_type,
    product_id,
    'total',
    DATE '1970-01-01',
    COUNT(*) FILTER (WHERE status = 'reserved'),
    COUNT(*) FILTER (WHERE status = 'consumed'),
    NOW(),
    NOW()
FROM payment_purchase_reservations
WHERE status <> 'released'
GROUP BY user_id, product_type, product_id
ON CONFLICT (user_id, product_type, product_id, period_type, period_start) DO NOTHING;
