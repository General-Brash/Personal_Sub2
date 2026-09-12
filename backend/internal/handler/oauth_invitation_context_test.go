package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"strings"
	"testing"
	"time"
)

func TestOAuthInvitationContextBoundToStateSecretAndExpiry(t *testing.T) {
	secret := "a-test-only-long-secret-for-signing"
	now := time.Unix(1800000000, 0)
	original := signedOAuthInvitation{State: "state-A", Invitation: "pi_test", Affiliate: "AFF123", Expires: now.Add(time.Minute).Unix()}
	encoded, err := encodeOAuthInvitation(original, secret)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeOAuthInvitation(encoded, "state-A", secret, now)
	if err != nil || decoded.Invitation != original.Invitation {
		t.Fatalf("round trip: %v", err)
	}
	for _, test := range []struct {
		raw, state, key string
		at              time.Time
	}{{encoded, "state-B", secret, now}, {encoded, "state-A", "different-long-signing-secret", now}, {encoded, "state-A", secret, now.Add(time.Minute)}, {encoded + "tampered", "state-A", secret, now}} {
		if _, err := decodeOAuthInvitation(test.raw, test.state, test.key, test.at); err == nil {
			t.Fatal("unsafe OAuth invitation accepted")
		}
	}
}
func TestPendingOAuthInvitationEncryptedAndConflictRejected(t *testing.T) {
	h := &AuthHandler{cfg: &config.Config{JWT: config.JWTConfig{Secret: "a-test-only-long-secret-for-signing"}}}
	fixed := signedOAuthInvitation{Invitation: "pi_raw_secret_token", Affiliate: "AFF123"}
	encrypted, err := h.protectPendingInvitation(fixed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, fixed.Invitation) || strings.Contains(encrypted, fixed.Affiliate) {
		t.Fatal("raw credential leaked")
	}
	invite, affiliate := "", ""
	claims := map[string]any{oauthEncryptedInvitationClaim: encrypted}
	if err := h.mergePendingInvitationClaims(claims, &invite, &affiliate); err != nil || invite != fixed.Invitation || affiliate != fixed.Affiliate {
		t.Fatalf("protected pending context: %v", err)
	}
	invite = "pi_attacker"
	if err := h.mergePendingInvitationClaims(claims, &invite, &affiliate); err == nil {
		t.Fatal("client silently replaced inviter")
	}

	legacyInvite, legacyAffiliate := "", ""
	if err := h.mergePendingInvitationClaims(map[string]any{legacyPendingOAuthAffiliateClaim: fixed.Affiliate}, &legacyInvite, &legacyAffiliate); err != nil {
		t.Fatalf("legacy plaintext claim: %v", err)
	}
	if legacyAffiliate != "" {
		t.Fatal("legacy plaintext affiliate claim was merged")
	}
}
