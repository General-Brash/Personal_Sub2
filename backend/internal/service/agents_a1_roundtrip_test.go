//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise the imported normalization together with P4.2's namespace handling.
// Replay actual returned calls, including custom input, through Forward again.
func TestForwardResponses_AgentsA1NamespaceCustomRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			account := forceChatResponsesFallbackAccount()
			account.Credentials["model_mapping"] = map[string]any{"Auto": "Agents-A1"}
			upstream := &httpUpstreamRecorder{}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			tools := json.RawMessage(`[
				{"type":"namespace","name":"mcp__one","tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}}]},
				{"type":"namespace","name":"mcp__two","tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}}]},
				{"type":"custom","name":"exec","format":{"type":"text"}}
			]`)
			history := []json.RawMessage{
				json.RawMessage(`{"role":"developer","content":"initial developer"}`),
				json.RawMessage(`{"role":"user","content":"use tools"}`),
			}
			calls := json.RawMessage(`[
				{"index":0,"id":"call_one","type":"function","function":{"name":"mcp__one__lookup","arguments":"{\"n\":9007199254740993}"}},
				{"index":1,"id":"call_two","type":"function","function":{"name":"mcp__two__lookup","arguments":"{\"n\":2}"}},
				{"index":2,"id":"call_exec","type":"function","function":{"name":"exec","arguments":"{\"input\":\"echo test\\n\"}"}}
			]`)
			// SSE data is line-delimited; keep each synthetic chunk on one line.
			var compactCalls bytes.Buffer
			require.NoError(t, json.Compact(&compactCalls, calls))
			calls = json.RawMessage(compactCalls.Bytes())
			for round := 0; round < 2; round++ {
				message, finish := `{"role":"assistant","tool_calls":`+string(calls)+`}`, "tool_calls"
				if round == 1 {
					message, finish = `{"role":"assistant","content":"ok"}`, "stop"
				}
				responseBody := `{"id":"chatcmpl_agents","model":"Agents-A1","choices":[{"index":0,"message":` + message + `,"finish_reason":"` + finish + `"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`
				if stream {
					responseBody = "data: " + `{"id":"chatcmpl_agents","model":"Agents-A1","choices":[{"index":0,"delta":` + message + `,"finish_reason":null}]}` + "\n\n" +
						"data: " + `{"id":"chatcmpl_agents","model":"Agents-A1","choices":[{"index":0,"delta":{},"finish_reason":"` + finish + `"}]}` + "\n\n" +
						"data: " + `{"id":"chatcmpl_agents","model":"Agents-A1","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2}}` + "\n\ndata: [DONE]\n\n"
				}
				resp := newOpenAIRejectedFieldTestResponse(http.StatusOK, responseBody)
				if stream {
					resp.Header.Set("Content-Type", "text/event-stream")
				}
				upstream.responses = append(upstream.responses, resp)
				body, err := json.Marshal(map[string]any{"model": "Auto", "instructions": "main", "input": history, "tools": tools, "stream": stream, "reasoning": map[string]any{"effort": "high"}, "parallel_tool_calls": true})
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				result, err := svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, rec.Code)
				require.Equal(t, "Auto", result.Model)
				require.Equal(t, "Agents-A1", result.BillingModel)
				require.Equal(t, "Agents-A1", result.UpstreamModel)
				require.Equal(t, stream, result.Stream)
				require.Equal(t, 3, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
				require.Len(t, upstream.bodies, round+1)
				require.Equal(t, "http://upstream.example/v1/chat/completions", upstream.lastReq.URL.String())

				// Compare the whole outgoing request against the existing converter,
				// changing only the expected model/system block and stream usage flag.
				var original apicompat.ResponsesRequest
				require.NoError(t, json.Unmarshal(body, &original))
				want, err := apicompat.ResponsesToChatCompletionsRequest(&original)
				require.NoError(t, err)
				want.Model = "Agents-A1"
				instruction := "main\n\ninitial developer"
				if round == 1 {
					instruction += "\n\nlate developer"
				}
				text, err := json.Marshal(instruction)
				require.NoError(t, err)
				messages := []apicompat.ChatMessage{{Role: "system", Content: text}}
				for _, message := range want.Messages {
					if message.Role != "system" {
						messages = append(messages, message)
					}
				}
				want.Messages = messages
				if stream {
					want.StreamOptions = &apicompat.ChatStreamOptions{IncludeUsage: true}
				}
				wantBody, err := json.Marshal(want)
				require.NoError(t, err)
				require.JSONEq(t, string(wantBody), string(upstream.lastBody))

				output := gjson.Get(rec.Body.String(), "output")
				if stream {
					events := collectSSEDataPayloads(t, rec.Body.String())
					completed := findSSEEvent(t, events, "response.completed", "")
					output = gjson.Get(completed, "response.output")
					require.Contains(t, rec.Body.String(), "data: [DONE]")
				}
				if round == 1 {
					require.Equal(t, "ok", output.Get("0.content.0.text").String())
					continue
				}
				require.Len(t, output.Array(), 3)
				expected := []struct{ id, namespace, name, arguments string }{
					{"call_one", "mcp__one", "lookup", `{"n":9007199254740993}`},
					{"call_two", "mcp__two", "lookup", `{"n":2}`},
					{"call_exec", "", "exec", ""},
				}
				for i, call := range output.Array() {
					require.Equal(t, expected[i].id, call.Get("call_id").String())
					require.Equal(t, expected[i].name, call.Get("name").String())
					replyType := "function_call_output"
					if i == 2 {
						require.Equal(t, "custom_tool_call", call.Get("type").String())
						require.Equal(t, "echo test\n", call.Get("input").String())
						replyType = "custom_tool_call_output"
					} else {
						require.Equal(t, "function_call", call.Get("type").String())
						require.Equal(t, expected[i].namespace, call.Get("namespace").String())
						require.Equal(t, expected[i].arguments, call.Get("arguments").String())
					}
					history = append(history, json.RawMessage(call.Raw))
					if i == 0 {
						history = append(history, json.RawMessage(`{"role":"developer","content":"late developer"}`))
					}
					reply, err := json.Marshal(map[string]any{"type": replyType, "call_id": expected[i].id, "output": "result " + expected[i].id})
					require.NoError(t, err)
					history = append(history, reply)
				}
			}
		})
	}
}
