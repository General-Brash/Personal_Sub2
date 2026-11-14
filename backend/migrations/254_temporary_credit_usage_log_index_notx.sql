CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_temporary_credit_consumptions_usage_log_id
    ON temporary_credit_consumptions (usage_log_id)
    WHERE usage_log_id IS NOT NULL;
