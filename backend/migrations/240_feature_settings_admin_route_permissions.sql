-- New feature activation defaults and the expanded administrative catalog.
-- No users or administrator identities are promoted, and no history is replayed.
INSERT INTO settings(key, value, updated_at) VALUES
 ('player_invitations_enabled', 'false', NOW()),
 ('model_plaza_v2_enabled', 'false', NOW())
ON CONFLICT(key) DO NOTHING;

INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('accounts.catalog.read','accounts','catalog.read',false,'accounts.catalog.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('accounts.catalog.write','accounts','catalog.write',true,'accounts.catalog.write') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('accounts.credentials.read','accounts','credentials.read',false,'accounts.credentials.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('accounts.credentials.write','accounts','credentials.write',true,'accounts.credentials.write') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('affiliates.manage','affiliates','manage',true,'affiliates.manage') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('affiliates.quota.adjust','affiliates','quota.adjust',true,'affiliates.quota.adjust') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('affiliates.read','affiliates','read',false,'affiliates.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('affiliates.rebate.replay','affiliates','rebate.replay',true,'affiliates.rebate.replay') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('affiliates.relationship.create','affiliates','relationship.create',true,'affiliates.relationship.create') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('announcements.manage','announcements','manage',true,'announcements.manage') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('announcements.read','announcements','read',false,'announcements.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('audit.export','audit','export',true,'audit.export') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('audit.manage','audit','manage',true,'audit.manage') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('audit.read','audit','read',false,'audit.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('bank.ledger.read','bank','ledger.read',false,'bank.ledger.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('bank.settings.read','bank','settings.read',false,'bank.settings.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('bank.settings.update','bank','settings.update',true,'bank.settings.update') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('bank.settlement.retry','bank','settlement.retry',true,'bank.settlement.retry') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('channels.catalog.read','channels','catalog.read',false,'channels.catalog.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('channels.catalog.write','channels','catalog.write',true,'channels.catalog.write') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('channels.credentials.read','channels','credentials.read',false,'channels.credentials.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('channels.credentials.write','channels','credentials.write',true,'channels.credentials.write') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('checkin.settings.read','checkin','settings.read',false,'checkin.settings.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('checkin.settings.update','checkin','settings.update',true,'checkin.settings.update') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('compliance.manage','compliance','manage',true,'compliance.manage') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('compliance.read','compliance','read',false,'compliance.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('groups.create','groups','create',true,'groups.create') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('groups.delete','groups','delete',true,'groups.delete') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('groups.dynamic_rates.manage','groups','dynamic_rates.manage',true,'groups.dynamic_rates.manage') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('groups.rates.manage','groups','rates.manage',true,'groups.rates.manage') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('groups.read','groups','read',false,'groups.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('groups.update','groups','update',true,'groups.update') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('invites.quota.adjust','invites','quota.adjust',true,'invites.quota.adjust') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('invites.read','invites','read',false,'invites.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('mall.fulfill','mall','fulfill',true,'mall.fulfill') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('mall.orders.read','mall','orders.read',false,'mall.orders.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('mall.products.read','mall','products.read',false,'mall.products.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('mall.products.write','mall','products.write',true,'mall.products.write') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('mall.refund','mall','refund',true,'mall.refund') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('models.catalog.read','models','catalog.read',false,'models.catalog.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('models.catalog.write','models','catalog.write',true,'models.catalog.write') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('models.pricing.manage','models','pricing.manage',true,'models.pricing.manage') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('ops.manage','ops','manage',true,'ops.manage') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('ops.read','ops','read',false,'ops.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('payment.credentials.read','payment','credentials.read',false,'payment.credentials.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('payment.credentials.write','payment','credentials.write',true,'payment.credentials.write') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('plugins.execute','plugins','execute',true,'plugins.execute') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('plugins.read','plugins','read',false,'plugins.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('security.permissions.grant','security','permissions.grant',true,'security.permissions.grant') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('security.superadmin.assign','security','superadmin.assign',true,'security.superadmin.assign') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('subscriptions.manage','subscriptions','manage',true,'subscriptions.manage') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('subscriptions.read','subscriptions','read',false,'subscriptions.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('system.settings.manage','system','settings.manage',true,'system.settings.manage') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('users.balance.adjust','users','balance.adjust',true,'users.balance.adjust') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('users.credentials.read','users','credentials.read',false,'users.credentials.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('users.credentials.write','users','credentials.write',true,'users.credentials.write') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('users.delete','users','delete',true,'users.delete') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('users.entitlement.manage','users','entitlement.manage',true,'users.entitlement.manage') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('users.read','users','read',false,'users.read') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('users.role.assign','users','role.assign',true,'users.role.assign') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('users.status','users','status',true,'users.status') ON CONFLICT(permission) DO NOTHING;
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES ('users.update','users','update',true,'users.update') ON CONFLICT(permission) DO NOTHING;

ALTER TABLE temporary_credit_grants ADD COLUMN IF NOT EXISTS expiry_policy_version TEXT NOT NULL DEFAULT 'legacy';
INSERT INTO settings(key,value,updated_at) VALUES ('temporary_credit_source_expiry','{"checkin":"00:00","admin_grant":"00:00"}',NOW()) ON CONFLICT(key) DO NOTHING;

INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES('pages.read','pages','read',false,'Read administrator pages') ON CONFLICT(permission) DO NOTHING;
