//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAgentsA1SystemMessages(t *testing.T) {
	toolHistory := `{"role":"user","content":"run"},
		{"role":"assistant","reasoning_content":"keep reasoning","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"value\"}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"tool output"},
		{"role":"system","content":"later instruction"},
		{"role":"user","content":"continue"}`
	tests := []struct {
		name     string
		messages string
		want     string // Empty means the complete request must remain byte-for-byte unchanged.
	}{
		{
			name:     "strings preserve whitespace and order",
			messages: `[{"role":"system","content":" first\n指令 "},{"role":"system","content":"second"},{"role":"system","content":"third"},{"role":"user","content":"hello"}]`,
			want:     `[{"role":"system","content":" first\n指令 \n\nsecond\n\nthird"},{"role":"user","content":"hello"}]`,
		},
		{
			name:     "mixed string and text arrays",
			messages: `[{"role":"system","content":[{"type":"text","text":"first"},{"type":"text","text":"第二\n行"}]},{"role":"system","content":"third"},{"role":"system","content":[{"type":"text","text":"fourth"}]}]`,
			want:     `[{"role":"system","content":"first\n\n第二\n行\n\nthird\n\nfourth"}]`,
		},
		{
			name:     "single system string unchanged",
			messages: `[{"role":"system","content":" first\n "},{"role":"user","content":"hello"}]`,
		},
		{
			name:     "single system array unchanged",
			messages: `[{"role":"system","content":[{"type":"text","text":"first"},{"type":"text","text":"second"}]},{"role":"user","content":"hello"}]`,
		},
		{
			name:     "empty history unchanged",
			messages: `[]`,
		},
		{
			name:     "move late systems to front",
			messages: `[{"role":"user","content":"hello"},{"role":"system","content":"first"},{"role":"system","content":"second"}]`,
			want:     `[{"role":"system","content":"first\n\nsecond"},{"role":"user","content":"hello"}]`,
		},
		{
			name:     "nonconsecutive systems preserve instruction order",
			messages: `[{"role":"system","content":"first"},{"role":"user","content":"hello"},{"role":"system","content":"later"}]`,
			want:     `[{"role":"system","content":"first\n\nlater"},{"role":"user","content":"hello"}]`,
		},
		{
			name:     "tool calls outputs unchanged with later system merged",
			messages: `[{"role":"system","content":"first"},{"role":"system","content":"second"},` + toolHistory + `]`,
			want:     `[{"role":"system","content":"first\n\nsecond\n\nlater instruction"},` + strings.Replace(toolHistory, `{"role":"system","content":"later instruction"},`, "", 1) + `]`,
		},
		{
			name:     "single late system moves to front",
			messages: `[{"role":"user","content":"hello"},{"role":"system","content":"later"}]`,
			want:     `[{"role":"system","content":"later"},{"role":"user","content":"hello"}]`,
		},
		{
			name:     "nontext content is not discarded",
			messages: `[{"role":"system","content":"first"},{"role":"system","content":[{"type":"text","text":"second"},{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]}]`,
		},
		{
			name:     "unknown content is not discarded",
			messages: `[{"role":"system","content":"first"},{"role":"system","content":{"unexpected":"second"}}]`,
		},
		{
			name:     "system metadata is not discarded",
			messages: `[{"role":"system","content":"first"},{"role":"system","name":"named","content":"second"}]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &apicompat.ChatCompletionsRequest{Model: "Agents-A1"}
			require.NoError(t, json.Unmarshal([]byte(tc.messages), &req.Messages))
			before, err := json.Marshal(req)
			require.NoError(t, err)

			normalizeAgentsA1SystemMessages(req)

			if tc.want == "" {
				after, err := json.Marshal(req)
				require.NoError(t, err)
				require.Equal(t, string(before), string(after))
				return
			}
			messages, err := json.Marshal(req.Messages)
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(messages))
		})
	}
}

func TestNormalizeAgentsA1SystemMessages_OtherModelsUnchanged(t *testing.T) {
	for _, model := range []string{"Atria", "Auto", "gpt-5.4", "agents-a1", "AGENTS-A1", "vendor/Agents-A1", "Agents-A1-preview", "Agents-A1 ", ""} {
		t.Run(model, func(t *testing.T) {
			req := &apicompat.ChatCompletionsRequest{Model: model}
			require.NoError(t, json.Unmarshal([]byte(`[{"role":"system","content":"first"},{"role":"system","content":[{"type":"text","text":"second"}]},{"role":"user","content":"hello"}]`), &req.Messages))
			before, err := json.Marshal(req)
			require.NoError(t, err)

			normalizeAgentsA1SystemMessages(req)

			after, err := json.Marshal(req)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after))
		})
	}
}

func TestForwardResponses_AgentsA1SystemMessagesAfterModelMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name           string
		requestedModel string
		mappedModel    string
		merge          bool
	}{
		{name: "auto maps to agents", requestedModel: "Auto", mappedModel: "Agents-A1", merge: true},
		{name: "agents direct", requestedModel: "Agents-A1", mappedModel: "Agents-A1", merge: true},
		{name: "auto maps to atria", requestedModel: "Auto", mappedModel: "Atria"},
		{name: "agents maps away to atria", requestedModel: "Agents-A1", mappedModel: "Atria"},
		{name: "atria direct", requestedModel: "Atria", mappedModel: "Atria"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"` + tc.requestedModel + `","instructions":" first\n指令 ","input":[
				{"role":"developer","content":[{"type":"input_text","text":"second"},{"type":"input_text","text":"third"}]},
				{"role":"user","content":"hello"},
				{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"value\"}"},
				{"type":"function_call_output","call_id":"call_1","output":"tool output"},
				{"role":"system","content":"later instruction"},
				{"role":"user","content":"continue"}
			],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}],"reasoning":{"effort":"high"},"parallel_tool_calls":true,"stream":false}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")

			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(
					`{"id":"chatcmpl_agents_a1","object":"chat.completion","model":"` + tc.mappedModel + `","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
				)),
			}}
			svc := &OpenAIGatewayService{
				cfg:          rawChatCompletionsTestConfig(),
				httpUpstream: upstream,
			}
			account := forceChatResponsesFallbackAccount()
			if tc.requestedModel != tc.mappedModel {
				account.Credentials["model_mapping"] = map[string]any{tc.requestedModel: tc.mappedModel}
			}

			// Compare the complete outgoing request with the existing converter;
			// only Agents-A1's system messages may differ after model mapping.
			var responsesReq apicompat.ResponsesRequest
			require.NoError(t, json.Unmarshal(body, &responsesReq))
			want, err := apicompat.ResponsesToChatCompletionsRequest(&responsesReq)
			require.NoError(t, err)
			want.Model = tc.mappedModel
			require.Len(t, want.Messages, 7)
			require.Equal(t, "high", want.ReasoningEffort)
			require.NotNil(t, want.ParallelToolCalls)
			require.True(t, *want.ParallelToolCalls)
			require.Len(t, want.Tools, 1)
			require.Len(t, want.Messages[3].ToolCalls, 1)
			require.Equal(t, "call_1", want.Messages[3].ToolCalls[0].ID)
			require.Equal(t, "call_1", want.Messages[4].ToolCallID)
			require.JSONEq(t, `"tool output"`, string(want.Messages[4].Content))
			if tc.merge {
				want.Messages = []apicompat.ChatMessage{
					{Role: "system", Content: json.RawMessage(`" first\n指令 \n\nsecond\n\nthird\n\nlater instruction"`)},
					want.Messages[2], want.Messages[3], want.Messages[4], want.Messages[6],
				}
			}
			wantBody, err := json.Marshal(want)
			require.NoError(t, err)

			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tc.mappedModel, result.UpstreamModel)
			require.NotNil(t, upstream.lastReq)
			require.Equal(t, "http://upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
			require.Equal(t, string(wantBody), string(upstream.lastBody))
		})
	}
}
