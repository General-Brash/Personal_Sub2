package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFusion021ConfigDefaultsAndEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	require.Empty(t, cfg.Pricing.OverrideFile)
	require.Equal(t, DefaultModelsListReadMaxBytes, cfg.Gateway.ModelsListReadMaxBytes)
	require.Equal(t, 120, cfg.Gateway.GrokResponseHeaderTimeout)
	t.Setenv("PRICING_OVERRIDE_FILE", "fixture-prices.json")
	t.Setenv("GATEWAY_MODELS_LIST_READ_MAX_BYTES", "4096")
	t.Setenv("GATEWAY_GROK_RESPONSE_HEADER_TIMEOUT", "300")
	cfg, err = Load()
	require.NoError(t, err)
	require.Equal(t, "fixture-prices.json", cfg.Pricing.OverrideFile)
	require.EqualValues(t, 4096, cfg.Gateway.ModelsListReadMaxBytes)
	require.Equal(t, 300, cfg.Gateway.GrokResponseHeaderTimeout)
}

func TestFusion021ConfigRejectsUnsafeLimits(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"empty model response limit", "GATEWAY_MODELS_LIST_READ_MAX_BYTES", "0"},
		{"negative grok timeout", "GATEWAY_GROK_RESPONSE_HEADER_TIMEOUT", "-1"},
		{"excessive grok timeout", "GATEWAY_GROK_RESPONSE_HEADER_TIMEOUT", "1801"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			require.Error(t, err)
		})
	}
}
