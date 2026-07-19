-- Bank temporary-credit advances, account-level debt, and immutable audit rows.
-- All amounts use the same eight-decimal ledger contract as migration 175.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS temporary_credit_debt NUMERIC(20,8) NOT NULL DEFAULT 0;

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS temporary_credit_debt_due_at TIMESTAMPTZ NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'users_temporary_credit_debt_nonnegative'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_temporary_credit_debt_nonnegative
            CHECK (temporary_credit_debt >= 0);
    END IF;
END $$;

-- Keep the existing temporary-credit ledger but distinguish user-bank grants
-- from administrator grants.  The source check is recreated idempotently so
-- older installations can be upgraded without rewriting migration 175.
ALTER TABLE temporary_credit_grants
    DROP CONSTRAINT IF EXISTS temporary_credit_grants_source_check;

ALTER TABLE temporary_credit_grants
    ADD CONSTRAINT temporary_credit_grants_source_check CHECK (
        (source = 'checkin' AND checkin_id IS NOT NULL AND granted_by IS NULL AND notes = '')
        OR
        (source = 'admin_grant' AND checkin_id IS NULL AND granted_by IS NOT NULL)
        OR
        (source IN ('bank_advance', 'bank_exchange') AND checkin_id IS NULL AND granted_by IS NULL AND notes = '')
    );

CREATE TABLE IF NOT EXISTS bank_loans (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    grant_id BIGINT NOT NULL UNIQUE REFERENCES temporary_credit_grants(id) ON DELETE RESTRICT,
    principal NUMERIC(20,8) NOT NULL CHECK (principal > 0),
    debt_remaining NUMERIC(20,8) NOT NULL CHECK (debt_remaining >= 0 AND debt_remaining <= principal),
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    grant_expires_at TIMESTAMPTZ NOT NULL,
    settlement_due_at TIMESTAMPTZ NOT NULL,
    settled_at TIMESTAMPTZ NULL,
    settlement_permanent_amount NUMERIC(20,8) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT bank_loans_status_check CHECK (status IN ('active', 'repaid', 'settled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS bank_loans_one_active_per_user
    ON bank_loans (user_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS bank_loans_due_idx
    ON bank_loans (settlement_due_at, id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS bank_ledger (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    operation VARCHAR(32) NOT NULL,
    loan_id BIGINT NULL REFERENCES bank_loans(id) ON DELETE RESTRICT,
    grant_id BIGINT NULL REFERENCES temporary_credit_grants(id) ON DELETE RESTRICT,
    actor_id BIGINT NULL REFERENCES users(id) ON DELETE RESTRICT,
    permanent_delta NUMERIC(20,8) NOT NULL DEFAULT 0,
    temporary_delta NUMERIC(20,8) NOT NULL DEFAULT 0,
    debt_delta NUMERIC(20,8) NOT NULL DEFAULT 0,
    debt_before NUMERIC(20,8) NOT NULL DEFAULT 0,
    debt_after NUMERIC(20,8) NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS bank_ledger_user_created_idx
    ON bank_ledger (user_id, created_at DESC, id DESC);

INSERT INTO settings (key, value, updated_at)
VALUES
    ('bank_advance_min_amount', '5.00000000', NOW()),
    ('bank_advance_max_amount', '20.00000000', NOW()),
    ('bank_debt_grace_days', '3', NOW()),
    ('bank_debt_conversion_ratio', '1.00000000', NOW()),
    ('bank_exchange_rate', '1.00000000', NOW())
ON CONFLICT (key) DO NOTHING;
