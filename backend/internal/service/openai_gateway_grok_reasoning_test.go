package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrokSupportsXHighReasoningEffort(t *testing.T) {
	for _, model := range []string{"grok-4.6", "grok-4.6-latest", "xai/grok-4.6"} {
		require.True(t, GrokSupportsXHighReasoningEffort(model), model)
	}
	for _, model := range []string{"grok-4.5", "grok-4.3", "grok-4.20-reasoning", ""} {
		require.False(t, GrokSupportsXHighReasoningEffort(model), model)
	}
}
