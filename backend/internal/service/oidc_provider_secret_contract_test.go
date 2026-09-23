package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type oidcSecretContractRepository struct {
	OIDCProviderRepository
	created      OIDCClientCreateInput
	authDigest   string
	client       *OIDCClientRecord
	rotatedAt    time.Time
	rotatedUntil time.Time
	overlapUntil time.Time
}

func (r *oidcSecretContractRepository) CreateClient(_ context.Context, input OIDCClientCreateInput) (*OIDCClientRecord, error) {
	r.created = input
	r.client = &OIDCClientRecord{
		ID:            42,
		ClientID:      input.ClientID,
		ClientType:    OIDCClientTypeConfidential,
		Enabled:       true,
		RedirectURIs:  append([]string(nil), input.RedirectURIs...),
		AllowedScopes: append([]string(nil), input.AllowedScopes...),
	}
	return r.client, nil
}

func (r *oidcSecretContractRepository) AuthenticateClient(_ context.Context, clientID, digest string, _ time.Time) (*OIDCClientRecord, error) {
	r.authDigest = digest
	if r.client == nil || r.client.ClientID != clientID {
		return nil, ErrOIDCInvalidClient
	}
	return r.client, nil
}

func (r *oidcSecretContractRepository) CreateClientSecretWithOverlap(_ context.Context, _, _ int64, _, _ string, at, until, overlap time.Time, _ string) (*OIDCClientSecretRecord, error) {
	r.rotatedAt, r.rotatedUntil, r.overlapUntil = at, until, overlap
	return &OIDCClientSecretRecord{NotBefore: at, ExpiresAt: until}, nil
}

type oidcSecretContractUserRepository struct{ UserRepository }

func TestOIDCClientSecretDigestMatchesAdminCreationAndAuthentication(t *testing.T) {
	const pepper = "runtime-secret-pepper-abcdefghijklmnopqrstuvwxyz"
	const encryptionKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

	cfg := &config.Config{OIDCProvider: config.OIDCProviderConfig{
		Enabled:                       true,
		EncryptionKey:                 encryptionKey,
		SecretPepper:                  pepper,
		AllowedScopes:                 []string{OIDCScopeOpenID, OIDCScopeEmail},
		ClientSecretMaxOverlapSeconds: 3600,
		ClientSecretTTLSeconds:        90 * 86400,
	}}
	repo := &oidcSecretContractRepository{}
	svc := NewOIDCProviderService(repo, &oidcSecretContractUserRepository{}, nil, nil, cfg, nil)

	client, secret, err := svc.AdminCreateClient(context.Background(), OIDCClientCreateInput{
		Name:          "contract client",
		Owner:         "contract test",
		RedirectURIs:  []string{"https://client.example/callback"},
		AllowedScopes: []string{OIDCScopeOpenID, OIDCScopeEmail},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := repo.created.SecretExpiresAt.Sub(repo.created.SecretNotBefore); got != 90*24*time.Hour {
		t.Fatalf("created lifetime = %v, want 90 days", got)
	}
	record, _, err := svc.AdminRotateSecret(context.Background(), 42, 7, "test rotation")
	if err != nil {
		t.Fatal(err)
	}
	if got := record.ExpiresAt.Sub(repo.rotatedAt); got != 90*24*time.Hour {
		t.Fatalf("rotated lifetime = %v", got)
	}
	if got := repo.overlapUntil.Sub(repo.rotatedAt); got != time.Hour {
		t.Fatalf("overlap = %v", got)
	}
	mac := hmac.New(sha256.New, []byte(pepper))
	_, _ = mac.Write([]byte(secret))
	wantDigest := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if repo.created.SecretDigest != wantDigest {
		t.Fatal("admin-created secret digest does not match manual HMAC digest")
	}

	if _, err := svc.authenticateClient(context.Background(), client.ClientID, secret); err != nil {
		t.Fatal(err)
	}
	if repo.authDigest != wantDigest {
		t.Fatal("AuthenticateClient digest does not match manual HMAC digest")
	}
}

type oidcExpiredSecretRepository struct{ OIDCProviderRepository }

func (oidcExpiredSecretRepository) AuthenticateClient(_ context.Context, _, _ string, _ time.Time) (*OIDCClientRecord, error) {
	return nil, sql.ErrNoRows
}

func TestOIDCExpiredSecretAuthenticationMapsToInvalidClient(t *testing.T) {
	svc := &OIDCProviderService{repo: oidcExpiredSecretRepository{}, cfg: &config.Config{OIDCProvider: config.OIDCProviderConfig{SecretPepper: "test-pepper"}}}
	_, err := svc.authenticateClient(context.Background(), "client-id", "unusable-test-value")
	if !errors.Is(err, ErrOIDCInvalidClient) {
		t.Fatalf("expired secret error = %v, want invalid_client", err)
	}
}
