package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeCompactionTriggerInputOrder(t *testing.T) {
	body := []byte(`{"input":[{"type":"compaction_trigger"},{"type":"message","role":"user"},{"type":"compaction_trigger"}]}`)
	normalized, changed, err := NormalizeCompactionTriggerInputOrder(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "message", gjson.GetBytes(normalized, "input.0.type").String())
	require.Equal(t, "compaction_trigger", gjson.GetBytes(normalized, "input.1.type").String())
	second, changedAgain, err := NormalizeCompactionTriggerInputOrder(normalized)
	require.NoError(t, err)
	require.False(t, changedAgain)
	require.Equal(t, normalized, second)
}
