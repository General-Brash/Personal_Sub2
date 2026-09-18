-- OIDC Provider second-round parity and audit hardening.
-- Raw SQL migrations remain authoritative for these custom tables; Ent schemas
-- mirror this shape for static generation/parity only. This migration is
-- additive and is intentionally not executed by the coding-only validation.

ALTER TABLE oidc_clients
    ADD COLUMN IF NOT EXISTS last_change_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE oidc_client_secrets
    ADD COLUMN IF NOT EXISTS created_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE oidc_consents
    ADD COLUMN IF NOT EXISTS revoked_by BIGINT REFERENCES users(id) ON DELETE RESTRICT;

ALTER TABLE oidc_signing_keys
    ADD COLUMN IF NOT EXISTS last_changed_by BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS last_change_reason TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS oidc_audit_action_lookup_idx
    ON audit_logs (action, created_at DESC, id DESC);

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
  ('oidc.keys.revoke', 'oidc', 'keys.revoke', true, 'Retire or revoke OIDC Provider signing keys'),
  ('oidc.audit.read', 'oidc', 'audit.read', true, 'Read OIDC Provider audit events')
ON CONFLICT(permission) DO NOTHING;
