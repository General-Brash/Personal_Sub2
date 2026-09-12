package service

// Permissions used by the explicit administrative route inventory.
func init() {
	knownAdminPermissions["pages.read"] = struct{}{}
	knownAdminPermissions["affiliates.manage"] = struct{}{}
	knownAdminPermissions["announcements.manage"] = struct{}{}
	knownAdminPermissions["announcements.read"] = struct{}{}
	knownAdminPermissions["audit.manage"] = struct{}{}
	knownAdminPermissions["compliance.manage"] = struct{}{}
	knownAdminPermissions["compliance.read"] = struct{}{}
	knownAdminPermissions["payment.credentials.read"] = struct{}{}
	knownAdminPermissions["payment.credentials.write"] = struct{}{}
	knownAdminPermissions["plugins.read"] = struct{}{}
	knownAdminPermissions["subscriptions.manage"] = struct{}{}
	knownAdminPermissions["subscriptions.read"] = struct{}{}
	knownAdminPermissions["users.credentials.read"] = struct{}{}
	knownAdminPermissions["users.credentials.write"] = struct{}{}
}
