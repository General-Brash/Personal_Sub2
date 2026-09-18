-- OIDC consent binding and atomic revocation hardening.
-- This migration is append-only: migrations 242 and 243 remain unchanged.
-- Existing rows that cannot be proven to have a consent fail the migration
-- rather than being assigned an invented consent.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'oidc_authorization_transactions_consent_fk'
    ) THEN
        ALTER TABLE oidc_authorization_transactions
            ADD CONSTRAINT oidc_authorization_transactions_consent_fk
            FOREIGN KEY (consent_id) REFERENCES oidc_consents(id) ON DELETE RESTRICT;
    END IF;
END $$;

ALTER TABLE oidc_authorization_codes
    ADD COLUMN IF NOT EXISTS consent_id BIGINT;

UPDATE oidc_authorization_codes c
SET consent_id = t.consent_id
FROM oidc_authorization_transactions t
WHERE t.id = c.transaction_id
  AND c.consent_id IS NULL;

ALTER TABLE oidc_authorization_codes
    ALTER COLUMN consent_id SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'oidc_authorization_codes_consent_fk'
    ) THEN
        ALTER TABLE oidc_authorization_codes
            ADD CONSTRAINT oidc_authorization_codes_consent_fk
            FOREIGN KEY (consent_id) REFERENCES oidc_consents(id) ON DELETE RESTRICT;
    END IF;
END $$;

ALTER TABLE oidc_access_tokens
    ADD COLUMN IF NOT EXISTS consent_id BIGINT;

-- Do not backfill access-token consent ids from indirect or ambiguous history.
-- SET NOT NULL intentionally fails closed if this migration is applied to a
-- database containing tokens created before consent binding was enforced.
ALTER TABLE oidc_access_tokens
    ALTER COLUMN consent_id SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'oidc_access_tokens_consent_fk'
    ) THEN
        ALTER TABLE oidc_access_tokens
            ADD CONSTRAINT oidc_access_tokens_consent_fk
            FOREIGN KEY (consent_id) REFERENCES oidc_consents(id) ON DELETE RESTRICT;
    END IF;
END $$;

ALTER TABLE oidc_refresh_token_families
    ALTER COLUMN consent_id SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'oidc_authorization_transactions_consent_state_ck'
    ) THEN
        ALTER TABLE oidc_authorization_transactions
            ADD CONSTRAINT oidc_authorization_transactions_consent_state_ck
            CHECK (status NOT IN ('consented', 'code_issued') OR consent_id IS NOT NULL);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS oidc_authorization_codes_consent_idx
    ON oidc_authorization_codes (consent_id, status, expires_at);

CREATE INDEX IF NOT EXISTS oidc_access_tokens_consent_idx
    ON oidc_access_tokens (consent_id, revoked_at, expires_at);

CREATE INDEX IF NOT EXISTS oidc_refresh_token_families_consent_idx
    ON oidc_refresh_token_families (consent_id, status);
