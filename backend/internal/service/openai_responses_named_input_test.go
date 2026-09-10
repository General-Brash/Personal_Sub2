package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSanitizeOpenAIResponsesNamedInputWhitespacePreservesFields(t *testing.T) {
	named := map[string]any{
		"type": "function_call_output", "name": "  local_tool\t", "call_id": " \t",
		"namespace": "local_namespace", "id": "input_local", "output": map[string]any{"text": "unchanged"},
	}
	before, err := json.Marshal(named)
	require.NoError(t, err)
	input := []any{named, map[string]any{"type": "function_call_output", "name": "local_tool", "call_id": "unmatched", "output": "orphan"}}
	reqBody := map[string]any{"input": input}
	require.True(t, sanitizeOpenAIResponsesOrphanToolOutputs(reqBody, input, false))
	got := reqBody["input"].([]any)
	require.Len(t, got, 1)
	after, err := json.Marshal(got[0])
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after), "classification must not rewrite the native input fields")
	require.False(t, sanitizeOpenAIResponsesOrphanToolOutputs(reqBody, got, false))
}

func TestOpenAIGatewayService_PreservesNamedStandaloneFunctionOutputHTTP(t *testing.T) {
	for _, withHistory := range []bool{false, true} {
		t.Run(fmt.Sprintf("history=%t", withHistory), func(t *testing.T) {
			input := []any{}
			if withHistory {
				input = append(input,
					map[string]any{"type": "function_call", "call_id": "fc_history", "name": "lookup", "arguments": "{}"},
					map[string]any{"type": "function_call_output", "call_id": "fc_history", "output": "historical result"},
				)
			}
			named := map[string]any{"type": "function_call_output", "name": "send_message_to_thread", "namespace": "codex_app", "output": "local delegation payload"}
			input = append(input, named)
			body, err := json.Marshal(map[string]any{"model": "gpt-5.5", "stream": false, "previous_response_id": "resp_missing", "input": input})
			require.NoError(t, err)
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"id":"resp_local","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`),
			}}
			result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
				context.Background(), newOpenAIRejectedFieldTestContext(body), newOpenAIOAuthNamespaceTestAccount(), body,
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.bodies, 1, "the compatibility fix must not replay the upstream request")
			forwarded := upstream.bodies[0]
			require.False(t, gjson.GetBytes(forwarded, "previous_response_id").Exists(), "exercise orphan cleanup after previous-response removal")
			items := gjson.GetBytes(forwarded, "input").Array()
			require.Len(t, items, len(input))
			// HTTP compatibility deliberately strips namespace from output items;
			// preserve that existing policy while retaining the named input itself.
			expected, err := json.Marshal(map[string]any{
				"type": "function_call_output", "name": "send_message_to_thread", "output": "local delegation payload",
			})
			require.NoError(t, err)
			require.JSONEq(t, string(expected), items[len(items)-1].Raw)
			if withHistory {
				require.Equal(t, "fc_history", items[0].Get("call_id").String())
				require.Equal(t, "fc_history", items[1].Get("call_id").String())
				require.Equal(t, "historical result", items[1].Get("output").String())
			}
		})
	}
}
