-- Internal-credit mall settlement and scheduled daily temporary-credit plans.
-- Existing external payment orders remain unchanged and keep using their
-- immutable snapshots.

ALTER TABLE currency_products
    ALTER COLUMN payment_price TYPE NUMERIC(20,8),
    ADD COLUMN IF NOT EXISTS payment_credit_type VARCHAR(20) NOT NULL DEFAULT 'permanent',
    ADD COLUMN IF NOT EXISTS credited_type VARCHAR(20) NOT NULL DEFAULT 'permanent',
    ADD COLUMN IF NOT EXISTS credited_amount NUMERIC(20,8);

UPDATE currency_products
SET credited_amount = credited_permanent_amount
WHERE credited_amount IS NULL;

ALTER TABLE currency_products
    ALTER COLUMN credited_amount SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'currency_products_internal_credit_types_check') THEN
        ALTER TABLE currency_products ADD CONSTRAINT currency_products_internal_credit_types_check
            CHECK (
                payment_credit_type IN ('permanent', 'temporary')
                AND credited_type IN ('permanent', 'temporary')
                AND credited_amount > 0
            );
    END IF;
END $$;

ALTER TABLE subscription_plans
    ALTER COLUMN price TYPE NUMERIC(20,8),
    ALTER COLUMN original_price TYPE NUMERIC(20,8),
    ADD COLUMN IF NOT EXISTS benefit_type VARCHAR(40) NOT NULL DEFAULT 'sub2',
    ADD COLUMN IF NOT EXISTS payment_credit_type VARCHAR(20) NOT NULL DEFAULT 'permanent',
    ADD COLUMN IF NOT EXISTS daily_temporary_credit_amount NUMERIC(20,8) NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'subscription_plans_internal_benefit_check') THEN
        ALTER TABLE subscription_plans ADD CONSTRAINT subscription_plans_internal_benefit_check
            CHECK (
                payment_credit_type IN ('permanent', 'temporary')
                AND (
                    (benefit_type = 'sub2' AND group_id > 0 AND daily_temporary_credit_amount = 0)
                    OR
                    (benefit_type = 'daily_temporary_credit' AND group_id = 0
                        AND validity_unit = 'day' AND validity_days > 0
                        AND daily_temporary_credit_amount > 0)
                )
            );
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS mall_purchases (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    product_type VARCHAR(20) NOT NULL,
    product_id BIGINT NOT NULL,
    idempotency_record_id BIGINT NOT NULL UNIQUE,
    payment_credit_type VARCHAR(20) NOT NULL,
    price NUMERIC(20,8) NOT NULL,
    credited_type VARCHAR(20),
    credited_amount NUMERIC(20,8),
    benefit_type VARCHAR(40),
    benefit_days INT,
    daily_temporary_credit_amount NUMERIC(20,8),
    subscription_expires_at TIMESTAMPTZ,
    status VARCHAR(20) NOT NULL DEFAULT 'completed',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT mall_purchases_shape_check CHECK (
        payment_credit_type IN ('permanent', 'temporary')
        AND price > 0
        AND status = 'completed'
        AND (
            (product_type = 'currency' AND credited_type IN ('permanent', 'temporary')
                AND credited_amount > 0 AND benefit_type IS NULL AND benefit_days IS NULL
                AND daily_temporary_credit_amount IS NULL)
            OR
            (product_type = 'subscription' AND credited_type IS NULL AND credited_amount IS NULL
                AND benefit_days > 0
                AND (
                    (benefit_type = 'sub2' AND daily_temporary_credit_amount IS NULL)
                    OR
                    (benefit_type = 'daily_temporary_credit' AND daily_temporary_credit_amount > 0)
                ))
        )
    )
);

CREATE INDEX IF NOT EXISTS mall_purchases_user_created_idx
    ON mall_purchases (user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS mall_purchases_product_idx
    ON mall_purchases (product_type, product_id, created_at DESC);

CREATE TABLE IF NOT EXISTS mall_daily_credit_subscriptions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    plan_id BIGINT NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    last_grant_date DATE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT mall_daily_credit_subscriptions_status_check CHECK (status IN ('active', 'expired')),
    CONSTRAINT mall_daily_credit_subscriptions_window_check CHECK (expires_at > starts_at),
    CONSTRAINT mall_daily_credit_subscriptions_user_plan_key UNIQUE (user_id, plan_id)
);

CREATE INDEX IF NOT EXISTS mall_daily_credit_subscriptions_user_expiry_idx
    ON mall_daily_credit_subscriptions (user_id, expires_at DESC);

ALTER TABLE temporary_credit_grants
    ADD COLUMN IF NOT EXISTS mall_purchase_id BIGINT REFERENCES mall_purchases(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS daily_subscription_id BIGINT REFERENCES mall_daily_credit_subscriptions(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS scheduled_date DATE;

ALTER TABLE temporary_credit_grants
    DROP CONSTRAINT IF EXISTS temporary_credit_grants_source_check;

ALTER TABLE temporary_credit_grants
    ADD CONSTRAINT temporary_credit_grants_source_check CHECK (
        (source = 'checkin' AND checkin_id IS NOT NULL AND granted_by IS NULL AND notes = ''
            AND mall_purchase_id IS NULL AND daily_subscription_id IS NULL AND scheduled_date IS NULL)
        OR
        (source = 'admin_grant' AND checkin_id IS NULL AND granted_by IS NOT NULL
            AND mall_purchase_id IS NULL AND daily_subscription_id IS NULL AND scheduled_date IS NULL)
        OR
        (source IN ('bank_advance', 'bank_exchange') AND checkin_id IS NULL AND granted_by IS NULL AND notes = ''
            AND mall_purchase_id IS NULL AND daily_subscription_id IS NULL AND scheduled_date IS NULL)
        OR
        (source = 'mall_product' AND checkin_id IS NULL AND granted_by IS NULL AND notes = ''
            AND mall_purchase_id IS NOT NULL AND daily_subscription_id IS NULL AND scheduled_date IS NULL)
        OR
        (source = 'subscription' AND checkin_id IS NULL AND granted_by IS NULL AND notes = ''
            AND mall_purchase_id IS NOT NULL AND daily_subscription_id IS NOT NULL AND scheduled_date IS NOT NULL)
    );

CREATE UNIQUE INDEX IF NOT EXISTS temporary_credit_grants_daily_subscription_date_key
    ON temporary_credit_grants (daily_subscription_id, scheduled_date)
    WHERE daily_subscription_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS temporary_credit_grants_mall_purchase_idx
    ON temporary_credit_grants (mall_purchase_id)
    WHERE mall_purchase_id IS NOT NULL;
