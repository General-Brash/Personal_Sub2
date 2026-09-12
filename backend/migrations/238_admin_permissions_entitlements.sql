-- W03/W04: explicit admin permissions and standard/premium entitlements.
-- This migration is intentionally additive and keeps legacy roles on the existing value space.
CREATE TABLE IF NOT EXISTS admin_permissions (
    permission   TEXT PRIMARY KEY,
    resource     TEXT NOT NULL,
    action       TEXT NOT NULL,
    sensitive    BOOLEAN NOT NULL DEFAULT FALSE,
    description  TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (resource, action)
);

ALTER TABLE admin_permissions ADD COLUMN IF NOT EXISTS sensitive BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE admin_permissions ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
ALTER TABLE admin_permissions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

INSERT INTO admin_permissions (permission, resource, action, sensitive, description) VALUES
('users.read','users','read',false,'Read users'),
('users.update','users','update',false,'Edit non-sensitive user fields'),
('users.status','users','status',true,'Enable or disable users'),
('users.delete','users','delete',true,'Delete users'),
('users.balance.adjust','users','balance.adjust',true,'Adjust user balance'),
('users.role.assign','users','role.assign',true,'Assign ordinary admin role'),
('users.entitlement.manage','users','entitlement.manage',true,'Manage user entitlement'),
('groups.read','groups','read',false,'Read groups'),
('groups.create','groups','create',true,'Create groups'),
('groups.update','groups','update',true,'Update groups'),
('groups.delete','groups','delete',true,'Delete groups'),
('groups.rates.manage','groups','rates.manage',true,'Manage group rates'),
('groups.dynamic_rates.manage','groups','dynamic_rates.manage',true,'Manage dynamic group rates'),
('bank.settings.read','bank','settings.read',false,'Read bank settings'),
('bank.settings.update','bank','settings.update',true,'Update bank settings'),
('bank.ledger.read','bank','ledger.read',true,'Read bank ledger'),
('bank.settlement.retry','bank','settlement.retry',true,'Retry bank settlement'),
('affiliates.read','affiliates','read',false,'Read affiliate data'),
('affiliates.quota.adjust','affiliates','quota.adjust',true,'Adjust affiliate quota'),
('affiliates.relationship.create','affiliates','relationship.create',true,'Create affiliate relationship'),
('affiliates.rebate.replay','affiliates','rebate.replay',true,'Replay affiliate rebate'),
('invites.read','invites','read',false,'Read invites'),
('invites.quota.adjust','invites','quota.adjust',true,'Adjust invite quota'),
('mall.products.read','mall','products.read',false,'Read mall products'),
('mall.products.write','mall','products.write',true,'Write mall products'),
('mall.orders.read','mall','orders.read',true,'Read mall orders'),
('mall.refund','mall','refund',true,'Refund mall orders'),
('mall.fulfill','mall','fulfill',true,'Fulfill mall orders'),
('models.catalog.read','models','catalog.read',false,'Read model catalog'),
('models.catalog.write','models','catalog.write',true,'Write model catalog'),
('models.pricing.manage','models','pricing.manage',true,'Manage model pricing'),
('channels.catalog.read','channels','catalog.read',false,'Read channels'),
('channels.catalog.write','channels','catalog.write',true,'Write channels'),
('channels.credentials.read','channels','credentials.read',true,'Read channel credentials'),
('channels.credentials.write','channels','credentials.write',true,'Write channel credentials'),
('accounts.catalog.read','accounts','catalog.read',false,'Read accounts'),
('accounts.catalog.write','accounts','catalog.write',true,'Write accounts'),
('accounts.credentials.read','accounts','credentials.read',true,'Read account credentials'),
('accounts.credentials.write','accounts','credentials.write',true,'Write account credentials'),
('audit.read','audit','read',true,'Read audit data'),
('audit.export','audit','export',true,'Export audit data'),
('ops.read','ops','read',false,'Read operations data'),
('ops.manage','ops','manage',true,'Manage operations'),
('plugins.execute','plugins','execute',true,'Execute plugins'),
('system.settings.manage','system','settings.manage',true,'Manage system settings'),
('security.permissions.grant','security','permissions.grant',true,'Grant or revoke permissions'),
('security.superadmin.assign','security','superadmin.assign',true,'Assign or remove super administrators')
ON CONFLICT (permission) DO UPDATE SET resource=EXCLUDED.resource, action=EXCLUDED.action, sensitive=EXCLUDED.sensitive, description=EXCLUDED.description;

CREATE TABLE IF NOT EXISTS admin_permission_versions (
    user_id    BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    version    BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS admin_principal_grants (
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    permission  TEXT NOT NULL REFERENCES admin_permissions(permission) ON DELETE CASCADE,
    effect      TEXT NOT NULL CHECK (effect IN ('allow','deny')),
    scope       JSONB NOT NULL DEFAULT '{}'::jsonb,
    granted_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    reason      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, permission)
);
CREATE INDEX IF NOT EXISTS idx_admin_principal_grants_user_effect ON admin_principal_grants(user_id, effect);

CREATE TABLE IF NOT EXISTS admin_api_key_bindings (
    binding_key TEXT PRIMARY KEY,
    principal_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    rotated_at TIMESTAMPTZ,
    updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (binding_key <> '')
);

CREATE TABLE IF NOT EXISTS admin_permission_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    actor_user_id BIGINT,
    target_user_id BIGINT,
    action TEXT NOT NULL,
    permission TEXT,
    old_value JSONB,
    new_value JSONB,
    request_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_admin_permission_audit_target_created ON admin_permission_audit_logs(target_user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS entitlement_tiers (
    tier TEXT PRIMARY KEY CHECK (tier IN ('standard','premium')),
    display_name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO entitlement_tiers (tier, display_name, enabled, version) VALUES
('standard','Standard',TRUE,1),
('premium','Premium',FALSE,1)
ON CONFLICT (tier) DO NOTHING;

CREATE TABLE IF NOT EXISTS entitlement_tier_groups (
    tier TEXT NOT NULL REFERENCES entitlement_tiers(tier) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    rate_multiplier DOUBLE PRECISION CHECK (rate_multiplier IS NULL OR rate_multiplier >= 0),
    source TEXT NOT NULL DEFAULT 'tier',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tier, group_id)
);

CREATE TABLE IF NOT EXISTS user_entitlements (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    tier TEXT NOT NULL REFERENCES entitlement_tiers(tier) ON DELETE RESTRICT,
    source TEXT NOT NULL CHECK (source IN ('default','tier_grant','independent_grant','manual','subscription','legacy')),
    expires_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_entitlement_grants (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tier TEXT NOT NULL REFERENCES entitlement_tiers(tier) ON DELETE RESTRICT,
    source TEXT NOT NULL CHECK (source IN ('independent_grant','manual','subscription','migration')),
    expires_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    granted_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, tier, source)
);

CREATE TABLE IF NOT EXISTS entitlement_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    actor_user_id BIGINT,
    target_user_id BIGINT NOT NULL,
    tier TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('preview','grant','revoke','tier_change','tier_config_change')),
    old_version BIGINT,
    new_version BIGINT,
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_entitlement_audit_target_created ON entitlement_audit_logs(target_user_id, created_at DESC);


INSERT INTO admin_permissions(permission, resource, action, sensitive, description)
VALUES ('checkin.settings.read', 'checkin', 'settings.read', false, 'Read check-in policies'),
       ('checkin.settings.update', 'checkin', 'settings.update', true, 'Update check-in policies')
ON CONFLICT (permission) DO NOTHING;

CREATE TABLE IF NOT EXISTS entitlement_change_requests(
 actor_user_id BIGINT NOT NULL REFERENCES users(id), request_id VARCHAR(128) NOT NULL,
 fingerprint CHAR(64) NOT NULL, result JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(actor_user_id,request_id)
);
