package apicompat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdaptResponsesClientToolsPromotesDiscoveredFunctionTools(t *testing.T) {
	req := map[string]any{
		"tools": []any{map[string]any{"type": "tool_search"}},
		"input": []any{map[string]any{
			"type": "tool_search_output", "status": "completed",
			"tools": []any{map[string]any{
				"type": "function", "name": "lookup_weather",
				"description": "look up weather", "parameters": map[string]any{"type": "object"},
			}},
		}},
	}
	mapping, changed, err := AdaptResponsesClientTools(req)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, mapping.ToolSearch)
	require.Len(t, req["tools"], 2)
	require.Equal(t, "lookup_weather", req["tools"].([]any)[1].(map[string]any)["name"])
}

func TestAdaptResponsesClientToolsRejectsConflictingDiscovery(t *testing.T) {
	req := map[string]any{
		"tools": []any{
			map[string]any{"type": "tool_search"},
			map[string]any{"type": "function", "name": "lookup_weather", "parameters": map[string]any{"type": "object"}},
		},
		"input": []any{map[string]any{
			"type": "tool_search_output", "status": "completed",
			"tools": []any{map[string]any{
				"type": "function", "name": "lookup_weather", "parameters": map[string]any{"type": "string"},
			}},
		}},
	}
	_, _, err := AdaptResponsesClientTools(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "conflicts")
}
