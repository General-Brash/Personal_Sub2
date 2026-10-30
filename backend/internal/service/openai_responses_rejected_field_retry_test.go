package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestOpenAIResponsesRejectedFieldRetryStateRejectsDuplicateBodyAndCap(t *testing.T) {
	initialBody := []byte(`{"model":"gpt-5.5"}`)
	state := newOpenAIResponsesRejectedFieldRetryState(initialBody)

	require.False(t, state.Allow(initialBody))
	for attempt := 0; attempt < maxOpenAIResponsesRejectedFieldRetries; attempt++ {
		nextBody := []byte(fmt.Sprintf(`{"model":"gpt-5.5","variant":%d}`, attempt))
		require.True(t, state.Allow(nextBody))
		require.False(t, state.Allow(nextBody))
	}
	require.False(t, state.Allow([]byte(`{"model":"gpt-5.5","variant":"overflow"}`)))
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRejectsAmbiguousErrors(t *testing.T) {
	tests := []struct {
		name         string
		body         []byte
		responseBody []byte
	}{
		{
			name:         "namespace belongs to message",
			body:         []byte(`{"input":[{"type":"message","namespace":"keep"}]}`),
			responseBody: []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: 'input[0].namespace'.","param":"input[0].namespace"}}`),
		},
		{
			name:         "max output tokens only mentioned",
			body:         []byte(`{"max_output_tokens":4096}`),
			responseBody: []byte(`{"error":{"code":"invalid_request_error","message":"max_output_tokens must be positive","param":"max_output_tokens"}}`),
		},
		{
			name:         "structured param overrides namespace mention",
			body:         []byte(`{"input":[{"type":"function_call","namespace":"keep","arguments":"{}"}]}`),
			responseBody: []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: 'input[0].namespace'.","param":"tools"}}`),
		},
		{
			name:         "nested max output tokens param is not top level",
			body:         []byte(`{"max_output_tokens":4096,"input":[{"type":"message","content":{"max_output_tokens":"keep"}}]}`),
			responseBody: []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: input[0].content.max_output_tokens","param":"input[0].content.max_output_tokens"}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, tt.body, tt.responseBody)
			require.NoError(t, err)
			require.False(t, changed)
			require.Nil(t, retryBody)
		})
	}
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyFindsNamespacePathInMessage(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call","namespace":"keep","arguments":"{}"},{"type":"function_call","namespace":"remove","arguments":"{}"}]}`)
	responseBody := []byte(`{"error":{"code":"unknown_parameter","message":"input[0] was accepted; Unknown parameter: 'input[1].namespace'."}}`)

	retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "keep", gjson.GetBytes(retryBody, "input.0.namespace").String())
	require.False(t, gjson.GetBytes(retryBody, "input.1.namespace").Exists())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyBindsNamespacePathToRejectionPhrase(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call","namespace":"keep","arguments":"{}"},{"type":"function_call","namespace":"remove","arguments":"{}"}]}`)
	responseBody := []byte(`{"error":{"code":"unknown_parameter","message":"input[0].namespace is supported; Unknown parameter: input[1].namespace."}}`)

	retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "keep", gjson.GetBytes(retryBody, "input.0.namespace").String())
	require.False(t, gjson.GetBytes(retryBody, "input.1.namespace").Exists())
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyDoesNotTreatMaxOutputTokensSuggestionAsRejection(t *testing.T) {
	body := []byte(`{"max_tokens":4096,"max_output_tokens":2048}`)
	responseBody := []byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter: max_tokens. Use max_output_tokens instead."}}`)

	retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.False(t, changed)
	require.Nil(t, retryBody)
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyBindsMaxOutputTokensToRejectionPhrase(t *testing.T) {
	body := []byte(`{"max_output_tokens":2048}`)
	responseBody := []byte(`{"error":{"code":"unsupported_parameter","message":"Unsupported parameter: max_output_tokens."}}`)

	retryBody, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)

	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(retryBody, "max_output_tokens").Exists())
}

func TestOpenAIGatewayService_APIKeyPreservesToolCallNamespacesBeforeFirstForward(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"function_call","name":"first","namespace":"mcp__ableton","arguments":"{}"},{"type":"custom_tool_call","name":"second","namespace":"mcp__other","input":"{}"}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
	}}

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		newOpenAIRejectedFieldTestAccount(),
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, "mcp__ableton", gjson.GetBytes(upstream.bodies[0], "input.0.namespace").String())
	require.Equal(t, "mcp__other", gjson.GetBytes(upstream.bodies[0], "input.1.namespace").String())
}

func TestOpenAIGatewayService_OpenAIHTTPStripsInputNamespacesBeforeFirstForward(t *testing.T) {
	accounts := []struct {
		name    string
		account *Account
	}{
		{name: "oauth", account: newOpenAIOAuthNamespaceTestAccount()},
		{name: "apikey", account: newOpenAIRejectedFieldTestAccount()},
	}
	for _, tt := range accounts {
		for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
			t.Run(tt.name+path, func(t *testing.T) {
				body := []byte(`{"model":"gpt-5.5","stream":false,"instructions":"test","input":[{"type":"message","role":"user","namespace":"remove","content":[{"type":"input_text","text":"hello","namespace":"nested-keep"}]}]}`)
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"id":"resp_namespace_ok","output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
				}}
				c := newOpenAIRejectedFieldTestContext(body)
				c.Request.URL.Path = path

				result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
					context.Background(),
					c,
					tt.account,
					body,
				)

				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, upstream.bodies, 1, "namespace must be removed before the first upstream request")
				require.False(t, gjson.GetBytes(upstream.bodies[0], "input.0.namespace").Exists())
				require.Equal(t, "nested-keep", gjson.GetBytes(upstream.bodies[0], "input.0.content.0.namespace").String())
			})
		}
	}
}

func TestOpenAIGatewayService_RetriesExplicitMaxOutputTokensRejection(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"max_output_tokens":4096,"input":[{"type":"message","role":"user","content":{"max_output_tokens":"keep"}}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"unsupported_parameter","message":"Unsupported parameter: max_output_tokens","param":"max_output_tokens","type":"invalid_request_error"}}`),
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
	}}

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		newOpenAIRejectedFieldTestAccount(),
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, int64(4096), gjson.GetBytes(upstream.bodies[0], "max_output_tokens").Int())
	require.False(t, gjson.GetBytes(upstream.bodies[1], "max_output_tokens").Exists())
	require.Equal(t, "keep", gjson.GetBytes(upstream.bodies[1], "input.0.content.max_output_tokens").String())
}

func TestOpenAIGatewayService_RejectedFieldRetryPreservesToolCallNamespaces(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"max_output_tokens":2048,"input":[{"type":"function_call","name":"first","namespace":"mcp__ableton","arguments":"{}"},{"type":"custom_tool_call","name":"second","namespace":"mcp__other","input":"{}"}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"unsupported_parameter","message":"Unsupported parameter: max_output_tokens","param":"max_output_tokens"}}`),
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
	}}

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		newOpenAIRejectedFieldTestAccount(),
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2)
	for _, forwardedBody := range upstream.bodies {
		require.Equal(t, "mcp__ableton", gjson.GetBytes(forwardedBody, "input.0.namespace").String())
		require.Equal(t, "mcp__other", gjson.GetBytes(forwardedBody, "input.1.namespace").String())
	}
	require.Equal(t, int64(2048), gjson.GetBytes(upstream.bodies[0], "max_output_tokens").Int())
	require.False(t, gjson.GetBytes(upstream.bodies[1], "max_output_tokens").Exists())
}

func newOpenAIRejectedFieldTestService(upstream *httpUpstreamRecorder) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
		httpUpstream: upstream,
	}
}

func newOpenAIRejectedFieldTestContext(body []byte) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "curl/8.0")
	return c
}

func newOpenAIRejectedFieldTestAccount() *Account {
	return &Account{
		ID:          5107,
		Name:        "responses-compatible",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://compat.example",
		},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode:      string(openai_compat.ResponsesSupportModeAuto),
			openai_compat.ExtraKeyResponsesSupported: true,
		},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func newOpenAIOAuthNamespaceTestAccount() *Account {
	return &Account{
		ID:          5108,
		Name:        "openai-oauth-namespace",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-account",
		},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func newOpenAIRejectedFieldTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestOpenAIGatewayService_APIKeyRetriesOnlyExplicitlyRejectedNamespace(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"function_call","name":"first","namespace":"mcp__ableton","call_id":"call_1","arguments":"{}"},{"type":"function_call","name":"second","namespace":"mcp__other","call_id":"call_2","arguments":"{}"}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"unsupported_parameter","message":"Unsupported parameter: input[1].namespace","param":"input[1].namespace"}}`),
		newOpenAIRejectedFieldTestResponse(http.StatusOK, namespaceForwardOKResponse),
	}}
	_, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), newOpenAIRejectedFieldTestContext(body), newOpenAIRejectedFieldTestAccount(), body,
	)
	require.NoError(t, err)
	require.Len(t, upstream.bodies, 2)
	require.Equal(t, "mcp__ableton", gjson.GetBytes(upstream.bodies[0], "input.0.namespace").String())
	require.Equal(t, "mcp__other", gjson.GetBytes(upstream.bodies[0], "input.1.namespace").String())
	require.JSONEq(t, gjson.GetBytes(upstream.bodies[0], "input.0").Raw, gjson.GetBytes(upstream.bodies[1], "input.0").Raw)
	require.False(t, gjson.GetBytes(upstream.bodies[1], "input.1.namespace").Exists())
	require.Equal(t, "second", gjson.GetBytes(upstream.bodies[1], "input.1.name").String())
	require.Equal(t, "call_2", gjson.GetBytes(upstream.bodies[1], "input.1.call_id").String())
}

func TestOpenAIGatewayService_AzureResourceRecoveryBoundaries(t *testing.T) {
	foreignError := `{"error":{"type":"invalid_request_error","message":"` + azureForeignResourceTestError + `"}}`
	okBody := `{"id":"resp_azure","output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`
	for _, passthrough := range []bool{false, true} {
		for _, tt := range []struct {
			name        string
			status      int
			response    string
			stream      bool
			ids         bool
			repeatError bool
			wantCalls   int
			wantError   bool
		}{
			{"JSON recovery", http.StatusBadRequest, foreignError, false, true, false, 2, false},
			{"SSE recovery", http.StatusBadRequest, foreignError, true, true, false, 2, false},
			{"persistent mismatch stops", http.StatusBadRequest, foreignError, false, true, true, 2, true},
			{"masked JSON recovery", http.StatusBadRequest, maskedForeignResourceTestResponse, false, true, false, 2, false},
			{"masked SSE recovery", http.StatusBadRequest, maskedForeignResourceTestResponse, true, true, false, 2, false},
			{"persistent masked mismatch stops", http.StatusBadRequest, maskedForeignResourceTestResponse, false, true, true, 2, true},
			{"masked permission error stays unchanged", http.StatusForbidden, maskedForeignResourceTestResponse, false, true, false, 1, true},
			{"masked no changes means no retry", http.StatusBadRequest, maskedForeignResourceTestResponse, false, false, false, 1, true},
			{"permission error stays unchanged", http.StatusForbidden, foreignError, false, true, false, 1, true},
			{"non Azure resource stays unchanged", http.StatusBadRequest, `{"error":{"message":"This object was created under a different subscription; resource access is denied"}}`, false, true, false, 1, true},
			{"no changes means no retry", http.StatusBadRequest, foreignError, false, false, false, 1, true},
			{"normal success retains IDs", http.StatusOK, okBody, false, true, false, 1, false},
		} {
			t.Run(fmt.Sprintf("passthrough=%v/%s", passthrough, tt.name), func(t *testing.T) {
				idField := ""
				if tt.ids {
					idField = `"id":"msg_resource_a",`
				}
				body := []byte(fmt.Sprintf(`{"model":"gpt-5.5","stream":%t,"previous_response_id":"resp_keep","metadata":{"nonce":9007199254740993},"input":[{%s"type":"message","role":"user","content":[{"type":"input_text","text":"continue","nonce":9007199254740993}]},{"type":"function_call","name":"run","call_id":"call_keep","arguments":"{\"id\":\"business\",\"nonce\":9007199254740993}"},{"type":"function_call_output","call_id":"call_keep","output":"tool output"}]}`, tt.stream, idField))
				firstBody := &passthroughCloseTrackingReadCloser{Reader: strings.NewReader(tt.response)}
				first := newOpenAIRejectedFieldTestResponse(tt.status, tt.response)
				first.Body = firstBody
				second := newOpenAIRejectedFieldTestResponse(http.StatusOK, okBody)
				if tt.repeatError {
					second = newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, tt.response)
				} else if tt.stream {
					second.Header.Set("Content-Type", "text/event-stream")
					second.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":" + okBody + "}\n\ndata: [DONE]\n\n"))
				}
				upstream := &httpUpstreamRecorder{responses: []*http.Response{first, second, newOpenAIRejectedFieldTestResponse(http.StatusOK, okBody)}}
				account := newOpenAIRejectedFieldTestAccount()
				account.Extra["openai_passthrough"] = passthrough
				c := newOpenAIRejectedFieldTestContext(body)
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				result, err := newOpenAIRejectedFieldTestService(upstream).Forward(context.Background(), c, account, body)
				if tt.wantError {
					require.Error(t, err)
					require.Nil(t, result)
				} else {
					require.NoError(t, err)
					require.NotNil(t, result)
					require.EqualValues(t, 1, result.Usage.InputTokens)
				}
				require.Len(t, upstream.bodies, tt.wantCalls)
				require.True(t, firstBody.closed)
				require.Equal(t, tt.ids, gjson.GetBytes(upstream.bodies[0], "input.0.id").Exists())
				for i, req := range upstream.requests {
					require.Equal(t, upstream.requests[0].URL.String(), req.URL.String())
					require.Equal(t, "Bearer sk-test", req.Header.Get("Authorization"))
					require.EqualValues(t, len(upstream.bodies[i]), req.ContentLength)
					require.Equal(t, "9007199254740993", gjson.GetBytes(upstream.bodies[i], "input.0.content.0.nonce").Raw)
					require.Equal(t, "call_keep", gjson.GetBytes(upstream.bodies[i], "input.1.call_id").String())
					require.Equal(t, "call_keep", gjson.GetBytes(upstream.bodies[i], "input.2.call_id").String())
				}
				if tt.wantCalls == 2 {
					require.False(t, gjson.GetBytes(upstream.bodies[1], "input.0.id").Exists())
					expected, err := sjson.DeleteBytes(upstream.bodies[0], "input.0.id")
					require.NoError(t, err)
					require.Equal(t, string(expected), string(upstream.bodies[1]))
				}
			})
		}
	}
}

func TestOpenAIGatewayService_AzureResourceRecoveryRetriesOnlyOnce(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"message","id":"msg_resource_a","role":"user","content":"continue"},{"type":"compaction","encrypted_content":"opaque"}]}`)
	compactionError := `{"error":{"type":"invalid_request_error","message":"` + maskedForeignResourceTestError + ` The compaction item is from another resource."}}`
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, maskedForeignResourceTestResponse),
				newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, compactionError),
				newOpenAIRejectedFieldTestResponse(http.StatusOK, namespaceForwardOKResponse),
			}}
			account := newOpenAIRejectedFieldTestAccount()
			account.Credentials["base_url"] = "https://api.openai.com"
			account.Extra["openai_passthrough"] = passthrough
			c := newOpenAIRejectedFieldTestContext(body)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			result, err := newOpenAIRejectedFieldTestService(upstream).Forward(context.Background(), c, account, body)
			require.Error(t, err)
			require.Nil(t, result)
			require.Len(t, upstream.bodies, 2)
			require.False(t, gjson.GetBytes(upstream.bodies[1], "input.0.id").Exists())
			require.Equal(t, "opaque", gjson.GetBytes(upstream.bodies[1], "input.1.encrypted_content").String())

			// The second error could clean the retained compaction; the one-retry
			// guard, not an unchanged payload, must prevent a third request.
			retryBody, changed, err := normalizeOpenAIResponsesAzureResourceRetryBody(
				http.StatusBadRequest, upstream.bodies[1], []byte(compactionError), upstream.requests[1].URL,
			)
			require.NoError(t, err)
			require.True(t, changed)
			require.False(t, gjson.GetBytes(retryBody, "input.1.encrypted_content").Exists())
		})
	}
}
