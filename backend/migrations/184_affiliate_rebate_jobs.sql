-- Reliable affiliate rebate delivery for redeem-code and admin balance credits.
-- This migration is intentionally additive: historical ledger rows and balance
-- adjustments are left untouched because their source cannot be reconstructed
-- without risking duplicate rebates.

ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS source_redeem_code_id BIGINT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'user_affiliate_ledger_source_redeem_code_fk'
          AND conrelid = 'user_affiliate_ledger'::regclass
    ) THEN
        ALTER TABLE user_affiliate_ledger
            ADD CONSTRAINT user_affiliate_ledger_source_redeem_code_fk
            FOREIGN KEY (source_redeem_code_id)
            REFERENCES redeem_codes(id)
            ON DELETE RESTRICT;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_affiliate_ledger_source_redeem_accrue_uniq
    ON user_affiliate_ledger(source_redeem_code_id)
    WHERE action = 'accrue' AND source_redeem_code_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS affiliate_rebate_jobs (
    id BIGSERIAL PRIMARY KEY,
    invitee_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    source_redeem_code_id BIGINT NOT NULL REFERENCES redeem_codes(id) ON DELETE RESTRICT,
    source_kind VARCHAR(32) NOT NULL,
    base_amount NUMERIC(20,8) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT NULL,
    last_error_at TIMESTAMPTZ NULL,
    processing_started_at TIMESTAMPTZ NULL,
    succeeded_at TIMESTAMPTZ NULL,
    skipped_at TIMESTAMPTZ NULL,
    failed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT affiliate_rebate_jobs_source_redeem_uniq UNIQUE (source_redeem_code_id),
    CONSTRAINT affiliate_rebate_jobs_source_kind_check
        CHECK (source_kind IN ('redeem', 'admin_recharge')),
    CONSTRAINT affiliate_rebate_jobs_status_check
        CHECK (status IN ('pending', 'processing', 'succeeded', 'skipped', 'failed')),
    CONSTRAINT affiliate_rebate_jobs_amount_check CHECK (base_amount > 0),
    CONSTRAINT affiliate_rebate_jobs_attempts_check CHECK (attempts >= 0)
);

CREATE INDEX IF NOT EXISTS idx_affiliate_rebate_jobs_due
    ON affiliate_rebate_jobs(next_retry_at, id)
    WHERE status IN ('pending', 'failed');

CREATE INDEX IF NOT EXISTS idx_affiliate_rebate_jobs_processing_lease
    ON affiliate_rebate_jobs(processing_started_at, id)
    WHERE status = 'processing';

CREATE INDEX IF NOT EXISTS idx_affiliate_rebate_jobs_invitee
    ON affiliate_rebate_jobs(invitee_user_id, created_at DESC);

COMMENT ON TABLE affiliate_rebate_jobs IS 'Reliable affiliate rebate outbox for balance credits';
COMMENT ON COLUMN affiliate_rebate_jobs.source_redeem_code_id IS 'Unique redeem-code audit source for idempotent rebate delivery';
COMMENT ON COLUMN affiliate_rebate_jobs.source_kind IS 'redeem or admin_recharge';
COMMENT ON COLUMN user_affiliate_ledger.source_redeem_code_id IS 'Redeem-code source for second-layer affiliate rebate deduplication';
