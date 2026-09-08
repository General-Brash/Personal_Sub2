package service

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIsOpenAIOAuthLike(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    bool
		codex   bool
	}{
		{name: "openai_oauth", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, want: true, codex: true},
		{name: "openai_setup_token", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeSetupToken}, want: true, codex: true},
		{name: "openai_api_key", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, want: false, codex: false},
		{name: "anthropic_setup_token", account: &Account{Platform: PlatformAnthropic, Type: AccountTypeSetupToken}, want: false, codex: false},
		{name: "grok_setup_token", account: &Account{Platform: PlatformGrok, Type: AccountTypeSetupToken}, want: false, codex: false},
		{name: "nil", account: nil, want: false, codex: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.account.IsOpenAIOAuthLike())
			require.Equal(t, tt.codex, tt.account.UsesOpenAICodexProtocol())
		})
	}
}
