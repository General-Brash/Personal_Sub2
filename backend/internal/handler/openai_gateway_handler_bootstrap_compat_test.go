package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeCodexAutomationBootstrap(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":[{"type":"function_call_output","namespace":"codex_app","name":"automation_update","output":"Automation: daily\nAutomation ID: daily\nAutomation memory: $CODEX_HOME/automations/daily/memory.md\nLast run: never\n\nRun completed"}]}`)
	normalized, changed := normalizeCodexAutomationBootstrap(body)
	require.True(t, changed)
	require.Equal(t, "message", gjson.GetBytes(normalized, "input.0.type").String())
	require.Contains(t, gjson.GetBytes(normalized, "input.0.content.0.text").String(), "Run completed")
}

func TestNormalizeCodexBootstrapDoesNotRewriteAnchoredOutput(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","input":[{"type":"function_call","call_id":"call_1"},{"type":"function_call_output","namespace":"codex_app","name":"automation_update","output":"Automation: daily\nAutomation ID: daily\nAutomation memory: $CODEX_HOME/automations/daily/memory.md\nLast run: never\n\nRun completed"}]}`)
	normalized, changed := normalizeCodexAutomationBootstrap(body)
	require.False(t, changed)
	require.JSONEq(t, string(body), string(normalized))
}
