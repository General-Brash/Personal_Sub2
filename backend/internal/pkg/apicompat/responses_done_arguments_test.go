package apicompat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func doneTestTool(state *ResponsesEventToChatState, index int, id, kind string) {
	ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.output_item.added", OutputIndex: index,
		Item: &ResponsesOutput{Type: kind, CallID: id, Name: "local_tool"},
	}, state)
}

func doneTestArguments(t *testing.T, chunks []ChatCompletionsChunk) ChatToolCall {
	t.Helper()
	require.Len(t, chunks, 1)
	require.Len(t, chunks[0].Choices, 1)
	require.Len(t, chunks[0].Choices[0].Delta.ToolCalls, 1)
	return chunks[0].Choices[0].Delta.ToolCalls[0]
}

func TestResponsesArgumentsDoneCompletesWithoutDuplicating(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, complete, suffix string
	}{
		{"done_only", "", `{"city":"local"}`, `{"city":"local"}`},
		{"partial", `{"city":`, `{"city":"local"}`, `"local"}`},
		{"complete_delta", `{"city":"local"}`, `{"city":"local"}`, ""},
		{"conflicting_done", `{"city":`, `{"other":"local"}`, ""},
		{"empty_done", `{"city":`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewResponsesEventToChatState()
			doneTestTool(state, 3, "call_local", "function_call")
			if tc.prefix != "" {
				chunk := doneTestArguments(t, ResponsesEventToChatChunks(&ResponsesStreamEvent{
					Type: "response.function_call_arguments.delta", OutputIndex: 3, Delta: tc.prefix,
				}, state))
				require.Equal(t, tc.prefix, chunk.Function.Arguments)
			}
			event := &ResponsesStreamEvent{Type: "response.function_call_arguments.done", OutputIndex: 3, Arguments: tc.complete}
			chunks := ResponsesEventToChatChunks(event, state)
			if tc.suffix == "" {
				require.Empty(t, chunks)
			} else {
				chunk := doneTestArguments(t, chunks)
				require.Equal(t, tc.suffix, chunk.Function.Arguments)
				require.Equal(t, tc.complete, tc.prefix+chunk.Function.Arguments)
			}
			require.Empty(t, ResponsesEventToChatChunks(event, state), "a repeated done event must not append arguments twice")
		})
	}
}

func TestResponsesArgumentsDoneHandlesCustomAndInterleavedTools(t *testing.T) {
	state := NewResponsesEventToChatState()
	doneTestTool(state, 2, "call_function", "function_call")
	doneTestTool(state, 8, "call_custom", "custom_tool_call")
	custom := doneTestArguments(t, ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.custom_tool_call_input.done", OutputIndex: 8, Input: "local custom input",
	}, state))
	require.Equal(t, 1, *custom.Index)
	require.Equal(t, "local custom input", custom.Function.Arguments)
	function := doneTestArguments(t, ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.function_call_arguments.done", OutputIndex: 2, Arguments: `{"a":1}`,
	}, state))
	require.Equal(t, 0, *function.Index)
	require.Equal(t, `{"a":1}`, function.Function.Arguments)
	require.Empty(t, ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.function_call_arguments.done", OutputIndex: 99, Arguments: "unknown",
	}, state))
}

func TestResponsesArgumentsDoneSupportsLegacyStateLiterals(t *testing.T) {
	for _, eventType := range []string{"response.function_call_arguments.delta", "response.function_call_arguments.done"} {
		t.Run(eventType, func(t *testing.T) {
			state := &ResponsesEventToChatState{OutputIndexToToolIndex: map[int]int{7: 0}}
			event := &ResponsesStreamEvent{Type: eventType, OutputIndex: 7, Delta: "payload", Arguments: "payload"}
			chunk := doneTestArguments(t, ResponsesEventToChatChunks(event, state))
			require.Equal(t, "payload", chunk.Function.Arguments)
			require.Equal(t, "payload", state.OutputIndexToArguments[7])
		})
	}
}

func doneTestAccumulator() *BufferedResponseAccumulator {
	acc := NewBufferedResponseAccumulator()
	for _, tool := range []struct {
		index               int
		id, kind, arguments string
	}{
		{0, "call_a", "function_call", `{"a":1}`},
		{2, "call_b", "custom_tool_call", "local custom input"},
	} {
		acc.ProcessEvent(&ResponsesStreamEvent{
			Type: "response.output_item.added", OutputIndex: tool.index,
			Item: &ResponsesOutput{Type: tool.kind, CallID: tool.id, Name: "local_tool"},
		})
		acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.function_call_arguments.delta", OutputIndex: tool.index, Delta: "partial"})
		event := &ResponsesStreamEvent{Type: "response.function_call_arguments.done", OutputIndex: tool.index, Arguments: tool.arguments}
		if tool.kind == "custom_tool_call" {
			event.Type = "response.custom_tool_call_input.done"
			event.Input = tool.arguments
		}
		acc.ProcessEvent(event)
		acc.ProcessEvent(event)
	}
	return acc
}

func TestBufferedArgumentsDoneFillsEmptyTerminalOutput(t *testing.T) {
	acc := doneTestAccumulator()
	acc.ProcessEvent(&ResponsesStreamEvent{Type: "response.function_call_arguments.done", OutputIndex: 99, Arguments: "unknown"})
	response := &ResponsesResponse{}
	acc.SupplementResponseOutput(response)
	require.Len(t, response.Output, 2)
	require.Equal(t, `{"a":1}`, response.Output[0].Arguments)
	require.Equal(t, "local custom input", response.Output[1].Arguments)
	acc.SupplementResponseOutput(response)
	require.Len(t, response.Output, 2)
	require.Equal(t, `{"a":1}`, response.Output[0].Arguments)
}

func TestBufferedArgumentsDoneMatchesCallIDBeforeOutputPosition(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output []ResponsesOutput
		want   []string
	}{
		{"reordered", []ResponsesOutput{{Type: "function_call", CallID: "call_b"}, {Type: "function_call", CallID: "call_a"}}, []string{"local custom input", `{"a":1}`}},
		{"mismatched_id", []ResponsesOutput{{Type: "function_call", CallID: "different_call"}}, []string{""}},
		{"authoritative_arguments", []ResponsesOutput{{Type: "function_call", CallID: "call_a", Arguments: "keep upstream"}}, []string{"keep upstream"}},
		{"idless_position", []ResponsesOutput{{Type: "function_call"}}, []string{`{"a":1}`}},
		{"non_function", []ResponsesOutput{{Type: "message", CallID: "call_a"}}, []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := &ResponsesResponse{Output: tc.output}
			doneTestAccumulator().SupplementResponseOutput(response)
			for index, want := range tc.want {
				require.Equal(t, want, response.Output[index].Arguments)
			}
		})
	}
}

func TestResponsesStreamServiceTierFollowsTerminalMetadata(t *testing.T) {
	state := NewResponsesEventToChatState()
	state.IncludeUsage = true
	created := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.created", Response: &ResponsesResponse{ServiceTier: "priority"},
	}, state)
	require.Equal(t, "priority", created[0].ServiceTier)
	text := ResponsesEventToChatChunks(&ResponsesStreamEvent{Type: "response.output_text.delta", Delta: "local"}, state)
	require.Equal(t, "priority", text[0].ServiceTier)
	completed := ResponsesEventToChatChunks(&ResponsesStreamEvent{
		Type: "response.completed", Response: &ResponsesResponse{Status: "completed", ServiceTier: "flex", Usage: &ResponsesUsage{InputTokens: 2, OutputTokens: 1}},
	}, state)
	require.Len(t, completed, 2)
	for _, chunk := range completed {
		require.Equal(t, "flex", chunk.ServiceTier)
		wire, err := ChatChunkToSSE(chunk)
		require.NoError(t, err)
		require.Contains(t, wire, `"service_tier":"flex"`)
	}
	require.NotNil(t, completed[1].Usage)
	require.Equal(t, 3, completed[1].Usage.TotalTokens)
	require.Empty(t, FinalizeResponsesChatStream(state))
}

func TestResponsesStreamServiceTierSurvivesFallbackFinalization(t *testing.T) {
	for _, tier := range []string{"", "priority"} {
		t.Run("tier="+tier, func(t *testing.T) {
			state := NewResponsesEventToChatState()
			state.IncludeUsage = true
			state.Usage = &ChatUsage{PromptTokens: 1}
			ResponsesEventToChatChunks(&ResponsesStreamEvent{Type: "response.created", Response: &ResponsesResponse{ServiceTier: tier}}, state)
			chunks := FinalizeResponsesChatStream(state)
			require.Len(t, chunks, 2)
			for _, chunk := range chunks {
				require.Equal(t, tier, chunk.ServiceTier)
				wire, err := ChatChunkToSSE(chunk)
				require.NoError(t, err)
				if tier == "" {
					require.NotContains(t, wire, "service_tier")
				}
			}
			require.Empty(t, FinalizeResponsesChatStream(state))
		})
	}
}
