package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelPlazaOverrideKey_Normalizes(t *testing.T) {
	require.Equal(t, "anthropic:claude-sonnet", ModelPlazaOverrideKey("Anthropic", "Claude-Sonnet"))
	require.Equal(t, "openai:gpt-4o", ModelPlazaOverrideKey("  OpenAI ", " GPT-4o "))
}

func TestParseModelPlazaAdminSettings_DefaultsHideNoAccountOn(t *testing.T) {
	// 空设置：默认收敛开启（隐藏无账号幽灵模型），无 overrides，version 非空。
	s, err := parseModelPlazaAdminSettings(map[string]string{})
	require.NoError(t, err)
	require.True(t, s.HideNoAccount)
	require.Empty(t, s.Overrides)
	require.NotEmpty(t, s.Version)
}

func TestParseModelPlazaAdminSettings_ExplicitFalseDisablesConvergence(t *testing.T) {
	s, err := parseModelPlazaAdminSettings(map[string]string{
		SettingKeyModelPlazaHideNoAccount: "false",
	})
	require.NoError(t, err)
	require.False(t, s.HideNoAccount)
}

func TestParseModelPlazaAdminSettings_ParsesOverrides(t *testing.T) {
	s, err := parseModelPlazaAdminSettings(map[string]string{
		SettingKeyModelPlazaOverrides: `{"anthropic:claude-sonnet":{"hidden":true,"pinned":true,"sort_order":3}}`,
	})
	require.NoError(t, err)
	ov, ok := s.Overrides["anthropic:claude-sonnet"]
	require.True(t, ok)
	require.True(t, ov.Hidden)
	require.True(t, ov.Pinned)
	require.Equal(t, 3, ov.SortOrder)
}

func TestParseModelPlazaAdminSettings_VersionIsContentAddressed(t *testing.T) {
	a, err := parseModelPlazaAdminSettings(map[string]string{
		SettingKeyModelPlazaOverrides:     `{"p:m":{"hidden":true}}`,
		SettingKeyModelPlazaHideNoAccount: "true",
	})
	require.NoError(t, err)
	b, err := parseModelPlazaAdminSettings(map[string]string{
		SettingKeyModelPlazaOverrides:     `{"p:m":{"hidden":true}}`,
		SettingKeyModelPlazaHideNoAccount: "true",
	})
	require.NoError(t, err)
	require.Equal(t, a.Version, b.Version) // 内容相同 → 版本一致

	c, err := parseModelPlazaAdminSettings(map[string]string{
		SettingKeyModelPlazaHideNoAccount: "false",
	})
	require.NoError(t, err)
	require.NotEqual(t, a.Version, c.Version) // 内容不同 → 版本不同
}

func TestParseModelPlazaAdminSettings_InvalidJSONErrors(t *testing.T) {
	_, err := parseModelPlazaAdminSettings(map[string]string{
		SettingKeyModelPlazaOverrides: `{bad json`,
	})
	require.Error(t, err)
}
