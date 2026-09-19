package service

import (
	"crypto/sha256"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestOIDCChatInteropAuditPKCEUsesRFC7636S256(t *testing.T) {
	verifier := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	if !validOIDCPKCEVerifier(verifier) {
		t.Fatal("a valid RFC 7636 verifier was rejected")
	}
	if !validOIDCPKCEChallenge(challenge) {
		t.Fatal("a valid S256 challenge was rejected")
	}
	for name, value := range map[string]string{
		"verifier too short":             strings.Repeat("a", 42),
		"verifier has space":             strings.Repeat("a", 42) + " ",
		"verifier has padding":           strings.Repeat("a", 42) + "=",
		"challenge has padding":          challenge + "=",
		"challenge has invalid alphabet": strings.Repeat("A", 42) + "!",
	} {
		t.Run(name, func(t *testing.T) {
			if strings.HasPrefix(name, "challenge") {
				if validOIDCPKCEChallenge(value) {
					t.Fatalf("invalid challenge %q was accepted", value)
				}
				return
			}
			if validOIDCPKCEVerifier(value) {
				t.Fatalf("invalid verifier %q was accepted", value)
			}
		})
	}
}

func TestOIDCChatInteropAuditDiscoveryAndClaimsAreProviderSafe(t *testing.T) {
	cfg := &config.Config{}
	cfg.OIDCProvider.AllowedScopes = []string{OIDCScopeOpenID, OIDCScopeProfile, OIDCScopeEmail, OIDCScopeRoles, OIDCScopeOfflineAccess}
	metadata := (&OIDCProviderService{cfg: cfg}).Discovery()

	for key, want := range map[string]string{
		"issuer":                 OIDCProviderIssuer,
		"authorization_endpoint": OIDCProviderIssuer + "/oauth/authorize",
		"token_endpoint":         OIDCProviderIssuer + "/oauth/token",
		"userinfo_endpoint":      OIDCProviderIssuer + "/oauth/userinfo",
		"jwks_uri":               OIDCProviderIssuer + "/oauth/jwks",
		"revocation_endpoint":    OIDCProviderIssuer + "/oauth/revoke",
	} {
		if got, ok := metadata[key].(string); !ok || got != want {
			t.Fatalf("metadata[%q] = %#v, want %q", key, metadata[key], want)
		}
	}
	if got := metadata["id_token_signing_alg_values_supported"]; !reflect.DeepEqual(got, []string{OIDCSigningRS256}) {
		t.Fatalf("signing algorithms = %#v, want RS256 only", got)
	}
	if got := metadata["code_challenge_methods_supported"]; !reflect.DeepEqual(got, []string{OIDCCodeChallengeS256}) {
		t.Fatalf("PKCE methods = %#v, want S256 only", got)
	}
	if got := metadata["token_endpoint_auth_methods_supported"]; !reflect.DeepEqual(got, []string{OIDCClientAuthBasic}) {
		t.Fatalf("token auth methods = %#v, want client_secret_basic only", got)
	}
	if got := metadata["revocation_endpoint_auth_methods_supported"]; !reflect.DeepEqual(got, []string{OIDCClientAuthBasic}) {
		t.Fatalf("revocation auth methods = %#v, want client_secret_basic only", got)
	}

	claims, err := UserInfoClaims(OIDCClaimsSubject{
		Subject:  "subject-1",
		Email:    "user@example.test",
		Username: "first-party-user",
		Role:     RoleSuperAdmin,
	}, "openid profile email roles")
	if err != nil {
		t.Fatal(err)
	}
	wantClaims := map[string]any{
		"sub":                "subject-1",
		"preferred_username": "first-party-user",
		"email":              "user@example.test",
		"role":               "superadmin",
	}
	if !reflect.DeepEqual(claims, wantClaims) {
		t.Fatalf("userinfo claims = %#v, want %#v", claims, wantClaims)
	}
	for _, forbidden := range []string{"groups", "balance", "quota", "subscription", "api_key", "model", "email_verified"} {
		if _, ok := claims[forbidden]; ok {
			t.Fatalf("userinfo leaked forbidden claim %q: %#v", forbidden, claims)
		}
	}
	if _, err := UserInfoClaims(OIDCClaimsSubject{Subject: "subject-1", Role: "owner"}, "openid roles"); err == nil {
		t.Fatal("unknown roles must fail closed")
	}
}
