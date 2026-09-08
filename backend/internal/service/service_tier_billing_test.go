package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveBillingServiceTierNeverRaisesBill(t *testing.T) {
	cases := []struct {
		requested, observed, billing string
		downgraded                   bool
	}{
		{"priority", "default", "default", true},
		{"fast", "standard", "standard", true},
		{"", "priority", "", false},
		{"flex", "default", "flex", false},
		{"priority", "turbo", "priority", false},
	}
	for _, tc := range cases {
		got := ResolveBillingServiceTier(tc.requested, tc.observed)
		require.Equal(t, tc.billing, got.Billing)
		require.Equal(t, tc.downgraded, got.Downgraded)
	}
}

func TestResolveOpenAIServiceTierBillingOAuthException(t *testing.T) {
	requested := ResolveOpenAIServiceTierBilling(
		&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		"priority",
		"default",
	)
	require.Equal(t, "priority", requested.Billing)
	require.False(t, requested.Downgraded)

	apiKey := ResolveOpenAIServiceTierBilling(
		&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		"priority",
		"default",
	)
	require.Equal(t, "default", apiKey.Billing)
	require.True(t, apiKey.Downgraded)
}
