-- Purchase-limit policy periods and rolling-window event history.
-- This migration is additive and idempotent. Existing daily limits keep
-- their natural-day semantics through the day/calendar/1 defaults.

ALTER TABLE currency_products
    ADD COLUMN IF NOT EXISTS purchase_limit_unit VARCHAR(10) NOT NULL DEFAULT 'day',
    ADD COLUMN IF NOT EXISTS purchase_limit_mode VARCHAR(10) NOT NULL DEFAULT 'calendar',
    ADD COLUMN IF NOT EXISTS purchase_limit_window_size INT NOT NULL DEFAULT 1;

ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS purchase_limit_unit VARCHAR(10) NOT NULL DEFAULT 'day',
    ADD COLUMN IF NOT EXISTS purchase_limit_mode VARCHAR(10) NOT NULL DEFAULT 'calendar',
    ADD COLUMN IF NOT EXISTS purchase_limit_window_size INT NOT NULL DEFAULT 1;

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS purchase_limit_unit_snapshot VARCHAR(10) NOT NULL DEFAULT 'day',
    ADD COLUMN IF NOT EXISTS purchase_limit_mode_snapshot VARCHAR(10) NOT NULL DEFAULT 'calendar',
    ADD COLUMN IF NOT EXISTS purchase_limit_window_size_snapshot INT NOT NULL DEFAULT 1;

ALTER TABLE payment_purchase_reservations
    ADD COLUMN IF NOT EXISTS period_type VARCHAR(10) NOT NULL DEFAULT 'daily';

UPDATE currency_products
SET purchase_limit_unit = 'day'
WHERE purchase_limit_unit IS NULL;
UPDATE currency_products
SET purchase_limit_mode = 'calendar'
WHERE purchase_limit_mode IS NULL;
UPDATE currency_products
SET purchase_limit_window_size = 1
WHERE purchase_limit_window_size IS NULL;

UPDATE subscription_plans
SET purchase_limit_unit = 'day'
WHERE purchase_limit_unit IS NULL;
UPDATE subscription_plans
SET purchase_limit_mode = 'calendar'
WHERE purchase_limit_mode IS NULL;
UPDATE subscription_plans
SET purchase_limit_window_size = 1
WHERE purchase_limit_window_size IS NULL;

UPDATE payment_orders
SET purchase_limit_unit_snapshot = 'day'
WHERE purchase_limit_unit_snapshot IS NULL;
UPDATE payment_orders
SET purchase_limit_mode_snapshot = 'calendar'
WHERE purchase_limit_mode_snapshot IS NULL;
UPDATE payment_orders
SET purchase_limit_window_size_snapshot = 1
WHERE purchase_limit_window_size_snapshot IS NULL;

UPDATE payment_purchase_reservations
SET period_type = 'daily'
WHERE period_type IS NULL;

ALTER TABLE currency_products
    ALTER COLUMN purchase_limit_unit SET DEFAULT 'day',
    ALTER COLUMN purchase_limit_mode SET DEFAULT 'calendar',
    ALTER COLUMN purchase_limit_window_size SET DEFAULT 1,
    ALTER COLUMN purchase_limit_unit SET NOT NULL,
    ALTER COLUMN purchase_limit_mode SET NOT NULL,
    ALTER COLUMN purchase_limit_window_size SET NOT NULL;

ALTER TABLE subscription_plans
    ALTER COLUMN purchase_limit_unit SET DEFAULT 'day',
    ALTER COLUMN purchase_limit_mode SET DEFAULT 'calendar',
    ALTER COLUMN purchase_limit_window_size SET DEFAULT 1,
    ALTER COLUMN purchase_limit_unit SET NOT NULL,
    ALTER COLUMN purchase_limit_mode SET NOT NULL,
    ALTER COLUMN purchase_limit_window_size SET NOT NULL;

ALTER TABLE payment_orders
    ALTER COLUMN purchase_limit_unit_snapshot SET DEFAULT 'day',
    ALTER COLUMN purchase_limit_mode_snapshot SET DEFAULT 'calendar',
    ALTER COLUMN purchase_limit_window_size_snapshot SET DEFAULT 1,
    ALTER COLUMN purchase_limit_unit_snapshot SET NOT NULL,
    ALTER COLUMN purchase_limit_mode_snapshot SET NOT NULL,
    ALTER COLUMN purchase_limit_window_size_snapshot SET NOT NULL;

ALTER TABLE payment_purchase_reservations
    ALTER COLUMN period_type SET DEFAULT 'daily',
    ALTER COLUMN period_type SET NOT NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'payment_purchase_counters_period_type_check'
    ) THEN
        ALTER TABLE payment_purchase_counters
            DROP CONSTRAINT payment_purchase_counters_period_type_check;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'payment_purchase_counters_period_type_check'
    ) THEN
        ALTER TABLE payment_purchase_counters
            ADD CONSTRAINT payment_purchase_counters_period_type_check
            CHECK (period_type IN ('daily', 'weekly', 'monthly', 'rolling', 'total'));
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'currency_products_purchase_limit_policy_check'
    ) THEN
        ALTER TABLE currency_products
            ADD CONSTRAINT currency_products_purchase_limit_policy_check
            CHECK (
                purchase_limit_unit IN ('day', 'week', 'month')
                AND purchase_limit_mode IN ('calendar', 'rolling')
                AND purchase_limit_window_size > 0
                AND (purchase_limit_mode = 'rolling' OR purchase_limit_window_size = 1)
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'subscription_plans_purchase_limit_policy_check'
    ) THEN
        ALTER TABLE subscription_plans
            ADD CONSTRAINT subscription_plans_purchase_limit_policy_check
            CHECK (
                purchase_limit_unit IN ('day', 'week', 'month')
                AND purchase_limit_mode IN ('calendar', 'rolling')
                AND purchase_limit_window_size > 0
                AND (purchase_limit_mode = 'rolling' OR purchase_limit_window_size = 1)
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'payment_orders_purchase_limit_policy_snapshot_check'
    ) THEN
        ALTER TABLE payment_orders
            ADD CONSTRAINT payment_orders_purchase_limit_policy_snapshot_check
            CHECK (
                purchase_limit_unit_snapshot IN ('day', 'week', 'month')
                AND purchase_limit_mode_snapshot IN ('calendar', 'rolling')
                AND purchase_limit_window_size_snapshot > 0
                AND (purchase_limit_mode_snapshot = 'rolling' OR purchase_limit_window_size_snapshot = 1)
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'payment_purchase_reservations_period_type_check'
    ) THEN
        ALTER TABLE payment_purchase_reservations
            ADD CONSTRAINT payment_purchase_reservations_period_type_check
            CHECK (period_type IN ('daily', 'weekly', 'monthly', 'rolling', 'total'));
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS payment_purchase_limit_events (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    product_type VARCHAR(20) NOT NULL,
    product_id BIGINT NOT NULL,
    source_type VARCHAR(20) NOT NULL,
    source_id BIGINT NOT NULL,
    period_type VARCHAR(10) NOT NULL DEFAULT 'daily',
    period_start DATE,
    status VARCHAR(20) NOT NULL DEFAULT 'reserved',
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT payment_purchase_limit_events_product_type_check
        CHECK (product_type IN ('currency', 'subscription')),
    CONSTRAINT payment_purchase_limit_events_period_type_check
        CHECK (period_type IN ('daily', 'weekly', 'monthly', 'rolling', 'total')),
    CONSTRAINT payment_purchase_limit_events_status_check
        CHECK (status IN ('reserved', 'consumed', 'released'))
);

CREATE UNIQUE INDEX IF NOT EXISTS payment_purchase_limit_events_source_key
    ON payment_purchase_limit_events (source_type, source_id);
CREATE INDEX IF NOT EXISTS payment_purchase_limit_events_user_product_status_time_idx
    ON payment_purchase_limit_events (user_id, product_type, product_id, status, occurred_at);
CREATE INDEX IF NOT EXISTS payment_purchase_limit_events_active_time_idx
    ON payment_purchase_limit_events (user_id, product_type, product_id, occurred_at)
    WHERE status IN ('reserved', 'consumed');

CREATE INDEX IF NOT EXISTS payment_purchase_reservations_scope_idx
    ON payment_purchase_reservations (user_id, product_type, product_id, period_type, status);