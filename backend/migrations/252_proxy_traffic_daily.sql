-- Independent of request logs: cleanup/compaction must not erase node traffic.
-- Per-process absolute counters make flush retries safe after uncertain commits.
CREATE TABLE IF NOT EXISTS proxy_traffic_daily (
    proxy_id BIGINT NOT NULL REFERENCES proxies(id) ON DELETE CASCADE,
    usage_date DATE NOT NULL,
    source_id UUID NOT NULL,
    upload_bytes BIGINT NOT NULL DEFAULT 0 CHECK (upload_bytes >= 0),
    download_bytes BIGINT NOT NULL DEFAULT 0 CHECK (download_bytes >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (proxy_id, usage_date, source_id)
);
