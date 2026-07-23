-- Financial ledger snapshots, mall product-name history, and marginal bank
-- exchange tiers. Monetary values keep the existing NUMERIC(20,8) contract.

ALTER TABLE mall_purchases
    ADD COLUMN IF NOT EXISTS product_name VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS permanent_balance_before NUMERIC(20,8),
    ADD COLUMN IF NOT EXISTS permanent_balance_after NUMERIC(20,8),
    ADD COLUMN IF NOT EXISTS temporary_balance_before NUMERIC(20,8),
    ADD COLUMN IF NOT EXISTS temporary_balance_after NUMERIC(20,8);

UPDATE mall_purchases AS purchase
SET product_name = COALESCE(
    CASE
        WHEN purchase.product_type = 'currency' THEN (
            SELECT product.name FROM currency_products AS product WHERE product.id = purchase.product_id
        )
        WHEN purchase.product_type = 'subscription' THEN (
            SELECT plan.name FROM subscription_plans AS plan WHERE plan.id = purchase.product_id
        )
    END,
    purchase.product_name
)
WHERE purchase.product_name = '';

CREATE INDEX IF NOT EXISTS mall_purchases_admin_created_idx
    ON mall_purchases (created_at DESC, id DESC);

ALTER TABLE bank_ledger
    ADD COLUMN IF NOT EXISTS permanent_balance_before NUMERIC(20,8),
    ADD COLUMN IF NOT EXISTS permanent_balance_after NUMERIC(20,8),
    ADD COLUMN IF NOT EXISTS temporary_balance_before NUMERIC(20,8),
    ADD COLUMN IF NOT EXISTS temporary_balance_after NUMERIC(20,8);

CREATE INDEX IF NOT EXISTS bank_ledger_admin_created_idx
    ON bank_ledger (created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS bank_exchange_daily_usage (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    usage_date DATE NOT NULL,
    permanent_exchanged NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (permanent_exchanged >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, usage_date)
);

CREATE INDEX IF NOT EXISTS bank_exchange_daily_usage_date_idx
    ON bank_exchange_daily_usage (usage_date, user_id);

-- Preserve today's tier position across an in-place upgrade. Historical rows
-- are also retained so the table remains auditable and can drive analytics.
INSERT INTO bank_exchange_daily_usage (
    user_id, usage_date, permanent_exchanged, created_at, updated_at
)
SELECT
    user_id,
    (created_at AT TIME ZONE 'Asia/Shanghai')::date,
    SUM(GREATEST(-permanent_delta, 0)),
    MIN(created_at),
    MAX(created_at)
FROM bank_ledger
WHERE operation = 'exchange'
GROUP BY user_id, (created_at AT TIME ZONE 'Asia/Shanghai')::date
ON CONFLICT (user_id, usage_date) DO NOTHING;

-- Old installations keep their effective flat rate as a single unbounded
-- tier. New clients can replace this JSON array through the bank policy API.
INSERT INTO settings (key, value, updated_at)
SELECT
    'bank_exchange_tiers',
    jsonb_build_array(jsonb_build_object(
        'up_to', NULL,
        'rate', COALESCE((SELECT value FROM settings WHERE key = 'bank_exchange_rate' LIMIT 1), '1.00000000')
    ))::text,
    NOW()
ON CONFLICT (key) DO NOTHING;

-- Bank mutations insert their ledger row after applying the balance change.
-- Capture the resulting balances and derive the pre-operation snapshot from
-- the immutable deltas in the same transaction.
CREATE OR REPLACE FUNCTION capture_bank_ledger_balance_snapshot()
RETURNS TRIGGER AS $$
DECLARE
    current_permanent NUMERIC(20,8);
    current_temporary NUMERIC(20,8);
BEGIN
    SELECT
        balance,
        COALESCE((
            SELECT SUM(remaining_amount)
            FROM temporary_credit_grants
            WHERE user_id = NEW.user_id
              AND remaining_amount > 0
              AND available_at <= clock_timestamp()
              AND expires_at > clock_timestamp()
        ), 0)
    INTO current_permanent, current_temporary
    FROM users
    WHERE id = NEW.user_id;

    NEW.permanent_balance_after := COALESCE(NEW.permanent_balance_after, current_permanent);
    NEW.temporary_balance_after := COALESCE(NEW.temporary_balance_after, current_temporary);
    NEW.permanent_balance_before := COALESCE(
        NEW.permanent_balance_before,
        NEW.permanent_balance_after - NEW.permanent_delta
    );
    NEW.temporary_balance_before := COALESCE(
        NEW.temporary_balance_before,
        NEW.temporary_balance_after - NEW.temporary_delta
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS bank_ledger_balance_snapshot_trigger ON bank_ledger;
CREATE TRIGGER bank_ledger_balance_snapshot_trigger
BEFORE INSERT ON bank_ledger
FOR EACH ROW EXECUTE FUNCTION capture_bank_ledger_balance_snapshot();
