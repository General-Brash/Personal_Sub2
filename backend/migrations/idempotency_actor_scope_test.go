package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

func TestIdempotencyActorScopeMigrationIsEmbeddedAndIsolatesLegacyRecords(t *testing.T) {
	contents, err := fs.ReadFile(FS, "176_idempotency_actor_scope.sql")
	if err != nil {
		t.Fatalf("read actor-scoped idempotency migration: %v", err)
	}

	sql := string(contents)
	for _, required := range []string{
		"ADD COLUMN IF NOT EXISTS operation_scope",
		"ADD COLUMN IF NOT EXISTS actor_scope",
		"SET actor_scope = 'legacy:' || id::text",
		"DROP COLUMN IF EXISTS scope",
		"(operation_scope, actor_scope, idempotency_key_hash)",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration must contain %q", required)
		}
	}
}
