package service

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIResponsesWebSocketCompatibilityBody_CompactionIsOptIn(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		t.Run(accountType, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: accountType}
			for _, explicit := range []bool{false, true} {
				name, input := "ordinary", `[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]`
				if explicit {
					name = "explicit_compaction"
					input = `[{"type":"compaction_trigger"},{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},{"type":"compaction_trigger"}]`
				}
				t.Run(name, func(t *testing.T) {
					body := []byte(`{"model":"gpt-5.6-sol","input":` + input + `}`)
					got, _, err := normalizeOpenAIResponsesWebSocketCompatibilityBody(body, account, false)
					require.NoError(t, err)
					require.Equal(t, explicit, HasCompactionTriggerInInput(got))
					items := gjson.GetBytes(got, "input").Array()
					if explicit {
						require.Len(t, items, 2)
						require.Equal(t, "compaction_trigger", items[len(items)-1].Get("type").String())
					} else {
						require.Len(t, items, 1)
					}
					require.Equal(t, "hello", items[0].Get("content.0.text").String())
				})
			}
		})
	}
}

const azureForeignResourceTestError = "The requested item was created under a different Azure OpenAI resource. Use the same resource that created the item to access it."

func TestNormalizeOpenAIResponsesAzureResourceRetryBodyRejectsNonTargetErrors(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","id":"msg_old","role":"user","content":"continue"}]}`)
	tests := []struct {
		name     string
		status   int
		response string
	}{
		{"http success", http.StatusOK, `{"error":{"message":"` + azureForeignResourceTestError + `"}}`},
		{"permission denied", http.StatusForbidden, `{"error":{"message":"` + azureForeignResourceTestError + `"}}`},
		{"rate limit", http.StatusTooManyRequests, `{"error":{"message":"` + azureForeignResourceTestError + `"}}`},
		{"server error", http.StatusBadGateway, `{"error":{"message":"` + azureForeignResourceTestError + `"}}`},
		{"non Azure resource", http.StatusBadRequest, `{"error":{"message":"This object was created under a different subscription; resource access is denied"}}`},
		{"raw text", http.StatusBadRequest, azureForeignResourceTestError},
		{"unrelated structured parameter", http.StatusBadRequest, `{"error":{"code":"invalid_request_error","param":"max_output_tokens","message":"` + azureForeignResourceTestError + `"}}`},
		{"unrelated top-level parameter", http.StatusBadRequest, `{"param":"model","message":"` + azureForeignResourceTestError + `"}`},
		{"explicit field rejection without param", http.StatusBadRequest, `{"error":{"code":"unsupported_parameter","message":"` + azureForeignResourceTestError + `"}}`},
		{"quoted resource explanation", http.StatusBadRequest, `{"error":{"code":"unsupported_parameter","param":"max_output_tokens","message":"Unsupported parameter: max_output_tokens. Not caused by an item created under a different Azure OpenAI resource."}}`},
		{"negated resource mismatch", http.StatusBadRequest, `{"error":{"message":"The requested item was not created under a different Azure OpenAI resource."}}`},
		{"nested business text", http.StatusBadRequest, `{"error":{"code":"bad_request","metadata":{"message":"` + azureForeignResourceTestError + `"}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed, err := normalizeOpenAIResponsesAzureResourceRetryBody(tt.status, body, []byte(tt.response), nil)
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, body, got)
		})
	}
}

func TestNormalizeOpenAIResponsesAzureResourceRetryBodyPreservesContextAndBusinessJSON(t *testing.T) {
	body := []byte(`{"previous_response_id":"resp_keep","metadata":{"nonce":9007199254740993},"input":[{"type":"message","id":"msg_old","role":"user","content":[{"type":"input_text","text":"hi","nonce":9007199254740993}]},{"type":"reasoning","id":"rs_old","encrypted_content":"opaque","summary":[{"type":"summary_text","text":"budget is 1000"}]},{"type":"reasoning","encrypted_content":"opaque-only"},{"type":"function_call","id":"fc_old","call_id":"call_keep","name":"run","namespace":"mcp__keep","arguments":"{\"id\":\"business\",\"nonce\":9007199254740993}"},{"type":"function_call_output","id":"fc_output","call_id":"call_keep","output":[{"type":"encrypted_content","encrypted_content":"stale"},{"type":"input_text","text":"keep","id":"nested"}]},{"type":"item_reference","id":"msg_reference"},{"type":"agent_message","id":"msg_past","content":[{"type":"encrypted_content","encrypted_content":"stale-task"}]},{"type":"agent_message","id":"msg_current","content":[{"type":"encrypted_content","encrypted_content":"current-task"}]},{"type":"additional_tools","id":"marker_id"}]}`)
	response := []byte(`{"error":{"message":"` + azureForeignResourceTestError + `"}}`)
	upstreamURL, err := url.Parse("https://azure-relay.example/v1/responses")
	require.NoError(t, err)
	got, changed, err := normalizeOpenAIResponsesAzureResourceRetryBody(http.StatusBadRequest, body, response, upstreamURL)
	require.NoError(t, err)
	require.True(t, changed)
	require.True(t, gjson.ValidBytes(got))
	items := gjson.GetBytes(got, "input").Array()
	require.Len(t, items, 8)
	for i, item := range items {
		if i != 4 {
			require.False(t, item.Get("id").Exists())
		}
	}
	require.Equal(t, "9007199254740993", items[0].Get("content.0.nonce").Raw)
	require.Equal(t, "message", items[1].Get("type").String())
	require.Contains(t, items[1].Get("content.0.text").String(), "budget is 1000")
	require.NotContains(t, items[1].Raw, "opaque")
	require.Equal(t, "call_keep", items[2].Get("call_id").String())
	require.Equal(t, "mcp__keep", items[2].Get("namespace").String())
	require.Equal(t, gjson.GetBytes(body, "input.3.arguments").Raw, items[2].Get("arguments").Raw)
	require.Equal(t, "call_keep", items[3].Get("call_id").String())
	require.Equal(t, "[encrypted content omitted]", items[3].Get("output.0.text").String())
	require.Equal(t, "nested", items[3].Get("output.1.id").String())
	require.Equal(t, "msg_reference", items[4].Get("id").String())
	require.Equal(t, "[encrypted content omitted]", items[5].Get("content.0.text").String())
	require.Equal(t, gjson.GetBytes(body, "input.7.content").Raw, items[6].Get("content").Raw)
	require.Equal(t, gjson.GetBytes(body, "metadata").Raw, gjson.GetBytes(got, "metadata").Raw)
	require.Equal(t, "resp_keep", gjson.GetBytes(got, "previous_response_id").String())
}

func TestNormalizeOpenAIResponsesAzureResourceRetryBodyPreservesPlaintextReasoning(t *testing.T) {
	body := []byte(`{"input":[{"type":"reasoning","summary":[{"type":"summary_text","text":"confirmed budget"}]},{"type":"message","id":"msg_old","role":"user","content":"continue"}]}`)
	response := []byte(`{"message":"` + azureForeignResourceTestError + `"}`)
	got, changed, err := normalizeOpenAIResponsesAzureResourceRetryBody(http.StatusBadRequest, body, response, nil)
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, gjson.GetBytes(got, "input.0.content.0.text").String(), "confirmed budget")
	require.False(t, gjson.GetBytes(got, "input.1.id").Exists())
}

func TestOpenAIResponsesAzureCompactionRecoveryPolicy(t *testing.T) {
	for _, tt := range []struct {
		host     string
		mentions bool
		clean    bool
	}{
		{"https://api.openai.com/v1/responses", false, false},
		{"https://chatgpt.com/backend-api/codex/responses", true, true},
		{"https://api.openai.com.evil.example/v1/responses", false, true},
		{"https://azure-relay.example/v1/responses", false, true},
		{"/v1/responses", true, false},
	} {
		t.Run(tt.host, func(t *testing.T) {
			u, err := url.Parse(tt.host)
			require.NoError(t, err)
			require.Equal(t, tt.clean, openAIResponsesAzureShouldCleanCompaction(u, tt.mentions))
		})
	}
	require.False(t, openAIResponsesAzureShouldCleanCompaction(nil, true))
	encoded := base64.RawURLEncoding.EncodeToString([]byte("retain summary"))
	require.Contains(t, openAIResponsesAzureCompactionText(openAIResponsesAzureCompactionEnvelope+encoded), "retain summary")
	for _, invalid := range []string{encoded + "=", encoded + "\n", "_w", "invalid+", "opaque"} {
		require.Equal(t, openAIResponsesAzureOpaqueNote, openAIResponsesAzureCompactionText(openAIResponsesAzureCompactionEnvelope+invalid))
	}
}
