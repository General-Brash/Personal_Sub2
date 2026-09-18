package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type oidcSecretContractRepository struct {
	OIDCProviderRepository
	created    OIDCClientCreateInput
	authDigest string
	client     *OIDCClientRecord
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
	}}
	repo := &oidcSecretContractRepository{}
	svc := NewOIDCProviderService(repo, &oidcSecretContractUserRepository{}, nil, nil, cfg)

	client, secret, err := svc.AdminCreateClient(context.Background(), OIDCClientCreateInput{
		Name:          "contract client",
		Owner:         "contract test",
		RedirectURIs:  []string{"https://client.example/callback"},
		AllowedScopes: []string{OIDCScopeOpenID, OIDCScopeEmail},
	})
	if err != nil {
		t.Fatal(err)
	}

	mac := hmac.New(sha256.New, []byte(pepper))
	_, _ = mac.Write([]byte(secret))
	wantDigest := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if repo.created.SecretDigest != wantDigest {
		t.Fatalf("admin-created secret digest = %q, want manual HMAC digest %q", repo.created.SecretDigest, wantDigest)
	}

	if _, err := svc.authenticateClient(context.Background(), client.ClientID, secret); err != nil {
		t.Fatal(err)
	}
	if repo.authDigest != wantDigest {
		t.Fatalf("AuthenticateClient digest = %q, want %q", repo.authDigest, wantDigest)
	}
}
