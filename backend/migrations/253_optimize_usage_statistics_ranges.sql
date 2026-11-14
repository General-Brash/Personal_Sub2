-- Separate branches let the view's constant is_archived values prune the
-- unused UNION ALL leaf and push exact raw bounds down to usage_logs.
-- Reuse the bucket view to preserve its complete row type and DST arithmetic.
CREATE OR REPLACE FUNCTION usage_statistics_range(p_start TIMESTAMPTZ, p_end TIMESTAMPTZ)
RETURNS SETOF usage_statistics_buckets
LANGUAGE sql STABLE AS $$
    SELECT * FROM usage_statistics_buckets
    WHERE NOT is_archived
      AND (p_start IS NULL OR p_end IS NULL OR p_start < p_end)
      AND (p_start IS NULL OR created_at >= p_start)
      AND (p_end IS NULL OR created_at < p_end)
    UNION ALL
    SELECT * FROM usage_statistics_buckets
    WHERE is_archived
      AND (p_start IS NULL OR p_end IS NULL OR p_start < p_end)
      AND (p_start IS NULL OR bucket_end > p_start)
      AND (p_end IS NULL OR created_at < p_end)
$$;

-- Metadata needs only archived buckets; restrict the source before aggregation
-- so an empty archive never requires scanning uncompacted requests.
CREATE OR REPLACE FUNCTION usage_statistics_archive_range(p_start TIMESTAMPTZ, p_end TIMESTAMPTZ)
RETURNS SETOF usage_statistics_buckets
LANGUAGE sql STABLE AS $$
    SELECT * FROM usage_statistics_buckets
    WHERE is_archived
      AND (p_start IS NULL OR p_end IS NULL OR p_start < p_end)
      AND (p_start IS NULL OR bucket_end > p_start)
      AND (p_end IS NULL OR created_at < p_end)
$$;

COMMENT ON FUNCTION usage_statistics_archive_range(TIMESTAMPTZ, TIMESTAMPTZ) IS
    'Archive-only metadata source; includes whole intersecting days with the same range policy as usage_statistics_range.';

-- Refresh selectivity estimates, including the newly added compacted_at column.
ANALYZE usage_logs;
ANALYZE usage_history_daily;
