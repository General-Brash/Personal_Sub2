package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type oidcProtector struct{ key []byte }

func newOIDCProtector(cfg *config.Config) (*oidcProtector, error) {
	key, err := hex.DecodeString(cfg.OIDCProvider.EncryptionKey)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("invalid oidc provider encryption key")
	}
	return &oidcProtector{key: key}, nil
}

func (p *oidcProtector) seal(plaintext, purpose, aad string) (string, string, error) {
	if p == nil || len(p.key) != 32 {
		return "", "", errors.New("oidc protector unavailable")
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}
	payload := gcm.Seal(nil, nonce, []byte(plaintext), []byte(purpose+"|"+aad))
	out := append(nonce, payload...)
	return base64.RawStdEncoding.EncodeToString(out), oidcFingerprint(plaintext), nil
}

func (p *oidcProtector) open(ciphertext, purpose, aad string) (string, error) {
	if p == nil || len(p.key) != 32 {
		return "", errors.New("oidc protector unavailable")
	}
	data, err := base64.RawStdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(p.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("oidc ciphertext too short")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(purpose+"|"+aad))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func oidcDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func oidcFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func oidcSecretDigest(pepper, secret string) string {
	mac := hmac.New(sha256.New, []byte(pepper))
	_, _ = mac.Write([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func oidcRandomToken(bytes int) (string, error) {
	if bytes < 32 {
		bytes = 32
	}
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func oidcCanonicalScope(scopes []string) (string, error) {
	seen := make(map[string]struct{}, len(scopes))
	ordered := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			return "", fmt.Errorf("duplicate scope %q", scope)
		}
		seen[scope] = struct{}{}
		ordered = append(ordered, scope)
	}
	if len(ordered) == 0 {
		return "", ErrOIDCInvalidScope
	}
	sort.Strings(ordered)
	return strings.Join(ordered, " "), nil
}
