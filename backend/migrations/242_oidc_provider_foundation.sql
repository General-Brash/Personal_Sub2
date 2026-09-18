-- OIDC Provider foundation: disabled-by-default local Provider state.
-- The SQL is additive and deliberately independent from RP/OAuth-login tables,
-- panel JWTs, Redis refresh state, and commercial group/ledger data.

CREATE TABLE oidc_subjects (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
    subject TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE oidc_clients (
    id BIGSERIAL PRIMARY KEY,
    client_id VARCHAR(128) NOT NULL UNIQUE,
    name VARCHAR(200) NOT NULL,
    owner VARCHAR(200) NOT NULL,
    client_type VARCHAR(32) NOT NULL DEFAULT 'confidential' CHECK (client_type = 'confidential'),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    trusted_skip_consent BOOLEAN NOT NULL DEFAULT FALSE,
    policy_version BIGINT NOT NULL DEFAULT 1 CHECK (policy_version > 0),
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    disabled_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    disabled_at TIMESTAMPTZ,
    disabled_reason TEXT,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0)
);

CREATE TABLE oidc_client_secrets (
    id BIGSERIAL PRIMARY KEY,
    client_pk BIGINT NOT NULL REFERENCES oidc_clients(id) ON DELETE RESTRICT,
    secret_digest TEXT NOT NULL,
    fingerprint VARCHAR(64) NOT NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('active', 'retiring', 'revoked', 'expired')),
    not_before TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    revoked_at TIMESTAMPTZ,
    revoke_reason TEXT,
    UNIQUE (client_pk, fingerprint),
    CHECK (expires_at > not_before)
);
CREATE INDEX oidc_client_secrets_active_idx ON oidc_client_secrets(client_pk, status, not_before, expires_at);

CREATE TABLE oidc_client_redirect_uris (
    id BIGSERIAL PRIMARY KEY,
    client_pk BIGINT NOT NULL REFERENCES oidc_clients(id) ON DELETE RESTRICT,
    redirect_uri TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    disabled_at TIMESTAMPTZ,
    UNIQUE (client_pk, redirect_uri),
    CHECK (position('#' IN redirect_uri) = 0)
);

CREATE TABLE oidc_client_scopes (
    id BIGSERIAL PRIMARY KEY,
    client_pk BIGINT NOT NULL REFERENCES oidc_clients(id) ON DELETE RESTRICT,
    scope VARCHAR(64) NOT NULL CHECK (scope IN ('openid', 'profile', 'email', 'roles', 'offline_access')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    UNIQUE (client_pk, scope)
);

CREATE TABLE oidc_browser_sessions (
    id BIGSERIAL PRIMARY KEY,
    handle_digest VARCHAR(64) NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    auth_time TIMESTAMPTZ NOT NULL,
    amr TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    idle_expires_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoke_reason TEXT,
    session_version BIGINT NOT NULL DEFAULT 1,
    CHECK (absolute_expires_at >= idle_expires_at)
);
CREATE INDEX oidc_browser_sessions_user_idx ON oidc_browser_sessions(user_id, revoked_at);
CREATE INDEX oidc_browser_sessions_expiry_idx ON oidc_browser_sessions(idle_expires_at, absolute_expires_at);

CREATE TABLE oidc_authorization_transactions (
    id BIGSERIAL PRIMARY KEY,
    handle_digest VARCHAR(64) NOT NULL UNIQUE,
    client_pk BIGINT NOT NULL REFERENCES oidc_clients(id) ON DELETE RESTRICT,
    browser_session_id BIGINT REFERENCES oidc_browser_sessions(id) ON DELETE RESTRICT,
    user_id BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    redirect_uri TEXT NOT NULL,
    scope_snapshot TEXT NOT NULL,
    state_ciphertext TEXT NOT NULL,
    state_fingerprint VARCHAR(64) NOT NULL,
    nonce_ciphertext TEXT NOT NULL,
    nonce_fingerprint VARCHAR(64) NOT NULL,
    code_challenge VARCHAR(128) NOT NULL,
    code_challenge_method VARCHAR(16) NOT NULL CHECK (code_challenge_method = 'S256'),
    prompt TEXT NOT NULL DEFAULT '',
    max_age_seconds BIGINT,
    display VARCHAR(16),
    auth_time TIMESTAMPTZ,
    consent_id BIGINT,
    status VARCHAR(32) NOT NULL CHECK (status IN ('created', 'authenticated', 'consented', 'code_issued', 'denied', 'expired', 'cancelled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1
);
CREATE INDEX oidc_authorization_transactions_expiry_idx ON oidc_authorization_transactions(expires_at, status);

CREATE TABLE oidc_authorization_codes (
    id BIGSERIAL PRIMARY KEY,
    code_digest VARCHAR(64) NOT NULL UNIQUE,
    transaction_id BIGINT NOT NULL UNIQUE REFERENCES oidc_authorization_transactions(id) ON DELETE RESTRICT,
    client_pk BIGINT NOT NULL REFERENCES oidc_clients(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    browser_session_id BIGINT REFERENCES oidc_browser_sessions(id) ON DELETE RESTRICT,
    redirect_uri TEXT NOT NULL,
    scope_snapshot TEXT NOT NULL,
    nonce_ciphertext TEXT NOT NULL,
    nonce_fingerprint VARCHAR(64) NOT NULL,
    code_challenge VARCHAR(128) NOT NULL,
    code_challenge_method VARCHAR(16) NOT NULL CHECK (code_challenge_method = 'S256'),
    auth_time TIMESTAMPTZ NOT NULL,
    status VARCHAR(32) NOT NULL CHECK (status IN ('active', 'consumed', 'expired', 'revoked')),
    issued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ
);
CREATE INDEX oidc_authorization_codes_expiry_idx ON oidc_authorization_codes(expires_at, status);

CREATE TABLE oidc_consents (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    client_pk BIGINT NOT NULL REFERENCES oidc_clients(id) ON DELETE RESTRICT,
    scope_snapshot TEXT NOT NULL,
    scope_set_hash VARCHAR(64) NOT NULL,
    policy_version BIGINT NOT NULL,
    source VARCHAR(32) NOT NULL CHECK (source IN ('interactive', 'admin_pre_authorized')),
    status VARCHAR(32) NOT NULL CHECK (status IN ('active', 'revoked')),
    approved_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    approved_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoke_reason TEXT
);
CREATE UNIQUE INDEX oidc_consents_active_unique_idx ON oidc_consents(user_id, client_pk, policy_version, scope_set_hash) WHERE status = 'active';

CREATE TABLE oidc_access_tokens (
    id BIGSERIAL PRIMARY KEY,
    token_digest VARCHAR(64) NOT NULL UNIQUE,
    client_pk BIGINT NOT NULL REFERENCES oidc_clients(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    purpose VARCHAR(32) NOT NULL CHECK (purpose = 'userinfo'),
    scope_snapshot TEXT NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoked_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    revoke_reason TEXT
);
CREATE INDEX oidc_access_tokens_lookup_idx ON oidc_access_tokens(client_pk, token_digest, expires_at);

CREATE TABLE oidc_refresh_token_families (
    family_id VARCHAR(64) PRIMARY KEY,
    client_pk BIGINT NOT NULL REFERENCES oidc_clients(id) ON DELETE RESTRICT,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    consent_id BIGINT REFERENCES oidc_consents(id) ON DELETE RESTRICT,
    scope_snapshot TEXT NOT NULL,
    status VARCHAR(32) NOT NULL CHECK (status IN ('active', 'revoked', 'compromised', 'expired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    idle_expires_at TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoked_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    revoke_reason TEXT,
    replay_detected_at TIMESTAMPTZ,
    replay_fingerprint VARCHAR(64),
    version BIGINT NOT NULL DEFAULT 1,
    CHECK (absolute_expires_at >= idle_expires_at)
);
CREATE INDEX oidc_refresh_token_families_client_idx ON oidc_refresh_token_families(client_pk, user_id, status);

CREATE TABLE oidc_refresh_tokens (
    id BIGSERIAL PRIMARY KEY,
    token_digest VARCHAR(64) NOT NULL UNIQUE,
    family_id VARCHAR(64) NOT NULL REFERENCES oidc_refresh_token_families(family_id) ON DELETE RESTRICT,
    parent_token_id BIGINT REFERENCES oidc_refresh_tokens(id) ON DELETE RESTRICT,
    status VARCHAR(32) NOT NULL CHECK (status IN ('active', 'rotated', 'replayed', 'revoked', 'expired')),
    issued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    rotated_at TIMESTAMPTZ,
    used_at TIMESTAMPTZ,
    replayed_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    child_token_id BIGINT REFERENCES oidc_refresh_tokens(id) ON DELETE RESTRICT
);
CREATE INDEX oidc_refresh_tokens_family_idx ON oidc_refresh_tokens(family_id, status, expires_at);

CREATE TABLE oidc_signing_keys (
    id BIGSERIAL PRIMARY KEY,
    kid VARCHAR(128) NOT NULL UNIQUE,
    alg VARCHAR(16) NOT NULL CHECK (alg = 'RS256'),
    public_jwk TEXT NOT NULL,
    private_key_ciphertext TEXT NOT NULL,
    fingerprint VARCHAR(64) NOT NULL UNIQUE,
    status VARCHAR(32) NOT NULL CHECK (status IN ('pending', 'active', 'retiring', 'retired', 'revoked')),
    not_before TIMESTAMPTZ NOT NULL,
    not_after TIMESTAMPTZ NOT NULL,
    created_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activated_at TIMESTAMPTZ,
    retired_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    key_encryption_version VARCHAR(32) NOT NULL DEFAULT 'v1',
    CHECK (not_after > not_before)
);
CREATE UNIQUE INDEX oidc_signing_keys_single_active_idx ON oidc_signing_keys(status) WHERE status = 'active';
CREATE INDEX oidc_signing_keys_publish_idx ON oidc_signing_keys(status, not_before, not_after);

INSERT INTO admin_permissions(permission, resource, action, sensitive, description) VALUES
 ('oidc.provider.read', 'oidc', 'provider.read', false, 'Read OIDC Provider status'),
 ('oidc.clients.read', 'oidc', 'clients.read', false, 'Read OIDC Provider clients'),
 ('oidc.clients.write', 'oidc', 'clients.write', true, 'Create or update OIDC Provider clients'),
 ('oidc.clients.secret.rotate', 'oidc', 'clients.secret.rotate', true, 'Rotate OIDC Provider client secrets'),
 ('oidc.clients.disable', 'oidc', 'clients.disable', true, 'Enable or disable OIDC Provider clients'),
 ('oidc.consents.read', 'oidc', 'consents.read', true, 'Read OIDC Provider consents'),
 ('oidc.consents.revoke', 'oidc', 'consents.revoke', true, 'Revoke OIDC Provider consents'),
 ('oidc.keys.read', 'oidc', 'keys.read', true, 'Read OIDC Provider signing key metadata'),
 ('oidc.keys.rotate', 'oidc', 'keys.rotate', true, 'Rotate OIDC Provider signing keys'),
 ('oidc.keys.revoke', 'oidc', 'keys.revoke', true, 'Revoke OIDC Provider signing keys'),
 ('oidc.audit.read', 'oidc', 'audit.read', true, 'Read OIDC Provider audit events')
ON CONFLICT(permission) DO NOTHING;
