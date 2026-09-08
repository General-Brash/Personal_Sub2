//go:build unit

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
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestR01OpenAIForwardToUnifiedUsageDirectUpstreamID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, tc := range []struct{ name, configured, value, want string }{
				{"configured", "X-Vendor-Request-Id", " vendor-request ", "vendor-request"},
				{"not-configured", "", "vendor-request", ""},
				{"missing-response-header", "X-Vendor-Request-Id", "", ""},
				{"utf8-boundary", "X-Vendor-Request-Id", strings.Repeat("界", 60), strings.Repeat("界", 42)},
			} {
				t.Run(fmt.Sprintf("passthrough=%v/stream=%v/%s", passthrough, stream, tc.name), func(t *testing.T) {
					usageRepo := &openAIRecordUsageLogRepoStub{}
					billingRepo := &openAIRecordUsageBillingRepoStub{}
					svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
					headers := http.Header{}
					headers.Set("Content-Type", "application/json")
					headers.Set("X-Request-Id", "transport-request")
					if tc.value != "" {
						headers.Set("X-Vendor-Request-Id", tc.value)
					}
					reply := `{"id":"resp_r01","object":"response","status":"completed","model":"gpt-5.1","output":[],"usage":{"input_tokens":12,"output_tokens":7,"total_tokens":19}}`
					if stream {
						headers.Set("Content-Type", "text/event-stream")
						reply = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + reply + "}\n\ndata: [DONE]\n\n"
					}
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: headers, Body: io.NopCloser(strings.NewReader(reply))}}
					svc.httpUpstream = upstream
					account := &Account{ID: 301, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
						Credentials: map[string]any{"api_key": "local-fixture-key", "base_url": "https://api.openai.com"},
						Extra:       map[string]any{"openai_passthrough": passthrough, AccountExtraUpstreamRequestIDHeader: tc.configured}}
					body := []byte(fmt.Sprintf(`{"model":"gpt-5.1","stream":%t,"input":"local fixture"}`, stream))
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
					result, err := svc.Forward(context.Background(), c, account, body)
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Len(t, upstream.requests, 1, "attribution must not replay an upstream request")
					requestID := result.RequestID
					err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, APIKey: &APIKey{ID: 101}, User: &User{ID: 201}, Account: account})
					require.NoError(t, err)
					log := requireUnifiedUsageLog(t, billingRepo, usageRepo)
					require.Equal(t, requestID, log.RequestID, "the attribution field must not replace the billing request identity")
					if tc.want == "" {
						require.Nil(t, log.UpstreamRequestID)
					} else {
						require.NotNil(t, log.UpstreamRequestID)
						require.Equal(t, tc.want, *log.UpstreamRequestID)
						require.True(t, utf8.ValidString(*log.UpstreamRequestID))
					}
					require.Equal(t, 1, billingRepo.calls)
				})
			}
		}
	}
}

func TestR01AnthropicForwardToUnifiedUsageDirectUpstreamID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	usageRepo := &openAIRecordUsageLogRepoStub{}
	billingRepo := &openAIRecordUsageBillingRepoStub{}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	headers := http.Header{"Content-Type": []string{"application/json"}}
	headers.Set("X-Request-Id", "anthropic-transport-request")
	headers.Set("X-Vendor-Request-Id", "anthropic-vendor-request")
	svc.httpUpstream = &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: headers, Body: io.NopCloser(strings.NewReader(`{"id":"msg_r01","type":"message","content":[],"usage":{"input_tokens":12,"output_tokens":7}}`))}}
	account := newAnthropicAPIKeyAccountForTest()
	account.Extra[AccountExtraUpstreamRequestIDHeader] = "X-Vendor-Request-Id"
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	body := []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"local fixture"}]}`)
	result, err := svc.forwardAnthropicAPIKeyPassthrough(context.Background(), c, account, body, "claude-sonnet-4", "claude-sonnet-4", false, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NoError(t, svc.RecordUsage(context.Background(), &RecordUsageInput{Result: result, APIKey: &APIKey{ID: 102}, User: &User{ID: 202}, Account: account}))
	log := requireUnifiedUsageLog(t, billingRepo, usageRepo)
	require.Equal(t, "anthropic-transport-request", log.RequestID)
	require.NotNil(t, log.UpstreamRequestID)
	require.Equal(t, "anthropic-vendor-request", *log.UpstreamRequestID)
	require.Equal(t, 1, billingRepo.calls)
}

func TestR01UsageDirectHeadersAreSeparateFromProtocolAndWSHeaders(t *testing.T) {
	for _, ws := range []bool{false, true} {
		t.Run(fmt.Sprintf("ws=%v", ws), func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{}
			billingRepo := &openAIRecordUsageBillingRepoStub{}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			account := &Account{ID: 303, Type: AccountTypeAPIKey, Extra: map[string]any{AccountExtraUpstreamRequestIDHeader: "X-Vendor-Request-Id"}}
			result := &OpenAIForwardResult{RequestID: "partial-or-turn", Model: "gpt-5.1", Stream: true, OpenAIWSMode: ws,
				Usage:           OpenAIUsage{InputTokens: 12, OutputTokens: 7},
				UpstreamHeaders: http.Header{"X-Vendor-Request-Id": []string{"direct-response"}},
				ResponseHeaders: http.Header{"X-Vendor-Request-Id": []string{"protocol-or-handshake"}}}
			require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result, APIKey: &APIKey{ID: 103}, User: &User{ID: 203}, Account: account}))
			log := requireUnifiedUsageLog(t, billingRepo, usageRepo)
			if ws {
				require.Nil(t, log.UpstreamRequestID)
			} else {
				require.NotNil(t, log.UpstreamRequestID)
				require.Equal(t, "direct-response", *log.UpstreamRequestID)
			}
			require.Equal(t, "partial-or-turn", log.RequestID)
			require.Equal(t, 1, billingRepo.calls)
		})
	}
}
