-- Daily check-in rewards and administrator-issued temporary credits are kept
-- outside users.balance and users.total_recharged. Expiry is query-enforced.

CREATE TABLE IF NOT EXISTS daily_checkins (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    checkin_date DATE NOT NULL,
    streak_day INT NOT NULL CHECK (streak_day > 0),
    reward_day INT NOT NULL CHECK (reward_day > 0),
    reward_amount NUMERIC(20,8) NOT NULL CHECK (reward_amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT daily_checkins_user_date_unique UNIQUE (user_id, checkin_date)
);

CREATE INDEX IF NOT EXISTS idx_daily_checkins_user_date_desc
    ON daily_checkins (user_id, checkin_date DESC);

CREATE TABLE IF NOT EXISTS temporary_credit_grants (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    source VARCHAR(20) NOT NULL,
    checkin_id BIGINT NULL REFERENCES daily_checkins(id) ON DELETE RESTRICT,
    amount NUMERIC(20,8) NOT NULL CHECK (amount > 0),
    remaining_amount NUMERIC(20,8) NOT NULL CHECK (remaining_amount >= 0 AND remaining_amount <= amount),
    expires_at TIMESTAMPTZ NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    granted_by BIGINT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT temporary_credit_grants_source_check CHECK (
        (source = 'checkin' AND checkin_id IS NOT NULL AND granted_by IS NULL AND notes = '')
        OR
        (source = 'admin_grant' AND checkin_id IS NULL AND granted_by IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS temporary_credit_grants_checkin_id_unique
    ON temporary_credit_grants (checkin_id)
    WHERE checkin_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_temporary_credit_grants_fefo_available
    ON temporary_credit_grants (user_id, expires_at ASC, id ASC)
    WHERE remaining_amount > 0;

CREATE TABLE IF NOT EXISTS temporary_credit_consumptions (
    id BIGSERIAL PRIMARY KEY,
    grant_id BIGINT NOT NULL REFERENCES temporary_credit_grants(id) ON DELETE RESTRICT,
    usage_log_id BIGINT NULL REFERENCES usage_logs(id) ON DELETE RESTRICT,
    request_id VARCHAR(255) NULL,
    amount NUMERIC(20,8) NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT temporary_credit_consumptions_reference_check CHECK (
        (usage_log_id IS NOT NULL AND request_id IS NULL)
        OR
        (usage_log_id IS NULL AND request_id IS NOT NULL AND btrim(request_id) <> '')
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS temporary_credit_consumptions_grant_usage_log_unique
    ON temporary_credit_consumptions (grant_id, usage_log_id)
    WHERE usage_log_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS temporary_credit_consumptions_grant_request_unique
    ON temporary_credit_consumptions (grant_id, request_id)
    WHERE request_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_temporary_credit_consumptions_grant_created_at
    ON temporary_credit_consumptions (grant_id, created_at);

CREATE OR REPLACE FUNCTION prevent_temporary_credit_consumption_request_id_update()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.request_id IS DISTINCT FROM OLD.request_id THEN
        RAISE EXCEPTION 'temporary credit consumption request_id is immutable';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS temporary_credit_consumptions_request_id_immutable
    ON temporary_credit_consumptions;

CREATE TRIGGER temporary_credit_consumptions_request_id_immutable
BEFORE UPDATE ON temporary_credit_consumptions
FOR EACH ROW
EXECUTE FUNCTION prevent_temporary_credit_consumption_request_id_update();

INSERT INTO settings (key, value, updated_at)
VALUES
    ('daily_checkin_enabled', 'false', NOW()),
    ('daily_checkin_max_reward_day', '7', NOW()),
    ('daily_checkin_reward_tiers', '[{"day":1,"amount":"1.00000000"},{"day":2,"amount":"1.00000000"},{"day":3,"amount":"1.00000000"},{"day":4,"amount":"1.00000000"},{"day":5,"amount":"1.00000000"},{"day":6,"amount":"1.00000000"},{"day":7,"amount":"1.00000000"}]', NOW())
ON CONFLICT (key) DO NOTHING;
