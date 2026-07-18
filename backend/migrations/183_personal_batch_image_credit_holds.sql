-- Batch-image holds need a dedicated ledger before temporary credits can be
-- reserved safely. Existing jobs are intentionally not backfilled: the legacy
-- users.frozen_balance aggregate cannot be attributed to individual grants
-- without inventing audit data.

CREATE TABLE IF NOT EXISTS batch_image_credit_holds (
    id BIGSERIAL PRIMARY KEY,
    batch_id VARCHAR(64) NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'reserved',
    hold_amount NUMERIC(20,8) NOT NULL,
    temporary_reserved_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    permanent_reserved_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    captured_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    temporary_captured_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    permanent_captured_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    expired_unrestored_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    reserve_fingerprint VARCHAR(128) NOT NULL,
    terminal_fingerprint VARCHAR(128),
    reserved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT batch_image_credit_holds_batch_id_unique UNIQUE (batch_id),
    CONSTRAINT batch_image_credit_holds_id_batch_id_unique UNIQUE (id, batch_id),
    CONSTRAINT batch_image_credit_holds_batch_fk
        FOREIGN KEY (batch_id) REFERENCES batch_image_jobs(batch_id) ON DELETE RESTRICT,
    CONSTRAINT batch_image_credit_holds_status_check
        CHECK (status IN ('reserved', 'captured', 'released')),
    CONSTRAINT batch_image_credit_holds_amounts_nonnegative_check CHECK (
        hold_amount >= 0
        AND temporary_reserved_amount >= 0
        AND permanent_reserved_amount >= 0
        AND captured_amount >= 0
        AND temporary_captured_amount >= 0
        AND permanent_captured_amount >= 0
        AND expired_unrestored_amount >= 0
    ),
    CONSTRAINT batch_image_credit_holds_reserved_conservation_check
        CHECK (temporary_reserved_amount + permanent_reserved_amount = hold_amount),
    CONSTRAINT batch_image_credit_holds_captured_conservation_check CHECK (
        temporary_captured_amount + permanent_captured_amount = captured_amount
        AND temporary_captured_amount <= temporary_reserved_amount
        AND permanent_captured_amount <= permanent_reserved_amount
        AND captured_amount <= hold_amount
    ),
    CONSTRAINT batch_image_credit_holds_expired_bound_check
        CHECK (expired_unrestored_amount <= temporary_reserved_amount - temporary_captured_amount),
    CONSTRAINT batch_image_credit_holds_fingerprint_check CHECK (
        btrim(reserve_fingerprint) <> ''
        AND (terminal_fingerprint IS NULL OR btrim(terminal_fingerprint) <> '')
    ),
    CONSTRAINT batch_image_credit_holds_terminal_state_check CHECK (
        (
            status = 'reserved'
            AND captured_amount = 0
            AND temporary_captured_amount = 0
            AND permanent_captured_amount = 0
            AND expired_unrestored_amount = 0
            AND terminal_fingerprint IS NULL
            AND settled_at IS NULL
        )
        OR
        (
            status = 'captured'
            AND terminal_fingerprint IS NOT NULL
            AND settled_at IS NOT NULL
            AND settled_at >= reserved_at
        )
        OR
        (
            status = 'released'
            AND captured_amount = 0
            AND temporary_captured_amount = 0
            AND permanent_captured_amount = 0
            AND terminal_fingerprint IS NOT NULL
            AND settled_at IS NOT NULL
            AND settled_at >= reserved_at
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_batch_image_credit_holds_user_status_reserved_at
    ON batch_image_credit_holds (user_id, status, reserved_at);

CREATE INDEX IF NOT EXISTS idx_batch_image_credit_holds_api_key_reserved_at
    ON batch_image_credit_holds (api_key_id, reserved_at);

CREATE INDEX IF NOT EXISTS idx_batch_image_credit_holds_group_reserved_at
    ON batch_image_credit_holds (group_id, reserved_at)
    WHERE group_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_batch_image_credit_holds_status_updated_at
    ON batch_image_credit_holds (status, updated_at);

CREATE TABLE IF NOT EXISTS batch_image_credit_hold_allocations (
    id BIGSERIAL PRIMARY KEY,
    hold_id BIGINT NOT NULL,
    batch_id VARCHAR(64) NOT NULL,
    grant_id BIGINT NOT NULL REFERENCES temporary_credit_grants(id) ON DELETE RESTRICT,
    grant_expires_at TIMESTAMPTZ NOT NULL,
    reserved_amount NUMERIC(20,8) NOT NULL,
    captured_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    refunded_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    expired_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT batch_image_credit_hold_allocations_hold_batch_fk
        FOREIGN KEY (hold_id, batch_id)
        REFERENCES batch_image_credit_holds(id, batch_id)
        ON DELETE RESTRICT,
    CONSTRAINT batch_image_credit_hold_allocations_hold_grant_unique
        UNIQUE (hold_id, grant_id),
    CONSTRAINT batch_image_credit_hold_allocations_amounts_check CHECK (
        reserved_amount > 0
        AND captured_amount >= 0
        AND refunded_amount >= 0
        AND expired_amount >= 0
        AND captured_amount + refunded_amount + expired_amount <= reserved_amount
    ),
    CONSTRAINT batch_image_credit_hold_allocations_settlement_check CHECK (
        (
            captured_amount = 0
            AND refunded_amount = 0
            AND expired_amount = 0
        )
        OR captured_amount + refunded_amount + expired_amount = reserved_amount
    ),
    CONSTRAINT batch_image_credit_hold_allocations_expiry_snapshot_check
        CHECK (grant_expires_at > created_at),
    CONSTRAINT batch_image_credit_hold_allocations_timestamps_check
        CHECK (updated_at >= created_at)
);

CREATE INDEX IF NOT EXISTS idx_batch_image_credit_hold_allocations_batch_id
    ON batch_image_credit_hold_allocations (batch_id);

CREATE INDEX IF NOT EXISTS idx_batch_image_credit_hold_allocations_grant_id_created_at
    ON batch_image_credit_hold_allocations (grant_id, created_at);

COMMENT ON TABLE batch_image_credit_holds IS
    'Auditable per-batch split between temporary-credit and permanent-balance holds';

COMMENT ON TABLE batch_image_credit_hold_allocations IS
    'Per-grant FEFO allocations for batch-image temporary-credit holds';
