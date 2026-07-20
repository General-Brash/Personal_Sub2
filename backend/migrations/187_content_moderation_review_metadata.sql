ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS review_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;
