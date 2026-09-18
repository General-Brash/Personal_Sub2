package repository

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The OIDC tables intentionally use raw SQL for transaction locks and partial
// indexes. This static gate documents that raw SQL is authoritative and guards
// the custom Ent schema/migration parity without running go generate or a DB.
func TestOIDCRawSQLSchemaParityContract(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	migration242, err := os.ReadFile(filepath.Join(root, "migrations", "242_oidc_provider_foundation.sql"))
	if err != nil {
		t.Fatal(err)
	}
	migration243, err := os.ReadFile(filepath.Join(root, "migrations", "243_oidc_provider_second_round.sql"))
	if err != nil {
		t.Fatal(err)
	}
	migration244, err := os.ReadFile(filepath.Join(root, "migrations", "244_oidc_consent_binding_and_revocation.sql"))
	if err != nil {
		t.Fatal(err)
	}
	combined := string(migration242) + "\n" + string(migration243) + "\n" + string(migration244)
	clientSchema, err := os.ReadFile(filepath.Join(root, "ent", "schema", "oidc_client.go"))
	if err != nil {
		t.Fatal(err)
	}
	sessionSchema, err := os.ReadFile(filepath.Join(root, "ent", "schema", "oidc_session.go"))
	if err != nil {
		t.Fatal(err)
	}
	tokenSchema, err := os.ReadFile(filepath.Join(root, "ent", "schema", "oidc_token.go"))
	if err != nil {
		t.Fatal(err)
	}
	schemaText := string(clientSchema) + "\n" + string(sessionSchema) + "\n" + string(tokenSchema)
	for _, fragment := range []string{
		"consent_id BIGINT",
		"CREATE UNIQUE INDEX oidc_consents_active_unique_idx",
		"WHERE status = 'active'",
		"created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"disabled_at TIMESTAMPTZ",
		"revoked_by BIGINT REFERENCES users(id)",
		"replay_fingerprint VARCHAR(64)",
		"last_change_reason TEXT NOT NULL DEFAULT ''",
		"ADD COLUMN IF NOT EXISTS consent_id BIGINT",
		"ALTER COLUMN consent_id SET NOT NULL",
		"oidc_authorization_codes_consent_fk",
		"oidc_access_tokens_consent_fk",
		"oidc_refresh_token_families_consent_idx",
		"status NOT IN ('consented', 'code_issued') OR consent_id IS NOT NULL",
	} {
		if !strings.Contains(combined, fragment) {
			t.Errorf("OIDC raw SQL parity fragment missing: %q", fragment)
		}
	}
	for _, fragment := range []string{
		"field.String(\"last_change_reason\")",
		"field.Int64(\"consent_id\").Optional().Nillable()",
		"field.Int64(\"consent_id\")",
		"field.Int64(\"revoked_by\").Optional().Nillable()",
		"IndexWhere(\"status = 'active'\")",
	} {
		if !strings.Contains(schemaText, fragment) {
			t.Errorf("OIDC Ent schema parity fragment missing: %q", fragment)
		}
	}
}
