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
