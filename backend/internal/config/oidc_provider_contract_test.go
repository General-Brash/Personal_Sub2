package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOIDCProviderSecretPepperEnvironmentOverridesConfigFile(t *testing.T) {
	resetViperWithJWTSecret(t)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("oidc_provider:\n  secret_pepper: file-secret-pepper-abcdefghijklmnopqrstuvwxyz\n"), 0o600))
	t.Setenv("CONFIG_FILE", configPath)
	t.Setenv("OIDC_PROVIDER_SECRET_PEPPER", "env-secret-pepper-abcdefghijklmnopqrstuvwxyz")

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "env-secret-pepper-abcdefghijklmnopqrstuvwxyz", cfg.OIDCProvider.SecretPepper)
}

func TestOIDCProviderClientSecretTTLDefaultAndBounds(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, 90*86400, cfg.OIDCProvider.ClientSecretTTLSeconds)
	require.Equal(t, 86400, cfg.OIDCProvider.ClientSecretMaxOverlapSeconds)
	p := cfg.OIDCProvider
	p.EncryptionKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	p.SecretPepper = "abcdefghijklmnopqrstuvwxyz0123456789"
	for _, seconds := range []int{1, 365 * 86400} {
		p.ClientSecretTTLSeconds = seconds
		require.NoError(t, validateOIDCProviderConfig(p))
	}
	for _, seconds := range []int{0, -1, 365*86400 + 1} {
		p.ClientSecretTTLSeconds = seconds
		require.ErrorContains(t, validateOIDCProviderConfig(p), "client_secret_ttl_seconds")
	}
	p.ClientSecretTTLSeconds = 90 * 86400
	p.ClientSecretMaxOverlapSeconds = 86401
	require.Error(t, validateOIDCProviderConfig(p))
}

func TestOIDCProviderOldOverlapConfigKeepsIndependentDefault(t *testing.T) {
	resetViperWithJWTSecret(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("oidc_provider:\n  client_secret_max_overlap_seconds: 3600\n"), 0o600))
	t.Setenv("CONFIG_FILE", configPath)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, 3600, cfg.OIDCProvider.ClientSecretMaxOverlapSeconds)
	require.Equal(t, 90*86400, cfg.OIDCProvider.ClientSecretTTLSeconds)
}
