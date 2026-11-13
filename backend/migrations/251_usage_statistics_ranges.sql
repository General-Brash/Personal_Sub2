-- Historical statistics are whole compaction-day estimates, not request timestamps.
-- Keep 250 immutable for installations that have already applied it.
CREATE OR REPLACE VIEW usage_statistics_buckets AS
SELECT s.*,
       CASE WHEN s.is_archived
            THEN ((h.bucket_start AT TIME ZONE h.timezone) + INTERVAL '1 day') AT TIME ZONE h.timezone
            ELSE s.created_at END AS bucket_end,
       h.timezone AS bucket_timezone
FROM usage_statistics s
LEFT JOIN usage_history_daily h ON s.is_archived AND h.id = -s.id;

-- Raw requests keep exact half-open boundaries. Include every intersecting
-- historical day in full, including days whose midnight precedes p_start.
-- Local-calendar arithmetic preserves 23/25-hour days at DST transitions.
CREATE OR REPLACE FUNCTION usage_statistics_range(p_start TIMESTAMPTZ, p_end TIMESTAMPTZ)
RETURNS SETOF usage_statistics_buckets
LANGUAGE sql STABLE AS $$
    SELECT * FROM usage_statistics_buckets
    WHERE (p_start IS NULL OR p_end IS NULL OR p_start < p_end)
      AND (p_end IS NULL OR created_at < p_end)
      AND (p_start IS NULL OR
           CASE WHEN is_archived THEN bucket_end > p_start ELSE created_at >= p_start END)
$$;

COMMENT ON FUNCTION usage_statistics_range(TIMESTAMPTZ, TIMESTAMPTZ) IS
    'Raw rows are exact; archived rows include whole intersecting days in their compaction timezone. Never prorate or interpret archived created_at as a request time.';
