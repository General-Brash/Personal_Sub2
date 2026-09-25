package migrations

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var adminPermissionMigrationInListPattern = regexp.MustCompile(`(?i)WHERE permission IN \(([^)]*)\)`)

// adminPermissionMigrationSQL returns the executed SQL of a migration with
// full-line comments dropped and whitespace collapsed.
func adminPermissionMigrationSQL(t *testing.T, name string) string {
	t.Helper()
	content, err := fs.ReadFile(FS, name)
	require.NoError(t, err)
	var lines []string
	for _, line := range strings.Split(string(content), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}

func adminPermissionMigrationInList(t *testing.T, sql string) []string {
	t.Helper()
	match := adminPermissionMigrationInListPattern.FindStringSubmatch(sql)
	require.Len(t, match, 2, "missing WHERE permission IN (...)")
	var permissions []string
	for _, item := range strings.Split(match[1], ",") {
		permissions = append(permissions, strings.Trim(strings.TrimSpace(item), "'"))
	}
	return permissions
}

func TestAdminPermissionSensitiveFixMigrationOnlyMarksListedPermissions(t *testing.T) {
	sql := adminPermissionMigrationSQL(t, "245_admin_permission_sensitive_fix.sql")

	// A single UPDATE whose SET list is only sensitive = TRUE.
	require.Regexp(t, `(?i)^UPDATE admin_permissions SET sensitive = TRUE WHERE permission IN \([^)]*\);$`, sql)
	require.NotContains(t, strings.ToUpper(sql), "INSERT")
	require.NotContains(t, strings.ToUpper(sql), "DELETE")
	require.ElementsMatch(t, []string{
		"users.update", "audit.read", "bank.ledger.read", "mall.orders.read",
		"channels.credentials.read", "accounts.credentials.read",
		"payment.credentials.read", "users.credentials.read",
	}, adminPermissionMigrationInList(t, sql))
}

func TestAdminPermissionRetireUnusedMigrationOnlyDeletesDeadPermissions(t *testing.T) {
	sql := adminPermissionMigrationSQL(t, "246_admin_permission_retire_unused.sql")

	require.Regexp(t, `(?i)^DELETE FROM admin_permissions WHERE permission IN \([^)]*\);$`, sql)
	require.NotContains(t, strings.ToUpper(sql), "INSERT")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE")
	require.ElementsMatch(t, []string{
		"affiliates.quota.adjust", "affiliates.rebate.replay",
		"bank.settlement.retry", "invites.read", "models.pricing.manage",
	}, adminPermissionMigrationInList(t, sql))

	// Existing grants go away through the FK cascade declared in 238.
	grants, err := fs.ReadFile(FS, "238_admin_permissions_entitlements.sql")
	require.NoError(t, err)
	require.Contains(t, string(grants), "REFERENCES admin_permissions(permission) ON DELETE CASCADE")
}
