package service

import (
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestOIDCProviderRoleMappingIsExhaustive(t *testing.T) {
	cases := map[string]string{RoleUser: "user", RoleAdmin: "admin", RoleSuperAdmin: "superadmin"}
	for input, want := range cases {
		got, err := MapOIDCRole(input)
		if err != nil || got != want {
			t.Fatalf("MapOIDCRole(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := MapOIDCRole("owner"); err == nil {
		t.Fatal("unknown role must fail closed")
	}
}

func TestOIDCProviderPromptRejectsNoneCombination(t *testing.T) {
	if _, err := normalizeOIDCPrompt("none login"); err == nil {
		t.Fatal("prompt=none combination must fail")
	}
	got, err := normalizeOIDCPrompt("login consent")
	if err != nil || got != "consent login" {
		t.Fatalf("normalized prompt = %q, %v", got, err)
	}
}

func TestOIDCProviderScopeCanonicalizationRejectsDuplicates(t *testing.T) {
	if _, err := oidcCanonicalScope([]string{"openid", "openid"}); err == nil {
		t.Fatal("duplicate scopes must fail")
	}
	got, err := oidcCanonicalScope([]string{"email", "openid"})
	if err != nil || got != "email openid" {
		t.Fatalf("canonical scope = %q, %v", got, err)
	}
}

func TestOIDCProviderProtectorBindsPurpose(t *testing.T) {
	cfg := &config.Config{}
	cfg.OIDCProvider.EncryptionKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	protector, err := newOIDCProtector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, _, err := protector.seal("state-value", "state", "tx-1")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := protector.open(ciphertext, "state", "tx-1"); err != nil || got != "state-value" {
		t.Fatalf("open = %q, %v", got, err)
	}
	if _, err := protector.open(ciphertext, "nonce", "tx-1"); err == nil {
		t.Fatal("purpose mismatch must fail")
	}
}

func TestOIDCProviderDiscoveryUsesConfiguredScopes(t *testing.T) {
	cfg := &config.Config{}
	cfg.OIDCProvider.AllowedScopes = []string{OIDCScopeOpenID, OIDCScopeEmail}
	svc := &OIDCProviderService{cfg: cfg}
	metadata := svc.Discovery()
	scopes, ok := metadata["scopes_supported"].([]string)
	if !ok || len(scopes) != 2 || scopes[0] != OIDCScopeEmail || scopes[1] != OIDCScopeOpenID {
		t.Fatalf("scopes_supported = %#v, want configured scopes only", metadata["scopes_supported"])
	}
	claims, ok := metadata["claims_supported"].([]string)
	if !ok {
		t.Fatalf("claims_supported type = %T", metadata["claims_supported"])
	}
	for _, claim := range claims {
		if claim == "role" || claim == "preferred_username" {
			t.Fatalf("claim %q advertised without its configured scope: %#v", claim, claims)
		}
	}
}

func TestOIDCProviderAllowedScopesRejectDuplicates(t *testing.T) {
	cfg := &config.Config{}
	cfg.OIDCProvider.AllowedScopes = []string{OIDCScopeOpenID, OIDCScopeEmail}
	svc := &OIDCProviderService{cfg: cfg}
	if _, err := svc.validateAllowedScopes([]string{OIDCScopeEmail, OIDCScopeEmail}); err == nil {
		t.Fatal("duplicate admin scopes must fail closed")
	}
}

func TestOIDCAdminCSRFTokenBindsSubjectSessionAndEpoch(t *testing.T) {
	secret := "test-secret-pepper-with-at-least-32-bytes"
	base := OIDCAdminCSRFToken(secret, 7, "session-a", 3)
	if base == "" {
		t.Fatal("bound admin CSRF token must be derived")
	}
	for name, got := range map[string]string{
		"different subject": OIDCAdminCSRFToken(secret, 8, "session-a", 3),
		"different session": OIDCAdminCSRFToken(secret, 7, "session-b", 3),
		"different epoch":   OIDCAdminCSRFToken(secret, 7, "session-a", 4),
	} {
		if got == base {
			t.Fatalf("%s reused the current admin CSRF token", name)
		}
	}
	if got := OIDCAdminCSRFToken(secret, 7, "", 3); got != "" {
		t.Fatalf("missing session must fail closed, got %q", got)
	}
}

func TestOIDCProviderTrustedSkipCannotBypassPromptConsent(t *testing.T) {
	if trustedOIDCConsentSkipAllowed("consent", true) {
		t.Fatal("prompt=consent must require interactive consent even for trusted clients")
	}
	if !trustedOIDCConsentSkipAllowed("", true) {
		t.Fatal("trusted client without prompt=consent should retain its server-side preauthorization path")
	}
	if trustedOIDCConsentSkipAllowed("", false) {
		t.Fatal("untrusted client must not skip consent")
	}
}

func TestOIDCProviderRevocationRejectsEmptyToken(t *testing.T) {
	if err := (&OIDCProviderService{}).Revoke(nil, "client", "secret", "   "); !errors.Is(err, ErrOIDCInvalidRequest) {
		t.Fatalf("empty revocation token error = %v, want ErrOIDCInvalidRequest", err)
	}
	if validOIDCRevocationToken("") || validOIDCRevocationToken("\t") {
		t.Fatal("blank revocation token must be invalid")
	}
	if !validOIDCRevocationToken("opaque-token") {
		t.Fatal("non-empty revocation token must be accepted for protocol lookup")
	}
}

func TestOIDCProviderActiveSigningKeyValidationFailsClosed(t *testing.T) {
	now := time.Now().UTC()
	valid := &OIDCSigningKeyRecord{KID: "kid-1", Alg: OIDCSigningRS256, Status: "active", PrivateKeyCiphertext: "ciphertext", NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Minute)}
	if !validOIDCActiveSigningKey(valid, now) {
		t.Fatal("current active signing key should be accepted")
	}
	for name, key := range map[string]*OIDCSigningKeyRecord{
		"nil":                 nil,
		"pending":             {KID: "kid-1", Alg: OIDCSigningRS256, Status: "pending", PrivateKeyCiphertext: "ciphertext", NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Minute)},
		"expired":             {KID: "kid-1", Alg: OIDCSigningRS256, Status: "active", PrivateKeyCiphertext: "ciphertext", NotBefore: now.Add(-2 * time.Minute), NotAfter: now.Add(-time.Minute)},
		"no private material": {KID: "kid-1", Alg: OIDCSigningRS256, Status: "active", NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Minute)},
	} {
		if validOIDCActiveSigningKey(key, now) {
			t.Fatalf("%s signing key must fail closed", name)
		}
	}
}
