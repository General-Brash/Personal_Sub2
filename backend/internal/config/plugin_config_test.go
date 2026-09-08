package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadPluginSafetyDefaults(t *testing.T) {
	resetViperWithJWTSecret(t)

	cfg, err := Load()
	require.NoError(t, err)
	require.Empty(t, cfg.Plugins.DataDir)
	require.False(t, cfg.Plugins.AllowUnsigned)
	require.Empty(t, cfg.Plugins.TrustedPublishers)
	require.Equal(t, int64(128*1024*1024), cfg.Plugins.MaxUploadBytes)
	require.Equal(t, int64(256*1024*1024), cfg.Plugins.MaxUncompressedBytes)
	require.Equal(t, 15, cfg.Plugins.StartTimeoutSeconds)
}

func TestLoadPluginConfigFromEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	dataDir := filepath.Join(t.TempDir(), "plugins")
	t.Setenv("PLUGINS_DATA_DIR", dataDir)
	t.Setenv("PLUGINS_ALLOW_UNSIGNED", "true")
	t.Setenv("PLUGINS_MAX_UPLOAD_BYTES", "1048576")
	t.Setenv("PLUGINS_MAX_UNCOMPRESSED_BYTES", "2097152")
	t.Setenv("PLUGINS_START_TIMEOUT_SECONDS", "30")

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, dataDir, cfg.Plugins.DataDir)
	require.True(t, cfg.Plugins.AllowUnsigned)
	require.Equal(t, int64(1048576), cfg.Plugins.MaxUploadBytes)
	require.Equal(t, int64(2097152), cfg.Plugins.MaxUncompressedBytes)
	require.Equal(t, 30, cfg.Plugins.StartTimeoutSeconds)
}

func TestLoadRejectsUnsafePluginLimits(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "upload size is zero",
			env:     map[string]string{"PLUGINS_MAX_UPLOAD_BYTES": "0"},
			wantErr: "plugins.max_upload_bytes must be between 1 and 1073741824",
		},
		{
			name: "uncompressed size is below upload size",
			env: map[string]string{
				"PLUGINS_MAX_UPLOAD_BYTES":       "2097152",
				"PLUGINS_MAX_UNCOMPRESSED_BYTES": "1048576",
			},
			wantErr: "plugins.max_uncompressed_bytes must be between max_upload_bytes and 2147483648",
		},
		{
			name:    "startup timeout is too large",
			env:     map[string]string{"PLUGINS_START_TIMEOUT_SECONDS": "121"},
			wantErr: "plugins.start_timeout_seconds must be between 1 and 120",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			_, err := Load()
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
