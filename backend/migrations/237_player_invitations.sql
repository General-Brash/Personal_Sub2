-- W08 player invitation quota, one-time registration credentials, audited
-- relationships, and rebate event relation snapshots.
-- This migration is additive. It does not initialize historical users and does
-- not replay historical rebate jobs; either action requires a separate,
-- explicitly authorized maintenance operation.

ALTER TABLE user_affiliates
    ADD COLUMN IF NOT EXISTS inviter_effective_at TIMESTAMPTZ NULL;

COMMENT ON COLUMN user_affiliates.inviter_effective_at IS
    'Effective time of inviter_id; rebate workers must not use a relation created after the source event';

-- Conservative legacy backfill only. updated_at is used because the historical
-- binding time was not recorded; this may skip an old pending job rather than
-- incorrectly paying a newly backfilled inviter.
UPDATE user_affiliates
SET inviter_effective_at = COALESCE(updated_at, created_at)
WHERE inviter_id IS NOT NULL
  AND inviter_effective_at IS NULL;

CREATE TABLE IF NOT EXISTS player_invitation_quota_events (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_type VARCHAR(32) NOT NULL,
    delta INTEGER NOT NULL,
    idempotency_key VARCHAR(160) NOT NULL,
    reason TEXT NULL,
    actor_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    request_id VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT player_invitation_quota_events_delta_check CHECK (delta <> 0),
    CONSTRAINT player_invitation_quota_events_type_check
        CHECK (event_type IN ('initial_grant', 'admin_grant', 'admin_revoke')),
    CONSTRAINT player_invitation_quota_events_idempotency_uniq
        UNIQUE (user_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_player_invitation_quota_events_user
    ON player_invitation_quota_events(user_id, created_at, id);

CREATE TABLE IF NOT EXISTS player_invitation_reservations (
    id BIGSERIAL PRIMARY KEY,
    inviter_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    quota_event_id BIGINT NOT NULL REFERENCES player_invitation_quota_events(id) ON DELETE RESTRICT,
    token_hash CHAR(64) NOT NULL UNIQUE,
    status VARCHAR(16) NOT NULL DEFAULT 'reserved',
    expires_at TIMESTAMPTZ NOT NULL,
    claimed_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    claimed_at TIMESTAMPTZ NULL,
    cancelled_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT player_invitation_reservations_status_check
        CHECK (status IN ('reserved', 'claimed', 'cancelled', 'expired')),
    CONSTRAINT player_invitation_reservations_claim_check
        CHECK ((status = 'claimed') = (claimed_user_id IS NOT NULL AND claimed_at IS NOT NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_player_invitation_reservations_claimed_user_uniq
    ON player_invitation_reservations(claimed_user_id)
    WHERE status = 'claimed' AND claimed_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_player_invitation_reservations_inviter_status
    ON player_invitation_reservations(inviter_user_id, status, expires_at, id);

CREATE INDEX IF NOT EXISTS idx_player_invitation_reservations_expiry
    ON player_invitation_reservations(expires_at, id)
    WHERE status = 'reserved';

CREATE TABLE IF NOT EXISTS player_invitation_relations (
    invitee_user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    inviter_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    source VARCHAR(32) NOT NULL,
    reservation_id BIGINT NULL REFERENCES player_invitation_reservations(id) ON DELETE RESTRICT,
    effective_at TIMESTAMPTZ NOT NULL,
    created_by_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    reason TEXT NULL,
    request_id VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT player_invitation_relations_source_check
        CHECK (source IN ('player_invitation', 'affiliate_code', 'admin_backfill')),
    CONSTRAINT player_invitation_relations_no_self_check CHECK (invitee_user_id <> inviter_user_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_player_invitation_relations_reservation_uniq
    ON player_invitation_relations(reservation_id)
    WHERE reservation_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_player_invitation_relations_inviter_effective
    ON player_invitation_relations(inviter_user_id, effective_at, invitee_user_id);

CREATE TABLE IF NOT EXISTS player_invitation_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    actor_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    target_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    action VARCHAR(48) NOT NULL,
    before_state JSONB NULL,
    after_state JSONB NULL,
    reason TEXT NOT NULL,
    request_id VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT player_invitation_audit_reason_check CHECK (length(btrim(reason)) > 0)
);

CREATE INDEX IF NOT EXISTS idx_player_invitation_audit_target
    ON player_invitation_audit_logs(target_user_id, created_at DESC, id DESC);

-- Snapshot the relation at outbox enqueue time. Existing pending jobs are
-- backfilled conservatively: event_occurred_at comes from created_at, and an
-- inviter snapshot is accepted only when the relation effective time is not
-- later than that event.
ALTER TABLE affiliate_rebate_jobs
    ADD COLUMN IF NOT EXISTS inviter_user_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS relation_effective_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS event_occurred_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS policy_snapshot JSONB NULL;

UPDATE affiliate_rebate_jobs
SET event_occurred_at = COALESCE(event_occurred_at, created_at)
WHERE event_occurred_at IS NULL;

ALTER TABLE affiliate_rebate_jobs
    ALTER COLUMN event_occurred_at SET DEFAULT NOW(),
    ALTER COLUMN event_occurred_at SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'affiliate_rebate_jobs_inviter_fk'
          AND conrelid = 'affiliate_rebate_jobs'::regclass
    ) THEN
        ALTER TABLE affiliate_rebate_jobs
            ADD CONSTRAINT affiliate_rebate_jobs_inviter_fk
            FOREIGN KEY (inviter_user_id) REFERENCES users(id) ON DELETE SET NULL;
    END IF;
END $$;

UPDATE affiliate_rebate_jobs j
SET inviter_user_id = r.inviter_user_id,
    relation_effective_at = r.effective_at
FROM player_invitation_relations r
WHERE j.invitee_user_id = r.invitee_user_id
  AND j.inviter_user_id IS NULL
  AND r.effective_at <= COALESCE(j.event_occurred_at, j.created_at);

UPDATE affiliate_rebate_jobs j
SET inviter_user_id = ua.inviter_id,
    relation_effective_at = ua.inviter_effective_at
FROM user_affiliates ua
WHERE j.invitee_user_id = ua.user_id
  AND j.inviter_user_id IS NULL
  AND ua.inviter_id IS NOT NULL
  AND ua.inviter_effective_at IS NOT NULL
  AND ua.inviter_effective_at <= COALESCE(j.event_occurred_at, j.created_at);

CREATE INDEX IF NOT EXISTS idx_affiliate_rebate_jobs_inviter_event
    ON affiliate_rebate_jobs(inviter_user_id, event_occurred_at);

COMMENT ON TABLE player_invitation_quota_events IS
    'Append-only player invitation quota ledger; initial grant is lazily inserted once per user';
COMMENT ON TABLE player_invitation_reservations IS
    'One-time player invitation credentials; only token_hash is stored';
COMMENT ON TABLE player_invitation_relations IS
    'Authoritative invitation/rebate relationship with effective_at';
COMMENT ON TABLE player_invitation_audit_logs IS
    'Administrative invitation quota and relationship audit trail';
COMMENT ON COLUMN affiliate_rebate_jobs.inviter_user_id IS
    'Relation snapshot captured at enqueue time; NULL means no eligible inviter at event time';
COMMENT ON COLUMN affiliate_rebate_jobs.relation_effective_at IS
    'Relationship effective time captured with inviter snapshot';
COMMENT ON COLUMN affiliate_rebate_jobs.event_occurred_at IS
    'Business event time used to prevent backfilled relations from reaching old events';
COMMENT ON COLUMN affiliate_rebate_jobs.policy_snapshot IS
    'Optional enqueue-time policy snapshot for replay-safe rebate calculation';

ALTER TABLE player_invitation_reservations ADD COLUMN IF NOT EXISTS request_key_hash CHAR(64);
CREATE UNIQUE INDEX IF NOT EXISTS player_invitation_request_unique
  ON player_invitation_reservations(inviter_user_id, request_key_hash) WHERE request_key_hash IS NOT NULL;

ALTER TABLE affiliate_rebate_jobs ADD COLUMN IF NOT EXISTS skip_reason TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS player_invitation_admin_request_unique ON player_invitation_relations(created_by_user_id,request_id) WHERE request_id IS NOT NULL AND created_by_user_id IS NOT NULL;
