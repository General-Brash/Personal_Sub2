-- Support latest per-user usage across uncompacted logs and daily history.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_user_latest_uncompacted
    ON usage_logs (user_id, created_at DESC)
    WHERE compacted_at IS NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_history_daily_user_latest
    ON usage_history_daily (((dimensions->>'user_id')::bigint), last_created_at DESC);
