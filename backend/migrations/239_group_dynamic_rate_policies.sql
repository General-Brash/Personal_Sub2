-- W09: versioned per-group dynamic rate policies and per-user/window counters.
-- This migration is additive. A missing policy row means dynamic pricing is disabled.

CREATE TABLE IF NOT EXISTS group_dynamic_rate_policies (
    group_id BIGINT PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    metric TEXT NOT NULL DEFAULT 'tokens_m' CHECK (metric IN ('tokens_m', 'wallet_spend')),
    timezone TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    reset_time TEXT NOT NULL DEFAULT '00:00',
    tiers JSONB NOT NULL DEFAULT '[{"id":"tier-0","threshold":"0","factor":1}]'::jsonb
        CHECK (jsonb_typeof(tiers) = 'array' AND jsonb_array_length(tiers) > 0),
    included_modes JSONB NOT NULL DEFAULT '["text"]'::jsonb
        CHECK (jsonb_typeof(included_modes) = 'array' AND jsonb_array_length(included_modes) > 0),
    cache_token_policy TEXT NOT NULL DEFAULT 'normalized' CHECK (cache_token_policy IN ('normalized','provider')),
    policy_version BIGINT NOT NULL DEFAULT 1 CHECK (policy_version > 0),
    effective_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS group_dynamic_rate_policy_versions (
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    policy_version BIGINT NOT NULL CHECK (policy_version > 0),
    snapshot JSONB NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, policy_version)
);

CREATE TABLE IF NOT EXISTS user_group_usage_periods (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    window_id TEXT NOT NULL,
    normalized_tokens BIGINT NOT NULL DEFAULT 0 CHECK (normalized_tokens >= 0),
    wallet_spent NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (wallet_spent >= 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, group_id, window_id)
);

CREATE INDEX IF NOT EXISTS idx_user_group_usage_periods_group_window
    ON user_group_usage_periods (group_id, window_id);

-- Keep the billing event ledger authoritative for retries: one dedup row owns
-- both the wallet effect and the dynamic-rate counter delta.
ALTER TABLE IF EXISTS usage_billing_dedup
    ADD COLUMN IF NOT EXISTS dynamic_rate_snapshot JSONB,
    ADD COLUMN IF NOT EXISTS dynamic_rate_window_id TEXT,
    ADD COLUMN IF NOT EXISTS dynamic_rate_tokens_delta BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS dynamic_rate_wallet_delta NUMERIC(20,8) NOT NULL DEFAULT 0;

ALTER TABLE IF EXISTS usage_billing_dedup
    DROP CONSTRAINT IF EXISTS usage_billing_dedup_dynamic_rate_delta_nonnegative;
ALTER TABLE IF EXISTS usage_billing_dedup
    ADD CONSTRAINT usage_billing_dedup_dynamic_rate_delta_nonnegative
    CHECK (dynamic_rate_tokens_delta >= 0 AND dynamic_rate_wallet_delta >= 0);

COMMENT ON TABLE group_dynamic_rate_policies IS 'W09 current per-group dynamic rate policy; enabled defaults to false';
COMMENT ON TABLE group_dynamic_rate_policy_versions IS 'W09 immutable dynamic rate policy versions for snapshot replay';
COMMENT ON TABLE user_group_usage_periods IS 'W09 per user x group x natural window counters; no daily reset SQL required';

-- One immutable admission snapshot per asynchronous batch. Captures, never
-- holds/releases, advance the original window's wallet counter.
ALTER TABLE batch_image_jobs ADD COLUMN IF NOT EXISTS dynamic_rate_snapshot JSONB;

-- Retention moves, not discards, immutable dynamic price and settlement evidence.
ALTER TABLE usage_billing_dedup_archive
 ADD COLUMN IF NOT EXISTS dynamic_rate_snapshot JSONB,
 ADD COLUMN IF NOT EXISTS dynamic_rate_window_id TEXT,
 ADD COLUMN IF NOT EXISTS dynamic_rate_tokens_delta BIGINT NOT NULL DEFAULT 0,
 ADD COLUMN IF NOT EXISTS dynamic_rate_wallet_delta NUMERIC(20,8) NOT NULL DEFAULT 0;
