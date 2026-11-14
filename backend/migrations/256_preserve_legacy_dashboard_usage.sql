-- Legacy dashboard buckets can outlive their original requests. Preserve only
-- their missing site-wide contribution, without fabricating user/key history.
CREATE TABLE IF NOT EXISTS usage_dashboard_legacy_daily (
    bucket_date DATE PRIMARY KEY,
    timezone TEXT NOT NULL,
    total_requests BIGINT NOT NULL CHECK (total_requests > 0),
    input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_creation_tokens >= 0),
    cache_read_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
    total_cost NUMERIC(38,10) NOT NULL DEFAULT 0,
    actual_cost NUMERIC(38,10) NOT NULL DEFAULT 0,
    account_cost NUMERIC NOT NULL DEFAULT 0,
    total_duration_ms BIGINT NOT NULL DEFAULT 0 CHECK (total_duration_ms >= 0),
    source_snapshot JSONB NOT NULL,
    represented_snapshot JSONB NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS usage_dashboard_legacy_capture (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    timezone TEXT NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- A history bucket with different actual boundaries cannot be split into
-- exact legacy days. Fail before recording capture, never silently lose a gap.
DO $$
DECLARE
    v_timezone TEXT := COALESCE(NULLIF(current_setting('sub2api.legacy_dashboard_timezone', true), ''), current_setting('TimeZone'));
BEGIN
    PERFORM NOW() AT TIME ZONE v_timezone;
    PERFORM pg_advisory_xact_lock_shared(25020261109::bigint);
    LOCK TABLE usage_dashboard_daily IN SHARE MODE;
    IF NOT EXISTS (SELECT 1 FROM usage_dashboard_legacy_capture WHERE id = 1) AND EXISTS (
        SELECT 1 FROM usage_dashboard_daily d JOIN usage_history_daily h
          ON h.bucket_start < (d.bucket_date + 1)::timestamp AT TIME ZONE v_timezone
         AND ((h.bucket_start AT TIME ZONE h.timezone) + INTERVAL '1 day') AT TIME ZONE h.timezone
             > d.bucket_date::timestamp AT TIME ZONE v_timezone
        WHERE d.bucket_date < (NOW() AT TIME ZONE v_timezone)::date
          AND (h.bucket_start <> d.bucket_date::timestamp AT TIME ZONE v_timezone
               OR ((h.bucket_start AT TIME ZONE h.timezone) + INTERVAL '1 day') AT TIME ZONE h.timezone
                   <> (d.bucket_date + 1)::timestamp AT TIME ZONE v_timezone)
    ) THEN
        RAISE EXCEPTION 'legacy dashboard and archive day boundaries differ; reconcile using the original dashboard timezone before retrying';
    END IF;

-- InitEnt opens migrations in the configured application timezone, which the
-- legacy daily writer also used. Freeze one capture, including when no gaps
-- exist, so a direct replay after cache changes cannot invent new history.
-- If that timezone changed, set sub2api.legacy_dashboard_timezone to the
-- verified original timezone on this connection before applying the migration.
WITH capture AS (
    INSERT INTO usage_dashboard_legacy_capture (id, timezone)
    VALUES (1, v_timezone)
    ON CONFLICT (id) DO NOTHING
    RETURNING timezone
), legacy AS (
    SELECT d.*, c.timezone,
           d.bucket_date::timestamp AT TIME ZONE c.timezone AS bucket_start,
           (d.bucket_date + 1)::timestamp AT TIME ZONE c.timezone AS bucket_end
    FROM usage_dashboard_daily d CROSS JOIN capture c
    WHERE d.bucket_date < (NOW() AT TIME ZONE c.timezone)::date
), missing AS (
    SELECT d.bucket_date, d.timezone,
           d.total_requests - c.total_requests AS total_requests,
           d.input_tokens - c.input_tokens AS input_tokens,
           d.output_tokens - c.output_tokens AS output_tokens,
           d.cache_creation_tokens - c.cache_creation_tokens AS cache_creation_tokens,
           d.cache_read_tokens - c.cache_read_tokens AS cache_read_tokens,
           d.total_cost - c.total_cost AS total_cost,
           d.actual_cost - c.actual_cost AS actual_cost,
           d.account_cost - c.account_cost AS account_cost,
           d.total_duration_ms - c.total_duration_ms AS total_duration_ms,
           to_jsonb(d) - 'timezone' - 'bucket_start' - 'bucket_end' AS source_snapshot,
           to_jsonb(c) AS represented_snapshot
    FROM legacy d
    CROSS JOIN LATERAL (
        SELECT COALESCE(SUM(request_count), 0) AS total_requests,
               COALESCE(SUM(input_tokens), 0) AS input_tokens,
               COALESCE(SUM(output_tokens), 0) AS output_tokens,
               COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
               COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
               COALESCE(SUM(total_cost), 0) AS total_cost,
               COALESCE(SUM(actual_cost), 0) AS actual_cost,
               COALESCE(SUM(account_stats_cost * account_rate_multiplier), 0) AS account_cost,
               COALESCE(SUM(duration_ms), 0) AS total_duration_ms
        FROM usage_statistics_range(d.bucket_start, d.bucket_end)
    ) c
)
INSERT INTO usage_dashboard_legacy_daily (
    bucket_date, timezone, total_requests, input_tokens, output_tokens,
    cache_creation_tokens, cache_read_tokens, total_cost, actual_cost,
    account_cost, total_duration_ms, source_snapshot, represented_snapshot
)
SELECT bucket_date, timezone, total_requests, input_tokens, output_tokens,
       cache_creation_tokens, cache_read_tokens, total_cost, actual_cost,
       account_cost, total_duration_ms, source_snapshot, represented_snapshot
FROM missing
WHERE total_requests > 0
ON CONFLICT (bucket_date) DO NOTHING;
END;
$$;

-- Use original local-calendar boundaries, including 23/25-hour DST days.
CREATE OR REPLACE VIEW usage_dashboard_legacy_daily_buckets AS
SELECT l.*,
       l.bucket_date::timestamp AT TIME ZONE l.timezone AS bucket_start,
       (l.bucket_date + 1)::timestamp AT TIME ZONE l.timezone AS bucket_end
FROM usage_dashboard_legacy_daily l;

COMMENT ON TABLE usage_dashboard_legacy_daily IS
    'Durable site-only legacy deltas missing from usage_statistics at capture. Never expire or rebuild as dashboard cache. Restoring old raw data requires explicit reconciliation.';
