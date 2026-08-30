-- 228 introduced positive multiplier checks, but PostgreSQL numeric also
-- accepts the special values NaN, Infinity and -Infinity. Normalize every
-- legacy invalid multiplier to NULL before adding the stricter constraints.
-- NULL means "not configured" and each UPDATE is idempotent.
UPDATE channel_model_pricing
SET fast_multiplier = CASE
        WHEN fast_multiplier::text IN ('NaN', 'Infinity', '-Infinity') OR fast_multiplier <= 0 THEN NULL
        ELSE fast_multiplier
    END,
    flex_multiplier = CASE
        WHEN flex_multiplier::text IN ('NaN', 'Infinity', '-Infinity') OR flex_multiplier <= 0 THEN NULL
        ELSE flex_multiplier
    END
WHERE (fast_multiplier IS NOT NULL AND (fast_multiplier::text IN ('NaN', 'Infinity', '-Infinity') OR fast_multiplier <= 0))
   OR (flex_multiplier IS NOT NULL AND (flex_multiplier::text IN ('NaN', 'Infinity', '-Infinity') OR flex_multiplier <= 0));

UPDATE channel_pricing_intervals
SET input_multiplier = CASE
        WHEN input_multiplier::text IN ('NaN', 'Infinity', '-Infinity') OR input_multiplier <= 0 THEN NULL
        ELSE input_multiplier
    END,
    output_multiplier = CASE
        WHEN output_multiplier::text IN ('NaN', 'Infinity', '-Infinity') OR output_multiplier <= 0 THEN NULL
        ELSE output_multiplier
    END,
    cache_write_multiplier = CASE
        WHEN cache_write_multiplier::text IN ('NaN', 'Infinity', '-Infinity') OR cache_write_multiplier <= 0 THEN NULL
        ELSE cache_write_multiplier
    END,
    cache_read_multiplier = CASE
        WHEN cache_read_multiplier::text IN ('NaN', 'Infinity', '-Infinity') OR cache_read_multiplier <= 0 THEN NULL
        ELSE cache_read_multiplier
    END
WHERE (input_multiplier IS NOT NULL AND (input_multiplier::text IN ('NaN', 'Infinity', '-Infinity') OR input_multiplier <= 0))
   OR (output_multiplier IS NOT NULL AND (output_multiplier::text IN ('NaN', 'Infinity', '-Infinity') OR output_multiplier <= 0))
   OR (cache_write_multiplier IS NOT NULL AND (cache_write_multiplier::text IN ('NaN', 'Infinity', '-Infinity') OR cache_write_multiplier <= 0))
   OR (cache_read_multiplier IS NOT NULL AND (cache_read_multiplier::text IN ('NaN', 'Infinity', '-Infinity') OR cache_read_multiplier <= 0));

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_model_pricing_fast_multiplier_finite_positive' AND conrelid = 'channel_model_pricing'::regclass) THEN
        ALTER TABLE channel_model_pricing
            ADD CONSTRAINT channel_model_pricing_fast_multiplier_finite_positive
            CHECK (fast_multiplier IS NULL OR (fast_multiplier::text NOT IN ('NaN', 'Infinity', '-Infinity') AND fast_multiplier > 0));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_model_pricing_flex_multiplier_finite_positive' AND conrelid = 'channel_model_pricing'::regclass) THEN
        ALTER TABLE channel_model_pricing
            ADD CONSTRAINT channel_model_pricing_flex_multiplier_finite_positive
            CHECK (flex_multiplier IS NULL OR (flex_multiplier::text NOT IN ('NaN', 'Infinity', '-Infinity') AND flex_multiplier > 0));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_pricing_intervals_input_multiplier_finite_positive' AND conrelid = 'channel_pricing_intervals'::regclass) THEN
        ALTER TABLE channel_pricing_intervals
            ADD CONSTRAINT channel_pricing_intervals_input_multiplier_finite_positive
            CHECK (input_multiplier IS NULL OR (input_multiplier::text NOT IN ('NaN', 'Infinity', '-Infinity') AND input_multiplier > 0));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_pricing_intervals_output_multiplier_finite_positive' AND conrelid = 'channel_pricing_intervals'::regclass) THEN
        ALTER TABLE channel_pricing_intervals
            ADD CONSTRAINT channel_pricing_intervals_output_multiplier_finite_positive
            CHECK (output_multiplier IS NULL OR (output_multiplier::text NOT IN ('NaN', 'Infinity', '-Infinity') AND output_multiplier > 0));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_pricing_intervals_cache_write_multiplier_finite_positive' AND conrelid = 'channel_pricing_intervals'::regclass) THEN
        ALTER TABLE channel_pricing_intervals
            ADD CONSTRAINT channel_pricing_intervals_cache_write_multiplier_finite_positive
            CHECK (cache_write_multiplier IS NULL OR (cache_write_multiplier::text NOT IN ('NaN', 'Infinity', '-Infinity') AND cache_write_multiplier > 0));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'channel_pricing_intervals_cache_read_multiplier_finite_positive' AND conrelid = 'channel_pricing_intervals'::regclass) THEN
        ALTER TABLE channel_pricing_intervals
            ADD CONSTRAINT channel_pricing_intervals_cache_read_multiplier_finite_positive
            CHECK (cache_read_multiplier IS NULL OR (cache_read_multiplier::text NOT IN ('NaN', 'Infinity', '-Infinity') AND cache_read_multiplier > 0));
    END IF;
END $$;
