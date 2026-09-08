package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSupportsOpenAIReasoningEffortMaxAstraVariants(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{name: "public alias", model: "gpt-6", want: true},
		{name: "astra base", model: "gpt-6-astra", want: true},
		{name: "astra dated", model: "gpt-6-astra-2026-08-31", want: true},
		{name: "provider prefixed dated", model: "openai/gpt-6-astra-2026-08-31", want: true},
		{name: "case underscore provider variant", model: " Azure/OpenAI/GPT_6_ASTRA_2026_08_31 ", want: true},
		{name: "other gpt6 family", model: "gpt-6-orion", want: false},
		{name: "astra lookalike", model: "gpt-6-astral", want: false},
		{name: "non target openai", model: "gpt-5.5", want: false},
		{name: "bare astra", model: "provider/astra", want: false},
		{name: "empty", model: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, supportsOpenAIReasoningEffortMax(tt.model))
			wantEffort := "xhigh"
			if tt.want {
				wantEffort = "max"
			}
			require.Equal(t, wantEffort, normalizeOpenAIReasoningEffortForModel(" MAX ", tt.model))
		})
	}
}

func TestAstraReasoningEffortExtractionPreservesMaxOnlyForAstra(t *testing.T) {
	body := []byte(`{"reasoning":{"effort":"max"}}`)

	for _, model := range []string{
		"gpt-6-astra",
		"gpt-6-astra-2026-08-31",
		"azure/openai/gpt-6-astra-2026-08-31",
	} {
		effort := extractOpenAIReasoningEffortFromBody(body, model)
		require.NotNil(t, effort, model)
		require.Equal(t, "max", *effort, model)
	}

	effort := extractOpenAIReasoningEffortFromBody(body, "gpt-6-orion")
	require.NotNil(t, effort)
	require.Equal(t, "xhigh", *effort)

	require.Nil(t, extractOpenAIReasoningEffortFromBody([]byte(`{"reasoning":{"effort":"none"}}`), "gpt-6-astra"))
	require.Equal(t, "high", normalizeOpenAIReasoningEffortForModel("high", "gpt-6-astra"))
}

func TestAstraReasoningPolicyNoneSourceAndBounds(t *testing.T) {
	mappings, err := NormalizeReasoningEffortMappings(PlatformOpenAI, []ReasoningEffortMapping{
		{From: " NONE ", To: "low"},
	})
	require.NoError(t, err)
	require.Equal(t, []ReasoningEffortMapping{{From: "none", To: "low"}}, mappings)

	lower, changed, err := ApplyOpenAIReasoningEffortPolicyWithOverLimit(
		[]byte(`{"model":"gpt-6-astra","reasoning":{"effort":"xhigh"}}`),
		"max",
		nil,
		ReasoningEffortOverLimitDeny,
	)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, "xhigh", gjson.GetBytes(lower, "reasoning.effort").String())

	downgraded, changed, err := ApplyOpenAIReasoningEffortPolicyWithOverLimit(
		[]byte(`{"model":"gpt-6-astra","reasoning":{"effort":"max"}}`),
		"xhigh",
		nil,
		ReasoningEffortOverLimitDowngrade,
	)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "xhigh", gjson.GetBytes(downgraded, "reasoning.effort").String())

	original := []byte(`{"model":"gpt-6-astra","reasoning":{"effort":"max"}}`)
	denied, changed, err := ApplyOpenAIReasoningEffortPolicyWithOverLimit(
		original,
		"xhigh",
		nil,
		ReasoningEffortOverLimitDeny,
	)
	var overLimitErr *ReasoningEffortOverLimitError
	require.ErrorAs(t, err, &overLimitErr)
	require.Equal(t, "max", overLimitErr.Requested)
	require.Equal(t, "xhigh", overLimitErr.Max)
	require.False(t, changed)
	require.Equal(t, original, denied)

	_, err = NormalizeReasoningEffortMappings(PlatformOpenAI, []ReasoningEffortMapping{{From: "low", To: "none"}})
	require.ErrorContains(t, err, "empty or unknown")
	_, err = normalizeMaxReasoningEffortForPlatform(PlatformOpenAI, "none")
	require.ErrorContains(t, err, "not supported")
}
func TestOpenAIGatewayServiceForwardPreservesAstraMaxEffort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"gpt-6-astra", "gpt-6-astra-2026-08-31"} {
		t.Run(model, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{
				resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":1,"output_tokens":2}}`)),
				},
			}
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.Enabled = false
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			account := &Account{
				ID:          71,
				Name:        "astra-apikey",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://example.com"},
				Extra:       map[string]any{"use_responses_api": true},
			}
			body := []byte(`{"model":"` + model + `","stream":false,"reasoning":{"effort":"max"},"input":"hello"}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)

			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, model, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, "max", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
			require.NotNil(t, result.ReasoningEffort)
			require.Equal(t, "max", *result.ReasoningEffort)
		})
	}
}
