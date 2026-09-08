//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWSPassthroughUsageMeta_InitFromFirstFrame_MappedModelCandidate(t *testing.T) {
	body := []byte(`{"type":"response.create","model":"sol","reasoning":{"effort":"max"}}`)

	meta := newOpenAIWSPassthroughUsageMeta("sol", body)
	meta.initFromFirstFrame(body, "gpt-5.6-sol")

	got := meta.reasoningEffort.Load()
	require.NotNil(t, got, "reasoning effort should be set")
	require.Equal(t, "max", *got, "mapped model gpt-5.6-sol should preserve max")
}

func TestWSPassthroughUsageMeta_InitFromFirstFrame_NonGPT56FallsBackToXHigh(t *testing.T) {
	body := []byte(`{"type":"response.create","model":"gpt-5.4","reasoning":{"effort":"max"}}`)

	meta := newOpenAIWSPassthroughUsageMeta("gpt-5.4", body)
	meta.initFromFirstFrame(body, "gpt-5.4")

	got := meta.reasoningEffort.Load()
	require.NotNil(t, got)
	require.Equal(t, "xhigh", *got, "non-5.6 model should normalize max to xhigh")
}

func TestWSPassthroughUsageMeta_UpdateFromResponseCreate_MappedModelCandidate(t *testing.T) {
	body := []byte(`{"type":"response.create","model":"sol","reasoning":{"effort":"max"}}`)

	meta := newOpenAIWSPassthroughUsageMeta("sol", body)
	meta.updateFromResponseCreate(body, "gpt-5.6-sol", "sol")

	got := meta.reasoningEffort.Load()
	require.NotNil(t, got)
	require.Equal(t, "max", *got, "mapped model should preserve max on multi-turn update")
}

func TestCaptureOpenAIWSRequestedReasoningEffortPreservesPerTurnInput(t *testing.T) {
	type capturedTurn struct {
		turn   int
		body   string
		model  string
		effort *string
	}
	var captured []capturedTurn
	hooks := &OpenAIWSIngressHooks{CaptureRequestedReasoningEffort: func(turn int, payload []byte, model string) {
		captured = append(captured, capturedTurn{
			turn: turn, body: string(payload), model: model,
			effort: CanonicalRequestedReasoningEffort(payload, model),
		})
	}}
	first := []byte(`{"type":"response.create","model":"gpt-5.6","reasoning":{"effort":"max"}}`)
	second := []byte(`{"type":"response.create","model":"gpt-5.4","reasoning":{"effort":"low"}}`)
	captureOpenAIWSRequestedReasoningEffort(hooks, 1, first, "gpt-5.6")
	captureOpenAIWSRequestedReasoningEffort(hooks, 2, second, "gpt-5.4")

	require.Len(t, captured, 2)
	require.Equal(t, 1, captured[0].turn)
	require.Equal(t, string(first), captured[0].body)
	require.Equal(t, "gpt-5.6", captured[0].model)
	require.NotNil(t, captured[0].effort)
	require.Equal(t, "max", *captured[0].effort)
	require.Equal(t, 2, captured[1].turn)
	require.Equal(t, string(second), captured[1].body)
	require.Equal(t, "gpt-5.4", captured[1].model)
	require.NotNil(t, captured[1].effort)
	require.Equal(t, "low", *captured[1].effort)
}
