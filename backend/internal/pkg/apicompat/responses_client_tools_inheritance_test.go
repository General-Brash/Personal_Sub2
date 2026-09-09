package apicompat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesClientToolsInheritOmittedDeclarations(t *testing.T) {
	first := map[string]any{"tools": []any{map[string]any{"type": "custom", "name": "write_file"}}}
	mapping, _, err := AdaptResponsesClientTools(first)
	require.NoError(t, err)
	next := map[string]any{"input": []any{map[string]any{"type": "custom_tool_call", "name": "write_file", "input": "hello"}}}
	lowered, ok := first["tools"].([]any)
	require.True(t, ok)
	inherited, changed, err := AdaptResponsesClientToolsWithInheritedMapping(next, mapping, lowered)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, inherited.CustomTools["write_file"])
	tools, ok := next["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	tool, ok := tools[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "function", tool["type"])
	input, ok := next["input"].([]any)
	require.True(t, ok)
	item, ok := input[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "function_call", item["type"])
}

func TestResponsesClientToolsExplicitEmptyReplacesInheritedDeclarations(t *testing.T) {
	req := map[string]any{"tools": []any{}}
	mapping, changed, err := AdaptResponsesClientToolsWithInheritedMapping(req, ResponsesClientToolMapping{CustomTools: map[string]bool{"write_file": true}}, []any{map[string]any{"type": "function", "name": "write_file"}})
	require.NoError(t, err)
	require.False(t, changed)
	require.Empty(t, mapping.CustomTools)
	require.Empty(t, req["tools"])
}
