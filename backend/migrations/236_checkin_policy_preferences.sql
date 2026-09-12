-- Additive check-in policy, period, preference, and immutable draw snapshots.
-- Migrate from calendar-date uniqueness to an immutable real-time period key.
-- Deploy only with the V2 check-in writer; do not mix legacy writer binaries.
-- A configured transition may contain two distinct periods on one local date.

ALTER TABLE daily_checkins
    ADD COLUMN IF NOT EXISTS business_period_key VARCHAR(96),
    ADD COLUMN IF NOT EXISTS period_start_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS period_end_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS mode VARCHAR(20) NOT NULL DEFAULT 'legacy',
    ADD COLUMN IF NOT EXISTS base_reward_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS multiplier_bps INT NOT NULL DEFAULT 10000,
    ADD COLUMN IF NOT EXISTS super_cost NUMERIC(20,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS auto_fee_bps INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS policy_version VARCHAR(96) NOT NULL DEFAULT 'legacy',
    ADD COLUMN IF NOT EXISTS random_rule_version VARCHAR(96) NOT NULL DEFAULT '';

UPDATE daily_checkins
SET business_period_key = COALESCE(business_period_key, 'checkin:' || EXTRACT(EPOCH FROM (checkin_date::timestamp AT TIME ZONE 'Asia/Shanghai'))::bigint::text),
    period_start_at = COALESCE(period_start_at, checkin_date::timestamp AT TIME ZONE 'Asia/Shanghai'),
    period_end_at = COALESCE(period_end_at, (checkin_date + 1)::timestamp AT TIME ZONE 'Asia/Shanghai'),
    base_reward_amount = CASE WHEN base_reward_amount = 0 THEN reward_amount ELSE base_reward_amount END
WHERE business_period_key IS NULL
   OR period_start_at IS NULL
   OR period_end_at IS NULL
   OR base_reward_amount = 0;

ALTER TABLE daily_checkins
    ALTER COLUMN business_period_key SET NOT NULL,
    ALTER COLUMN period_start_at SET NOT NULL,
    ALTER COLUMN period_end_at SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS daily_checkins_user_period_unique
    ON daily_checkins (user_id, business_period_key);

ALTER TABLE daily_checkins DROP CONSTRAINT IF EXISTS daily_checkins_user_date_unique;
ALTER TABLE daily_checkins DROP CONSTRAINT IF EXISTS daily_checkins_reward_amount_check;
ALTER TABLE daily_checkins ADD CONSTRAINT daily_checkins_reward_amount_check CHECK (reward_amount >= 0);


DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'daily_checkins_mode_check'
          AND conrelid = 'daily_checkins'::regclass
    ) THEN
        ALTER TABLE daily_checkins
            ADD CONSTRAINT daily_checkins_mode_check
            CHECK (mode IN ('legacy', 'direct', 'direct-auto', 'normal', 'super'));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'daily_checkins_multiplier_bps_check'
          AND conrelid = 'daily_checkins'::regclass
    ) THEN
        ALTER TABLE daily_checkins
            ADD CONSTRAINT daily_checkins_multiplier_bps_check
            CHECK (multiplier_bps > 0 AND multiplier_bps <= 1000000);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'daily_checkins_super_cost_nonnegative'
          AND conrelid = 'daily_checkins'::regclass
    ) THEN
        ALTER TABLE daily_checkins
            ADD CONSTRAINT daily_checkins_super_cost_nonnegative
            CHECK (super_cost >= 0);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'daily_checkins_auto_fee_bps_check'
          AND conrelid = 'daily_checkins'::regclass
    ) THEN
        ALTER TABLE daily_checkins
            ADD CONSTRAINT daily_checkins_auto_fee_bps_check
            CHECK (auto_fee_bps >= 0 AND auto_fee_bps <= 10000);
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS daily_checkin_preferences (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
    auto_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    consent_policy_version VARCHAR(96) NOT NULL DEFAULT '',
    consent_fee_bps INT NOT NULL DEFAULT 0 CHECK (consent_fee_bps >= 0 AND consent_fee_bps <= 10000),
    consented_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO settings (key, value, updated_at)
VALUES (
    'daily_checkin_policy_v2',
    '{"version":"checkin-v2-default","refresh_time":"00:00","auto_fee_bps":500,"normal":{"enabled":false,"min_bps":10000,"max_bps":10000},"super":{"enabled":false,"min_bps":10000,"max_bps":10000,"cost":"0.00000000"}}',
    NOW()
)
ON CONFLICT (key) DO NOTHING;

-- Super check-in writes a permanent-cost ledger event. Migration 235 installed
-- a NOT VALID allowlist that predates this operation; replace that allowlist
-- without touching historical rows or any bank service code.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'bank_ledger_operation_check'
          AND conrelid = 'bank_ledger'::regclass
    ) THEN
        ALTER TABLE bank_ledger DROP CONSTRAINT bank_ledger_operation_check;
    END IF;
    ALTER TABLE bank_ledger
        ADD CONSTRAINT bank_ledger_operation_check
        CHECK (operation IN (
            'advance',
            'exchange',
            'debt_offset',
            'permanent_settlement',
            'unused_advance_repayment',
            'early_repay_temporary',
            'early_repay_permanent',
            'exchange_expiry_refund',
            'checkin_super_cost'
        )) NOT VALID;
END
$$;
