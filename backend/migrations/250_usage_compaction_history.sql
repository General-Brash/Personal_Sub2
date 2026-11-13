-- Structures only. Historical backfill and compaction are explicit jobs.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS compacted_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_usage_logs_uncompacted_created_id
    ON usage_logs (created_at, id) WHERE compacted_at IS NULL;

CREATE TABLE IF NOT EXISTS usage_history_daily (
    id BIGSERIAL PRIMARY KEY,
    bucket_start TIMESTAMPTZ NOT NULL,
    month_start DATE NOT NULL,
    timezone TEXT NOT NULL,
    dimensions JSONB NOT NULL,
    dimension_key TEXT NOT NULL,
    request_count BIGINT NOT NULL CHECK (request_count > 0),
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_5m_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_1h_tokens BIGINT NOT NULL DEFAULT 0,
    image_input_tokens BIGINT NOT NULL DEFAULT 0,
    image_output_tokens BIGINT NOT NULL DEFAULT 0,
    image_count BIGINT NOT NULL DEFAULT 0,
    video_count BIGINT NOT NULL DEFAULT 0,
    input_cost NUMERIC(38,10) NOT NULL DEFAULT 0,
    output_cost NUMERIC(38,10) NOT NULL DEFAULT 0,
    cache_creation_cost NUMERIC(38,10) NOT NULL DEFAULT 0,
    cache_read_cost NUMERIC(38,10) NOT NULL DEFAULT 0,
    image_input_cost NUMERIC(38,10) NOT NULL DEFAULT 0,
    image_output_cost NUMERIC(38,10) NOT NULL DEFAULT 0,
    total_cost NUMERIC(38,10) NOT NULL DEFAULT 0,
    actual_cost NUMERIC(38,10) NOT NULL DEFAULT 0,
    account_cost NUMERIC NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    duration_samples BIGINT NOT NULL DEFAULT 0,
    first_created_at TIMESTAMPTZ NOT NULL,
    last_created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (bucket_start, dimension_key)
);
CREATE INDEX IF NOT EXISTS idx_usage_history_daily_month_user
    ON usage_history_daily (month_start, ((dimensions->>'user_id')::bigint));
CREATE INDEX IF NOT EXISTS idx_usage_history_daily_key_bucket
    ON usage_history_daily (((dimensions->>'api_key_id')::bigint), bucket_start);

CREATE TABLE IF NOT EXISTS usage_request_archive (
    request_id VARCHAR(255) NOT NULL,
    api_key_id BIGINT NOT NULL,
    usage_log_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (request_id, api_key_id)
);
CREATE INDEX IF NOT EXISTS idx_usage_request_archive_log_id ON usage_request_archive (usage_log_id);

-- Short compaction batches lock exclusively; inserts share the same lock so
-- archived request identities cannot slip through legacy billing writers.
CREATE OR REPLACE FUNCTION guard_compacted_usage_request() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock_shared(25020261109::bigint);
    IF NEW.request_id IS NOT NULL AND btrim(NEW.request_id) <> '' AND EXISTS (
        SELECT 1 FROM usage_request_archive
        WHERE request_id = NEW.request_id AND api_key_id = NEW.api_key_id
    ) THEN
        RETURN NULL;
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS usage_logs_compacted_request_guard ON usage_logs;
CREATE TRIGGER usage_logs_compacted_request_guard BEFORE INSERT ON usage_logs
    FOR EACH ROW EXECUTE FUNCTION guard_compacted_usage_request();

-- Exactly one contribution: an original row or its durable numeric history.
CREATE OR REPLACE VIEW usage_statistics AS
SELECT id, created_at, created_at AS last_created_at,
       user_id, api_key_id, account_id, group_id, model, requested_model,
       upstream_model, upstream_response_model, upstream_model_mismatch,
       channel_id, model_mapping_chain, billing_tier, billing_mode,
       billing_type, request_type, stream, openai_ws_mode,
       inbound_endpoint, upstream_endpoint, service_tier, reasoning_effort,
       requested_reasoning_effort,
       input_tokens::bigint, output_tokens::bigint, cache_creation_tokens::bigint,
       cache_read_tokens::bigint, cache_creation_5m_tokens::bigint,
       cache_creation_1h_tokens::bigint, image_input_tokens::bigint,
       image_output_tokens::bigint, image_count::bigint, video_count::bigint,
       input_cost, output_cost, cache_creation_cost, cache_read_cost,
       image_input_cost, image_output_cost, total_cost, actual_cost,
       COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier,1) AS account_stats_cost,
       1::numeric AS account_rate_multiplier,
       COALESCE(duration_ms,0)::bigint AS duration_ms,
       CASE WHEN duration_ms IS NULL THEN 0 ELSE 1 END::bigint AS duration_samples,
       1::bigint AS request_count, NULL::text AS platform, false AS is_archived,
       created_at AS financial_created_at
FROM usage_logs WHERE compacted_at IS NULL
UNION ALL
SELECT -id, bucket_start, last_created_at,
       (dimensions->>'user_id')::bigint, (dimensions->>'api_key_id')::bigint,
       (dimensions->>'account_id')::bigint, (dimensions->>'group_id')::bigint,
       dimensions->>'model', dimensions->>'requested_model',
       dimensions->>'upstream_model', dimensions->>'upstream_response_model',
       (dimensions->>'upstream_model_mismatch')::boolean,
       (dimensions->>'channel_id')::bigint, dimensions->>'model_mapping_chain',
       dimensions->>'billing_tier', dimensions->>'billing_mode',
       (dimensions->>'billing_type')::smallint, (dimensions->>'request_type')::smallint,
       (dimensions->>'stream')::boolean, (dimensions->>'openai_ws_mode')::boolean,
       dimensions->>'inbound_endpoint', dimensions->>'upstream_endpoint',
       dimensions->>'service_tier', dimensions->>'reasoning_effort',
       dimensions->>'requested_reasoning_effort',
       input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
       cache_creation_5m_tokens, cache_creation_1h_tokens, image_input_tokens,
       image_output_tokens, image_count, video_count,
       input_cost, output_cost, cache_creation_cost, cache_read_cost,
       image_input_cost, image_output_cost, total_cost, actual_cost, account_cost,
       1::numeric, duration_ms, duration_samples, request_count,
       dimensions->>'platform', true,
       (dimensions->>'financial_day')::date::timestamp AT TIME ZONE 'Asia/Shanghai'
FROM usage_history_daily;

CREATE OR REPLACE VIEW usage_user_monthly AS
SELECT month_start, (dimensions->>'user_id')::bigint AS user_id,
       SUM(request_count)::bigint AS request_count,
       SUM(input_tokens)::bigint AS input_tokens, SUM(output_tokens)::bigint AS output_tokens,
       SUM(cache_creation_tokens)::bigint AS cache_creation_tokens,
       SUM(cache_read_tokens)::bigint AS cache_read_tokens,
       SUM(total_cost) AS total_cost, SUM(actual_cost) AS actual_cost, SUM(account_cost) AS account_cost
FROM usage_history_daily GROUP BY month_start, (dimensions->>'user_id')::bigint;
CREATE OR REPLACE VIEW usage_site_monthly AS
SELECT month_start, SUM(request_count)::bigint AS request_count,
       SUM(input_tokens)::bigint AS input_tokens, SUM(output_tokens)::bigint AS output_tokens,
       SUM(cache_creation_tokens)::bigint AS cache_creation_tokens,
       SUM(cache_read_tokens)::bigint AS cache_read_tokens,
       SUM(total_cost) AS total_cost, SUM(actual_cost) AS actual_cost, SUM(account_cost) AS account_cost
FROM usage_user_monthly GROUP BY month_start;
CREATE OR REPLACE VIEW usage_site_yearly AS
SELECT EXTRACT(YEAR FROM month_start)::integer AS year, SUM(request_count)::bigint AS request_count,
       SUM(input_tokens)::bigint AS input_tokens, SUM(output_tokens)::bigint AS output_tokens,
       SUM(cache_creation_tokens)::bigint AS cache_creation_tokens,
       SUM(cache_read_tokens)::bigint AS cache_read_tokens,
       SUM(total_cost) AS total_cost, SUM(actual_cost) AS actual_cost, SUM(account_cost) AS account_cost
FROM usage_site_monthly GROUP BY EXTRACT(YEAR FROM month_start)::integer;

CREATE OR REPLACE VIEW usage_site_lifetime AS
SELECT COALESCE(SUM(request_count),0)::bigint AS request_count,
       COALESCE(SUM(input_tokens),0)::bigint AS input_tokens,
       COALESCE(SUM(output_tokens),0)::bigint AS output_tokens,
       COALESCE(SUM(cache_creation_tokens),0)::bigint AS cache_creation_tokens,
       COALESCE(SUM(cache_read_tokens),0)::bigint AS cache_read_tokens,
       COALESCE(SUM(total_cost),0) AS total_cost,COALESCE(SUM(actual_cost),0) AS actual_cost,
       COALESCE(SUM(account_cost),0) AS account_cost FROM usage_site_yearly;

CREATE OR REPLACE FUNCTION compact_usage_batch(p_ids BIGINT[], p_timezone TEXT)
RETURNS TABLE(processed_rows BIGINT, deleted_rows BIGINT, protected_rows BIGINT)
LANGUAGE plpgsql AS $$
DECLARE
    v_processed BIGINT;
    v_deleted BIGINT;
    v_before NUMERIC;
    v_count BIGINT;
    v_ids BIGINT[];
BEGIN
    PERFORM pg_advisory_xact_lock(25020261109::bigint);
    PERFORM id FROM usage_group_rollup_state WHERE id=1 FOR UPDATE NOWAIT;
    PERFORM id FROM usage_logs WHERE id = ANY(p_ids) AND compacted_at IS NULL FOR UPDATE NOWAIT;
    SELECT COALESCE(array_agg(id),ARRAY[]::bigint[]) INTO v_ids FROM usage_logs
        WHERE id=ANY(p_ids) AND compacted_at IS NULL;
    p_ids := v_ids;
    SELECT COUNT(*) INTO v_count FROM usage_logs WHERE id=ANY(p_ids) AND compacted_at IS NULL;
    SELECT COALESCE(SUM(request_count),0) INTO v_before FROM usage_history_daily;
    IF EXISTS (SELECT 1 FROM usage_history_daily WHERE timezone<>p_timezone) THEN
        RAISE EXCEPTION 'usage compaction timezone differs from existing history';
    END IF;
    IF EXISTS (SELECT 1 FROM usage_logs WHERE id = ANY(p_ids) AND compacted_at IS NULL
        AND created_at >= date_trunc('month', NOW() AT TIME ZONE p_timezone) AT TIME ZONE p_timezone) THEN
        RAISE EXCEPTION 'current month usage cannot be compacted';
    END IF;
    WITH source AS (
        SELECT ul.*, date_trunc('day', ul.created_at AT TIME ZONE p_timezone) AT TIME ZONE p_timezone AS bucket,
          jsonb_build_object(
            'user_id',ul.user_id,'api_key_id',ul.api_key_id,'account_id',ul.account_id,'group_id',ul.group_id,
            'model',ul.model,'requested_model',ul.requested_model,'upstream_model',ul.upstream_model,
            'upstream_response_model',ul.upstream_response_model,'upstream_model_mismatch',ul.upstream_model_mismatch,
            'channel_id',ul.channel_id,'model_mapping_chain',ul.model_mapping_chain,'billing_tier',ul.billing_tier,
            'billing_mode',ul.billing_mode,'billing_type',ul.billing_type,'request_type',ul.request_type,
            'stream',ul.stream,'openai_ws_mode',ul.openai_ws_mode,
            'inbound_endpoint',ul.inbound_endpoint,'upstream_endpoint',ul.upstream_endpoint,
            'service_tier',ul.service_tier,'reasoning_effort',ul.reasoning_effort,
            'requested_reasoning_effort',ul.requested_reasoning_effort,
            'positive_actual_cost',ul.actual_cost>0,'positive_total_cost',ul.total_cost>0,
            'positive_image_count',COALESCE(ul.image_count,0)>0,
            'financial_day',(ul.created_at AT TIME ZONE 'Asia/Shanghai')::date,
            'platform',CASE WHEN g.platform='composite' THEN a.platform ELSE COALESCE(NULLIF(g.platform,''),a.platform) END
          ) AS dims
        FROM usage_logs ul LEFT JOIN groups g ON g.id=ul.group_id LEFT JOIN accounts a ON a.id=ul.account_id
        WHERE ul.id=ANY(p_ids) AND ul.compacted_at IS NULL
    )
    INSERT INTO usage_history_daily (
        bucket_start,month_start,timezone,dimensions,dimension_key,request_count,
        input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,cache_creation_5m_tokens,cache_creation_1h_tokens,
        image_input_tokens,image_output_tokens,image_count,video_count,input_cost,output_cost,cache_creation_cost,
        cache_read_cost,image_input_cost,image_output_cost,total_cost,actual_cost,account_cost,
        duration_ms,duration_samples,first_created_at,last_created_at
    )
    SELECT bucket,date_trunc('month',bucket AT TIME ZONE p_timezone)::date,p_timezone,dims,md5(dims::text),COUNT(*),
        SUM(input_tokens),SUM(output_tokens),SUM(cache_creation_tokens),SUM(cache_read_tokens),
        SUM(cache_creation_5m_tokens),SUM(cache_creation_1h_tokens),SUM(image_input_tokens),SUM(image_output_tokens),
        COALESCE(SUM(image_count),0),COALESCE(SUM(video_count),0),COALESCE(SUM(input_cost),0),COALESCE(SUM(output_cost),0),COALESCE(SUM(cache_creation_cost),0),COALESCE(SUM(cache_read_cost),0),
        COALESCE(SUM(image_input_cost),0),COALESCE(SUM(image_output_cost),0),COALESCE(SUM(total_cost),0),COALESCE(SUM(actual_cost),0),
        SUM(COALESCE(account_stats_cost,total_cost)*COALESCE(account_rate_multiplier,1)),
        COALESCE(SUM(duration_ms),0),COUNT(duration_ms),MIN(created_at),MAX(created_at)
    FROM source GROUP BY bucket,dims
    ON CONFLICT (bucket_start,dimension_key) DO UPDATE SET
        request_count=usage_history_daily.request_count+EXCLUDED.request_count,
        input_tokens=usage_history_daily.input_tokens+EXCLUDED.input_tokens,
        output_tokens=usage_history_daily.output_tokens+EXCLUDED.output_tokens,
        cache_creation_tokens=usage_history_daily.cache_creation_tokens+EXCLUDED.cache_creation_tokens,
        cache_read_tokens=usage_history_daily.cache_read_tokens+EXCLUDED.cache_read_tokens,
        cache_creation_5m_tokens=usage_history_daily.cache_creation_5m_tokens+EXCLUDED.cache_creation_5m_tokens,
        cache_creation_1h_tokens=usage_history_daily.cache_creation_1h_tokens+EXCLUDED.cache_creation_1h_tokens,
        image_input_tokens=usage_history_daily.image_input_tokens+EXCLUDED.image_input_tokens,
        image_output_tokens=usage_history_daily.image_output_tokens+EXCLUDED.image_output_tokens,
        image_count=usage_history_daily.image_count+EXCLUDED.image_count,
        video_count=usage_history_daily.video_count+EXCLUDED.video_count,
        input_cost=usage_history_daily.input_cost+EXCLUDED.input_cost,
        output_cost=usage_history_daily.output_cost+EXCLUDED.output_cost,
        cache_creation_cost=usage_history_daily.cache_creation_cost+EXCLUDED.cache_creation_cost,
        cache_read_cost=usage_history_daily.cache_read_cost+EXCLUDED.cache_read_cost,
        image_input_cost=usage_history_daily.image_input_cost+EXCLUDED.image_input_cost,
        image_output_cost=usage_history_daily.image_output_cost+EXCLUDED.image_output_cost,
        total_cost=usage_history_daily.total_cost+EXCLUDED.total_cost,
        actual_cost=usage_history_daily.actual_cost+EXCLUDED.actual_cost,
        account_cost=usage_history_daily.account_cost+EXCLUDED.account_cost,
        duration_ms=usage_history_daily.duration_ms+EXCLUDED.duration_ms,
        duration_samples=usage_history_daily.duration_samples+EXCLUDED.duration_samples,
        first_created_at=LEAST(usage_history_daily.first_created_at,EXCLUDED.first_created_at),
        last_created_at=GREATEST(usage_history_daily.last_created_at,EXCLUDED.last_created_at),updated_at=NOW()
    WHERE usage_history_daily.dimensions=EXCLUDED.dimensions;
    IF (SELECT COALESCE(SUM(request_count),0) FROM usage_history_daily) <> v_before+v_count THEN
        RAISE EXCEPTION 'usage compaction contribution mismatch';
    END IF;
    IF EXISTS (
        SELECT 1 FROM usage_logs ul JOIN usage_history_daily h
          ON h.bucket_start=date_trunc('day',ul.created_at AT TIME ZONE p_timezone) AT TIME ZONE p_timezone
        WHERE ul.id=ANY(p_ids) AND ul.compacted_at IS NULL AND h.timezone<>p_timezone
    ) THEN
        RAISE EXCEPTION 'usage compaction timezone differs from existing history';
    END IF;
    INSERT INTO usage_request_archive(request_id,api_key_id,usage_log_id,user_id,created_at)
        SELECT request_id,api_key_id,id,user_id,created_at FROM usage_logs
        WHERE id=ANY(p_ids) AND compacted_at IS NULL AND request_id IS NOT NULL AND btrim(request_id)<>''
        ON CONFLICT(request_id,api_key_id) DO NOTHING;
    UPDATE usage_logs SET compacted_at=NOW() WHERE id=ANY(p_ids) AND compacted_at IS NULL;
    GET DIAGNOSTICS v_processed=ROW_COUNT;
    DELETE FROM usage_logs ul WHERE ul.id=ANY(p_ids) AND ul.compacted_at IS NOT NULL
        AND NOT EXISTS (SELECT 1 FROM temporary_credit_consumptions t WHERE t.usage_log_id=ul.id)
        AND NOT EXISTS (SELECT 1 FROM billing_usage_entries b WHERE b.usage_log_id=ul.id);
    GET DIAGNOSTICS v_deleted=ROW_COUNT;
    RETURN QUERY SELECT v_processed,v_deleted,GREATEST(v_processed-v_deleted,0::bigint);
END;
$$;
