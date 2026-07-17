-- Idempotency keys are scoped by operation and actor. Existing binary keys are
-- intentionally isolated as legacy rows so they cannot be replayed by another
-- actor after the schema upgrade.

ALTER TABLE idempotency_records
    ADD COLUMN IF NOT EXISTS operation_scope VARCHAR(128);

ALTER TABLE idempotency_records
    ADD COLUMN IF NOT EXISTS actor_scope VARCHAR(128);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'idempotency_records'
          AND column_name = 'scope'
    ) THEN
        EXECUTE '
            UPDATE idempotency_records
            SET operation_scope = scope
            WHERE operation_scope IS NULL';
    END IF;
END;
$$;

UPDATE idempotency_records
SET actor_scope = 'legacy:' || id::text
WHERE actor_scope IS NULL;

ALTER TABLE idempotency_records
    ALTER COLUMN operation_scope SET NOT NULL;

ALTER TABLE idempotency_records
    ALTER COLUMN actor_scope SET NOT NULL;

DROP INDEX IF EXISTS idx_idempotency_records_scope_key;

CREATE UNIQUE INDEX IF NOT EXISTS idx_idempotency_records_operation_actor_key
    ON idempotency_records (operation_scope, actor_scope, idempotency_key_hash);

ALTER TABLE idempotency_records
    DROP COLUMN IF EXISTS scope;
