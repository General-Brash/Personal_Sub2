package repository

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOIDCConsentBindingAndAtomicRevocationContract(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(data)
	}
	migration := read("migrations/244_oidc_consent_binding_and_revocation.sql")
	authorization := read("internal/repository/oidc_authorization_repo.go")
	tokens := read("internal/repository/oidc_token_repo.go")
	admin := read("internal/repository/oidc_admin_repo.go")
	service := read("internal/service/oidc_provider_service.go")

	for _, fragment := range []string{
		"ADD COLUMN IF NOT EXISTS consent_id BIGINT",
		"ALTER COLUMN consent_id SET NOT NULL",
		"oidc_authorization_codes_consent_fk",
		"oidc_access_tokens_consent_fk",
		"oidc_authorization_transactions_consent_state_ck",
	} {
		if !strings.Contains(migration, fragment) {
			t.Errorf("consent migration fragment missing: %q", fragment)
		}
	}
	for _, fragment := range []string{
		"INSERT INTO oidc_authorization_codes(code_digest,transaction_id,client_pk,user_id,consent_id",
		"WHERE c.id=$1 AND c.user_id=t.user_id AND c.client_pk=t.client_pk",
		"c.status='active'",
		"RETURNING t.consent_id",
	} {
		if !strings.Contains(authorization, fragment) {
			t.Errorf("authorization consent binding fragment missing: %q", fragment)
		}
	}
	for _, fragment := range []string{
		"if input.ConsentID <= 0",
		"WHERE c.id=$1 FOR UPDATE",
		`consentStatus != "active"`,
		"cl.policy_version",
		"INSERT INTO oidc_access_tokens(token_digest,client_pk,user_id,consent_id",
		"f.consent_id",
	} {
		if !strings.Contains(tokens, fragment) {
			t.Errorf("token consent binding/rotation fragment missing: %q", fragment)
		}
	}
	for _, fragment := range []string{
		"tx, err := r.db.BeginTx(ctx, nil)",
		"UPDATE oidc_consents SET status='revoked'",
		"UPDATE oidc_access_tokens SET revoked_at",
		"UPDATE oidc_refresh_token_families SET status='revoked'",
		"UPDATE oidc_refresh_tokens SET status='revoked'",
		"return tx.Commit()",
	} {
		if !strings.Contains(admin, fragment) {
			t.Errorf("atomic consent revocation fragment missing: %q", fragment)
		}
	}
	for _, fragment := range []string{
		"transaction.ConsentID == nil",
		"ConsentID: codeRecord.ConsentID",
	} {
		if !strings.Contains(service, fragment) {
			t.Errorf("service consent propagation fragment missing: %q", fragment)
		}
	}
}

func TestOIDCSessionVersionAndRevocationFailClosedContract(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	authorization, err := os.ReadFile(filepath.Join(root, "internal", "repository", "oidc_authorization_repo.go"))
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := os.ReadFile(filepath.Join(root, "internal", "repository", "oidc_token_repo.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"session_version)",
		"session_version>0",
		"&out.SessionVersion",
	} {
		if !strings.Contains(string(authorization), fragment) {
			t.Errorf("browser session version contract fragment missing: %q", fragment)
		}
	}
	for _, fragment := range []string{
		"if err != nil && !errors.Is(err, sql.ErrNoRows)",
		"return tx.Commit()",
	} {
		if !strings.Contains(string(tokens), fragment) {
			t.Errorf("revocation fail-closed contract fragment missing: %q", fragment)
		}
	}
}
