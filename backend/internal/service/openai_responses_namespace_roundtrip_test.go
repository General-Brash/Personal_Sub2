package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Replay the actual downstream output, not a hand-built approximation of the
// second request. HTTP 200 alone does not prove that MCP tool identity survived.
func TestOpenAIGatewayService_APIKeyNamespaceRoundTrip(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("passthrough=%t/stream=%t", passthrough, stream), func(t *testing.T) {
				account := newOpenAIRejectedFieldTestAccount()
				account.Extra["openai_passthrough"] = passthrough
				upstream := &httpUpstreamRecorder{}
				svc := newOpenAIRejectedFieldTestService(upstream)
				namespaces := []string{"mcp__ableton", "mcp__other"}
				tools := make([]any, 0, len(namespaces))
				for _, namespace := range namespaces {
					tools = append(tools, map[string]any{
						"type": "namespace", "name": namespace,
						"tools": []any{map[string]any{
							"type": "function", "name": "get_track_info",
							"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
						}},
					})
				}
				history := []json.RawMessage{json.RawMessage(`{"type":"message","role":"user","namespace":"leftover","content":[{"type":"input_text","text":"inspect tracks"}]}`)}
				for round := 0; round < 3; round++ {
					// Same tool name in distinct namespaces must not collide. By the
					// third request, history exceeds the six rejected-field retries.
					calls := make([]map[string]any, 0, 4)
					for index := 0; index < 4; index++ {
						calls = append(calls, map[string]any{
							"type": "function_call", "namespace": namespaces[index%len(namespaces)],
							"name": "get_track_info", "call_id": fmt.Sprintf("call_%d_%d", round, index),
							"arguments": `{"track_index":1,"large":9007199254740993}`,
						})
					}
					response, err := json.Marshal(map[string]any{
						"id": fmt.Sprintf("resp_%d", round), "object": "response", "status": "completed",
						"model": "gpt-5.5", "output": calls,
						"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
					})
					require.NoError(t, err)
					responseBody := string(response)
					if stream {
						responseBody = "data: {\"type\":\"response.completed\",\"response\":" + responseBody + "}\n\ndata: [DONE]\n\n"
					}
					resp := newOpenAIRejectedFieldTestResponse(http.StatusOK, responseBody)
					if stream {
						resp.Header.Set("Content-Type", "text/event-stream")
					}
					upstream.responses = append(upstream.responses, resp)

					request := map[string]any{"model": "gpt-5.5", "stream": stream, "input": history}
					// Continuations may omit declarations; identity cannot be inferred
					// only from the tools present in the current request.
					if round != 1 {
						request["tools"] = tools
					}
					body, err := json.Marshal(request)
					require.NoError(t, err)
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
					c.Request.Header.Set("Content-Type", "application/json")
					c.Request.Header.Set("User-Agent", "curl/8.0")
					result, err := svc.Forward(context.Background(), c, account, body)
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, http.StatusOK, rec.Code)
					require.Len(t, upstream.bodies, round+1, "accepted namespace must not trigger a retry")
					forwarded := upstream.bodies[round]
					if round != 1 {
						expectedTools, err := json.Marshal(tools)
						require.NoError(t, err)
						require.JSONEq(t, string(expectedTools), gjson.GetBytes(forwarded, "tools").Raw)
					}
					input := gjson.GetBytes(forwarded, "input").Array()
					require.Len(t, input, len(history))
					require.False(t, input[0].Get("namespace").Exists(), "non-call residue still gets removed")
					for index := 1; index < len(history); index++ {
						require.JSONEq(t, string(history[index]), input[index].Raw, "round=%d item=%d", round, index)
					}

					output := gjson.Get(rec.Body.String(), "output")
					if stream {
						for _, line := range strings.Split(rec.Body.String(), "\n") {
							data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
							if gjson.Get(data, "type").String() == "response.completed" {
								output = gjson.Get(data, "response.output")
							}
						}
					}
					require.Len(t, output.Array(), len(calls))
					for index, call := range output.Array() {
						expected, err := json.Marshal(calls[index])
						require.NoError(t, err)
						require.JSONEq(t, string(expected), call.Raw)
						history = append(history, json.RawMessage(call.Raw))
						toolOutput, err := json.Marshal(map[string]any{
							"type": "function_call_output", "call_id": call.Get("call_id").String(), "output": "ok",
						})
						require.NoError(t, err)
						history = append(history, toolOutput)
					}
				}
			})
		}
	}
}
