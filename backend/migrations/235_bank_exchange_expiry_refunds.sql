-- Bank exchange expiry-refund snapshots and one-shot settlements.
-- New grants freeze principal/generated/fee/policy/expiry; historical grants
-- are intentionally not backfilled and cannot be inferred from current rates.

CREATE TABLE IF NOT EXISTS bank_exchange_grant_snapshots (
    grant_id BIGINT PRIMARY KEY REFERENCES temporary_credit_grants(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    principal_permanent NUMERIC(20,8) NOT NULL CHECK (principal_permanent > 0),
    generated_temporary NUMERIC(20,8) NOT NULL CHECK (generated_temporary > 0),
    fee_bps INTEGER NOT NULL CHECK (fee_bps >= 0 AND fee_bps <= 10000),
    policy_version BIGINT NOT NULL CHECK (policy_version > 0),
    eligibility VARCHAR(16) NOT NULL DEFAULT 'eligible',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT bank_exchange_grant_snapshots_eligibility_check
        CHECK (eligibility IN ('eligible', 'ineligible', 'manual_review')),
    CONSTRAINT bank_exchange_grant_snapshots_user_grant_unique UNIQUE (user_id, grant_id)
);

CREATE INDEX IF NOT EXISTS bank_exchange_grant_snapshots_due_idx
    ON bank_exchange_grant_snapshots (expires_at, grant_id)
    WHERE eligibility = 'eligible';

CREATE TABLE IF NOT EXISTS bank_exchange_expiry_settlements (
    id BIGSERIAL PRIMARY KEY,
    event_id VARCHAR(96) NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    grant_id BIGINT NOT NULL UNIQUE REFERENCES temporary_credit_grants(id) ON DELETE RESTRICT,
    expired_remaining NUMERIC(20,8) NOT NULL CHECK (expired_remaining >= 0),
    refundable_principal NUMERIC(20,8) NOT NULL CHECK (refundable_principal >= 0),
    fee_amount NUMERIC(20,8) NOT NULL CHECK (fee_amount >= 0),
    net_refund NUMERIC(20,8) NOT NULL CHECK (net_refund >= 0),
    status VARCHAR(16) NOT NULL DEFAULT 'settled',
    reason VARCHAR(64) NOT NULL DEFAULT '',
    policy_version BIGINT NOT NULL CHECK (policy_version > 0),
    settled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT bank_exchange_expiry_settlements_status_check
        CHECK (status IN ('pending', 'manual_review', 'settled', 'failed')),
    CONSTRAINT bank_exchange_expiry_settlements_amount_check
        CHECK (refundable_principal = net_refund + fee_amount),
    CONSTRAINT bank_exchange_expiry_settlements_snapshot_fk
        FOREIGN KEY (user_id, grant_id)
        REFERENCES bank_exchange_grant_snapshots (user_id, grant_id)
        ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS bank_exchange_expiry_settlements_user_idx
    ON bank_exchange_expiry_settlements (user_id, settled_at DESC, id DESC);

-- Older deployments may carry a narrower operation check that predates the
-- expiry-refund event. Replace only check constraints that mention operation,
-- then install the widened allowlist. NOT VALID keeps historical rows intact
-- while enforcing the allowlist for every new bank_ledger row.
DO $$
DECLARE
    operation_constraint_name TEXT;
BEGIN
    FOR operation_constraint_name IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'bank_ledger'::regclass
          AND contype = 'c'
          AND pg_get_constraintdef(oid) LIKE '%operation%'
    LOOP
        EXECUTE format('ALTER TABLE bank_ledger DROP CONSTRAINT %I', operation_constraint_name);
    END LOOP;
END $$;

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
        'exchange_expiry_refund'
    )) NOT VALID;

COMMENT ON TABLE bank_exchange_grant_snapshots IS
    'Immutable P/T/fee/policy/exchange-expiry snapshot for bank_exchange grants created after W05 is enabled';
COMMENT ON TABLE bank_exchange_expiry_settlements IS
    'One terminal settlement row per bank_exchange grant; unique grant_id prevents duplicate refunds';

INSERT INTO settings (key, value, updated_at)
VALUES
    ('bank_exchange_expiry_refund_enabled', 'false', NOW()),
    ('bank_exchange_expiry_refund_fee_bps', '1000', NOW()),
    ('bank_exchange_expiry_refund_policy_version', '1', NOW()),
    ('bank_exchange_expiry_local_time', '00:00', NOW())
ON CONFLICT (key) DO NOTHING;
