-- Independent audit trail for destructive data-cleanup operations.
-- This table is deliberately excluded from every cleanup target allowlist.

CREATE TABLE IF NOT EXISTS data_cleanup_audits (
    id BIGSERIAL PRIMARY KEY,
    operator_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    operator_email VARCHAR(255) NOT NULL DEFAULT '',
    auth_method VARCHAR(32) NOT NULL DEFAULT '',
    category VARCHAR(64) NOT NULL,
    cleanup_mode VARCHAR(16) NOT NULL,
    filters JSONB NOT NULL DEFAULT '{}'::jsonb,
    preview_rows BIGINT NOT NULL DEFAULT 0,
    blocked_rows BIGINT NOT NULL DEFAULT 0,
    deleted_rows BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL,
    error_message VARCHAR(500) NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT data_cleanup_audits_mode_check CHECK (cleanup_mode IN ('range', 'all')),
    CONSTRAINT data_cleanup_audits_status_check CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'canceled'))
);

CREATE INDEX IF NOT EXISTS data_cleanup_audits_created_idx
    ON data_cleanup_audits (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS data_cleanup_audits_category_created_idx
    ON data_cleanup_audits (category, created_at DESC, id DESC);
