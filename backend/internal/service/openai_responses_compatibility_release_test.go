package service

import (
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
