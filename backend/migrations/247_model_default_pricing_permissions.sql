-- No price changes or automatic grants: display editors do not gain billing-write access.
INSERT INTO admin_permissions(permission,resource,action,sensitive,description) VALUES
 ('models.pricing.read','models','pricing.read',false,'查看全局默认计费标准 / Read global default pricing'),
 ('models.pricing.write','models','pricing.write',true,'修改全局默认计费标准 / Write global default pricing')
ON CONFLICT(permission) DO NOTHING;
