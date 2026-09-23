package config

import (
	"encoding/hex"
	"fmt"
	"net/url"
)

func validateOIDCProviderConfig(p OIDCProviderConfig) error {
	if p.Issuer != "https://auth.taffy.edu.kg" {
		return fmt.Errorf("oidc_provider.issuer must be exactly https://auth.taffy.edu.kg")
	}
	u, err := url.Parse(p.Issuer)
	if err != nil || u.Scheme != "https" || u.Host != "auth.taffy.edu.kg" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("oidc_provider.issuer must be an exact HTTPS origin")
	}
	if p.PublicHost != "auth.taffy.edu.kg" || p.SigningAlg != "RS256" || p.SigningKeySource != "database_encrypted" {
		return fmt.Errorf("oidc_provider public host, signing algorithm, or key source is not frozen")
	}
	key, err := hex.DecodeString(p.EncryptionKey)
	if err != nil || len(key) != 32 {
		return fmt.Errorf("oidc_provider.encryption_key must be a 64-character hex AES-256 key")
	}
	if len(p.SecretPepper) < 32 {
		return fmt.Errorf("oidc_provider.secret_pepper must contain at least 32 characters")
	}
	if !p.RequirePKCES256 || !p.AuthorizationResponseIssuer {
		return fmt.Errorf("oidc_provider PKCE S256 and authorization response issuer are mandatory")
	}
	if p.Cookie.SessionName != "__Host-sub2_oidc_session" || p.Cookie.TransactionName != "__Host-sub2_oidc_tx" || !p.Cookie.Secure || !p.Cookie.HTTPOnly || p.Cookie.SameSite != "lax" {
		return fmt.Errorf("oidc_provider cookie contract is invalid")
	}
	for _, item := range []struct {
		name  string
		value int
		min   int
		max   int
	}{
		{"transaction_ttl_seconds", p.TransactionTTLSeconds, 60, 900},
		{"browser_session_idle_ttl_seconds", p.BrowserSessionIdleTTLSeconds, 60, 86400},
		{"browser_session_absolute_ttl_seconds", p.BrowserSessionAbsoluteTTLSeconds, p.BrowserSessionIdleTTLSeconds, 86400 * 7},
		{"authorization_code_ttl_seconds", p.AuthorizationCodeTTLSeconds, 10, 300},
		{"access_token_ttl_seconds", p.AccessTokenTTLSeconds, 30, 3600},
		{"id_token_ttl_seconds", p.IDTokenTTLSeconds, 30, 3600},
		{"refresh_token_idle_ttl_seconds", p.RefreshTokenIdleTTLSeconds, 3600, 86400 * 30},
		{"refresh_token_absolute_ttl_seconds", p.RefreshTokenAbsoluteTTLSeconds, p.RefreshTokenIdleTTLSeconds, 86400 * 90},
	} {
		if item.value < item.min || item.value > item.max {
			return fmt.Errorf("oidc_provider.%s must be between %d and %d seconds", item.name, item.min, item.max)
		}
	}
	if p.ClientSecretTTLSeconds < 1 || p.ClientSecretTTLSeconds > 86400*365 {
		return fmt.Errorf("oidc_provider.client_secret_ttl_seconds must be between 1 and 31536000 seconds")
	}
	if p.ClockSkewSeconds < 0 || p.ClockSkewSeconds > 300 || p.JWKSCacheMaxAgeSeconds <= 0 || p.JWKSCacheMaxAgeSeconds > 3600 || p.ClientSecretMaxOverlapSeconds <= 0 || p.ClientSecretMaxOverlapSeconds > 86400 {
		return fmt.Errorf("oidc_provider clock skew, JWKS cache age, or secret overlap is outside the safe range")
	}
	allowed := map[string]struct{}{"openid": {}, "profile": {}, "email": {}, "roles": {}, "offline_access": {}}
	seen := make(map[string]struct{}, len(p.AllowedScopes))
	for _, scope := range p.AllowedScopes {
		if _, ok := allowed[scope]; !ok {
			return fmt.Errorf("oidc_provider.allowed_scopes contains unsupported scope %q", scope)
		}
		if _, ok := seen[scope]; ok {
			return fmt.Errorf("oidc_provider.allowed_scopes contains duplicate scope %q", scope)
		}
		seen[scope] = struct{}{}
	}
	if _, ok := seen["openid"]; !ok {
		return fmt.Errorf("oidc_provider.allowed_scopes must contain openid")
	}
	return nil
}
