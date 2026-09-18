package service

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type OIDCSigningService struct {
	repo      OIDCProviderRepository
	cfg       *config.Config
	protector *oidcProtector
}

func NewOIDCSigningService(repo OIDCProviderRepository, cfg *config.Config) *OIDCSigningService {
	var protector *oidcProtector
	if cfg != nil && cfg.OIDCProvider.EncryptionKey != "" {
		protector, _ = newOIDCProtector(cfg)
	}
	return &OIDCSigningService{repo: repo, cfg: cfg, protector: protector}
}

func (s *OIDCSigningService) GenerateAndActivate(ctx context.Context, actorID int64, reason string) (*OIDCSigningKeyRecord, error) {
	if s == nil || s.repo == nil || s.cfg == nil || !s.cfg.OIDCProvider.Enabled || s.protector == nil {
		return nil, ErrOIDCKeyUnavailable
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return nil, err
	}
	publicJWK, err := publicJWKJSON(&privateKey.PublicKey, "")
	if err != nil {
		return nil, err
	}
	fingerprint := oidcFingerprint(publicJWK)
	kid := "sub2-" + fingerprint[:24]
	publicJWK, err = publicJWKJSON(&privateKey.PublicKey, kid)
	if err != nil {
		return nil, err
	}
	der := x509.MarshalPKCS1PrivateKey(privateKey)
	ciphertext, _, err := s.protector.seal(string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})), "private-key", kid)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	record := OIDCSigningKeyCreateInput{KID: kid, Alg: OIDCSigningRS256, PublicJWK: publicJWK, PrivateKeyCiphertext: ciphertext, Fingerprint: fingerprint, Status: "pending", NotBefore: now, NotAfter: now.Add(365 * 24 * time.Hour), CreatedBy: actorID, ChangeReason: reason}
	if err := s.repo.CreateSigningKey(ctx, record); err != nil {
		return nil, err
	}
	if err := s.repo.SetSigningKeyStatus(ctx, kid, "active", actorID, reason); err != nil {
		return nil, err
	}
	return &OIDCSigningKeyRecord{KID: kid, Alg: OIDCSigningRS256, PublicJWK: publicJWK, PrivateKeyCiphertext: ciphertext, Fingerprint: fingerprint, Status: "active", NotBefore: now, NotAfter: record.NotAfter, CreatedAt: now}, nil
}

func (s *OIDCSigningService) loadActive(ctx context.Context) (*OIDCSigningKeyMaterial, error) {
	if s == nil || s.repo == nil || s.protector == nil {
		return nil, ErrOIDCKeyUnavailable
	}
	now := time.Now().UTC()
	record, err := s.repo.GetActiveSigningKey(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("load active oidc signing key: %w", err)
	}
	if !validOIDCActiveSigningKey(record, now) {
		return nil, ErrOIDCKeyUnavailable
	}
	plain, err := s.protector.open(record.PrivateKeyCiphertext, "private-key", record.KID)
	if err != nil {
		return nil, fmt.Errorf("decrypt oidc signing key: %w", err)
	}
	block, _ := pem.Decode([]byte(plain))
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return nil, ErrOIDCKeyUnavailable
	}
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil || privateKey == nil || privateKey.N.BitLen() < 2048 || privateKey.Validate() != nil {
		return nil, ErrOIDCKeyUnavailable
	}
	if err := validateOIDCKeyPair(*record, privateKey); err != nil {
		return nil, err
	}
	return &OIDCSigningKeyMaterial{Record: *record, PrivateKey: privateKey}, nil
}

func (s *OIDCSigningService) SignIDToken(ctx context.Context, clientID string, subject OIDCClaimsSubject, nonce, scope string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(subject.Subject) == "" || strings.TrimSpace(nonce) == "" || ttl <= 0 || subject.AuthTime.IsZero() {
		return "", ErrOIDCInvalidRequest
	}
	material, err := s.loadActive(ctx)
	if err != nil {
		return "", err
	}
	role, err := MapOIDCRole(subject.Role)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	claims := map[string]any{
		"iss":       OIDCProviderIssuer,
		"sub":       subject.Subject,
		"aud":       clientID,
		"iat":       now.Unix(),
		"exp":       now.Add(ttl).Unix(),
		"nonce":     nonce,
		"auth_time": subject.AuthTime.Unix(),
	}
	scopes := scopeSet(scope)
	if _, ok := scopes[OIDCScopeProfile]; ok && strings.TrimSpace(subject.Username) != "" {
		claims["preferred_username"] = subject.Username
	}
	if _, ok := scopes[OIDCScopeEmail]; ok && strings.TrimSpace(subject.Email) != "" {
		claims["email"] = subject.Email
	}
	if _, ok := scopes[OIDCScopeRoles]; ok && role != "" {
		claims["role"] = role
	}
	return signRS256(material.Record.KID, material.PrivateKey, claims)
}

func (s *OIDCSigningService) PublicJWKS(ctx context.Context) ([]map[string]any, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOIDCKeyUnavailable
	}
	keys, err := s.repo.ListSigningKeys(ctx, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, ErrOIDCKeyUnavailable
	}
	result := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		public, err := parseOIDCPublicJWK(key.PublicJWK)
		if err != nil || public.Kid != key.KID || public.Alg != OIDCSigningRS256 || public.Kty != "RSA" || public.Use != "sig" || public.N == "" || public.E == "" {
			return nil, ErrOIDCKeyUnavailable
		}
		result = append(result, map[string]any{"kty": public.Kty, "kid": public.Kid, "use": public.Use, "alg": public.Alg, "n": public.N, "e": public.E})
	}
	return result, nil
}

type oidcPublicJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func parseOIDCPublicJWK(raw string) (*oidcPublicJWK, error) {
	var public oidcPublicJWK
	if err := json.Unmarshal([]byte(raw), &public); err != nil {
		return nil, err
	}
	return &public, nil
}

func validateOIDCKeyPair(record OIDCSigningKeyRecord, privateKey *rsa.PrivateKey) error {
	public, err := parseOIDCPublicJWK(record.PublicJWK)
	if err != nil || public.Kid != record.KID || public.Alg != OIDCSigningRS256 || public.Kty != "RSA" || public.Use != "sig" || public.N == "" || public.E == "" {
		return ErrOIDCKeyUnavailable
	}
	withKid, err := publicJWKJSON(&privateKey.PublicKey, record.KID)
	if err != nil {
		return ErrOIDCKeyUnavailable
	}
	computed, err := parseOIDCPublicJWK(withKid)
	if err != nil || *computed != *public {
		return ErrOIDCKeyUnavailable
	}
	withoutKid, err := publicJWKJSON(&privateKey.PublicKey, "")
	if err != nil || oidcFingerprint(withoutKid) != record.Fingerprint {
		return ErrOIDCKeyUnavailable
	}
	return nil
}

func publicJWKJSON(key *rsa.PublicKey, kid string) (string, error) {
	data, err := json.Marshal(map[string]string{"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())})
	return string(data), err
}

func signRS256(kid string, privateKey *rsa.PrivateKey, claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	input := encodedHeader + "." + encodedPayload
	hash := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func validOIDCActiveSigningKey(record *OIDCSigningKeyRecord, now time.Time) bool {
	return record != nil && record.Status == "active" && record.Alg == OIDCSigningRS256 && !now.Before(record.NotBefore) && now.Before(record.NotAfter) && record.KID != "" && record.PrivateKeyCiphertext != ""
}

func (s *OIDCSigningService) KeyMetadata(ctx context.Context) ([]OIDCSigningKeyRecord, error) {
	if s == nil || s.repo == nil {
		return nil, ErrOIDCKeyUnavailable
	}
	return s.repo.ListAllSigningKeys(ctx)
}

func (s *OIDCSigningService) VerifyConfig() error {
	if s == nil || s.cfg == nil || !s.cfg.OIDCProvider.Enabled {
		return ErrOIDCProviderDisabled
	}
	if s.cfg.OIDCProvider.SigningAlg != OIDCSigningRS256 {
		return fmt.Errorf("unsupported signing algorithm %s", s.cfg.OIDCProvider.SigningAlg)
	}
	return nil
}
