package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	openAIResponsesAzureCompactionEnvelope = "ccswitch-compaction-v1:"
	openAIResponsesAzureSummaryPrefix      = "Another language model started to solve this problem and produced a summary of its thinking process. You also have access to the state of the tools that were used by that language model. Use this to build on the work that has already been done and avoid duplicating work. Here is the summary produced by the other language model, use the information in this summary to assist with your own analysis:"
	openAIResponsesAzureOpaqueNote         = "[earlier conversation was compacted; the summary is stored in a format this model cannot read]"
	openAIResponsesAzureEncryptedPart      = `{"type":"input_text","text":"[encrypted content omitted]"}`
)

// Relays can redact the resource qualifier. Restrict it to qualifier tokens and
// require adjacent same-resource guidance before cleaning input.
var openAIResponsesForeignResourceMessagePattern = regexp.MustCompile(`\b(?:was|were) created under a different ((?:[[:alnum:]_*-]+ )*)openai resource\.? use the same resource that created the items? to access (?:it|them)\b`)

// normalizeOpenAIResponsesAzureResourceRetryBody ports the foreign-resource
// recovery in CC Switch v4.0.21, issue #8877, commit
// 96f01af07fddc80790c2fe4fd80c4a073a06074a (opaque_state_rectifier.rs and
// providers/codex_compaction.rs). It is a pure request-body helper: HTTP callers
// supply the actual upstreamReq.URL and own the one-retry guard. It does not
// handle WS errors, SSE/HTTP 200 business errors, or other opaque-state failures.
// Recovery requires an Azure HTTP 400 resource mismatch or a compatible message
// with explicit same-resource guidance; raw response text is not used.
func normalizeOpenAIResponsesAzureResourceRetryBody(statusCode int, body, responseBody []byte, upstreamURL *url.URL) ([]byte, bool, error) {
	if statusCode != http.StatusBadRequest || !gjson.ValidBytes(responseBody) {
		return body, false, nil
	}

	payload := gjson.ParseBytes(responseBody)
	if !payload.IsObject() {
		return body, false, nil
	}
	errorValue := payload.Get("error")
	// An explicit rejection of another field takes precedence over incidental
	// Azure wording in the message. Never discard input history to repair it.
	for _, errorObject := range []gjson.Result{payload, errorValue} {
		if !errorObject.IsObject() {
			continue
		}
		param := strings.TrimSpace(errorObject.Get("param").String())
		if param != "" && param != "input" && !strings.HasPrefix(param, "input.") && !strings.HasPrefix(param, "input[") {
			return body, false, nil
		}
		switch strings.ToLower(strings.TrimSpace(errorObject.Get("code").String())) {
		case "unsupported_parameter", "unknown_parameter", "unrecognized_parameter":
			return body, false, nil
		}
	}
	messages := []gjson.Result{payload.Get("message"), errorValue, payload.Get("detail")}
	if errorValue.IsObject() {
		messages = append(messages, errorValue.Get("message"))
	}
	foreignResource, mentionsCompaction := false, false
	for _, message := range messages {
		if message.Type != gjson.String {
			continue
		}
		text := message.String()
		if openAIResponsesForeignResourceMessageMatches(text) {
			foreignResource = true
		}
		if strings.Contains(text, "compaction") {
			mentionsCompaction = true
		}
	}
	if !foreignResource || !gjson.ValidBytes(body) {
		return body, false, nil
	}
	request := gjson.ParseBytes(body)
	if !request.IsObject() {
		return body, false, nil
	}
	input := request.Get("input")
	if !input.IsArray() {
		return body, false, nil
	}

	items := input.Array()
	current := -1
	for i := len(items) - 1; i >= 0; i-- {
		itemType := openAIResponsesAzureItemType(items[i])
		if itemType != "compaction_trigger" && itemType != "additional_tools" {
			current = i
			break
		}
	}

	cleanCompaction := openAIResponsesAzureShouldCleanCompaction(upstreamURL, mentionsCompaction)
	rebuilt := make([][]byte, 0, len(items))
	changed := false
	for index, item := range items {
		itemBody := []byte(item.Raw)
		if !item.IsObject() {
			rebuilt = append(rebuilt, itemBody)
			continue
		}
		itemType := openAIResponsesAzureItemType(item)
		switch itemType {
		case "reasoning":
			// Resource-bound reasoning cannot be replayed, but its plaintext
			// summary remains useful. Rebuild it as an ordinary history message
			// instead of retaining a provider-specific item that requires its id.
			message, hasSummary, err := openAIResponsesAzureReasoningSummaryMessage(item)
			if err != nil {
				return body, false, fmt.Errorf("preserve input.%d reasoning summary: %w", index, err)
			}
			if hasSummary {
				rebuilt = append(rebuilt, message)
			}
			changed = true
			continue
		case "compaction", "compaction_summary", "context_compaction":
			if cleanCompaction {
				encrypted := item.Get("encrypted_content")
				if encrypted.Type == gjson.String && encrypted.String() != "" {
					// Marshal only this newly synthesized message, never business
					// JSON: untouched numbers and tool payloads retain their raw bytes.
					message, err := json.Marshal(map[string]any{
						"type": "message",
						"role": "user",
						"content": []map[string]string{{
							"type": "input_text",
							"text": openAIResponsesAzureCompactionText(encrypted.String()),
						}},
					})
					if err != nil {
						return body, false, fmt.Errorf("replace input.%d compaction: %w", index, err)
					}
					rebuilt = append(rebuilt, message)
				}
				// A compaction marker without a nonempty string payload is removed.
				changed = true
				continue
			}
		case "function_call_output", "custom_tool_call_output", "agent_message":
			field := "output"
			if itemType == "agent_message" {
				// Protect this turn's agent task: determine its index before
				// deleting history, ignoring only trailing protocol markers.
				// Its top-level id is still stripped below.
				if index == current {
					break
				}
				field = "content"
			}
			var partsChanged bool
			var err error
			itemBody, partsChanged, err = openAIResponsesAzureReplaceEncryptedParts(itemBody, field)
			if err != nil {
				return body, false, fmt.Errorf("sanitize input.%d.%s: %w", index, field, err)
			}
			changed = changed || partsChanged
		}

		// A resource mismatch invalidates even well-formed msg/fc IDs. Keep the
		// same narrow top-level-only policy as proactive input-ID stripping.
		var idChanged bool
		var err error
		itemBody, idChanged, err = openAIResponsesAzureStripInputItemID(itemBody, itemType)
		if err != nil {
			return body, false, fmt.Errorf("delete input.%d.id: %w", index, err)
		}
		changed = changed || idChanged
		rebuilt = append(rebuilt, itemBody)
	}
	if !changed {
		return body, false, nil
	}

	// Replace only input: previous_response_id, include, and all other top-level
	// fields are retained without decoding/re-encoding their values.
	normalized, err := sjson.SetRawBytes(body, "input", openAIResponsesAzureRawArray(rebuilt))
	if err != nil {
		return body, false, fmt.Errorf("replace Azure resource retry input: %w", err)
	}
	return normalized, true, nil
}

func openAIResponsesAzureReasoningSummaryMessage(item gjson.Result) ([]byte, bool, error) {
	var summaries []string
	for _, summary := range item.Get("summary").Array() {
		text := summary.Get("text")
		if summary.Get("type").String() == "summary_text" && text.Type == gjson.String && text.String() != "" {
			summaries = append(summaries, text.String())
		}
	}
	if len(summaries) == 0 {
		return nil, false, nil
	}
	message, err := json.Marshal(map[string]any{
		"type": "message",
		"role": "user",
		"content": []map[string]string{{
			"type": "input_text",
			"text": openAIResponsesAzureSummaryPrefix + "\n" + strings.Join(summaries, "\n"),
		}},
	})
	if err != nil {
		return nil, false, err
	}
	return message, true, nil
}

func openAIResponsesAzureStripInputItemID(itemBody []byte, itemType string) ([]byte, bool, error) {
	if itemType == "item_reference" || !gjson.GetBytes(itemBody, "id").Exists() {
		return itemBody, false, nil
	}
	updated, err := sjson.DeleteBytes(itemBody, "id")
	if err != nil {
		return itemBody, false, err
	}
	return updated, true, nil
}

func openAIResponsesForeignResourceMessageMatches(text string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
	// Keep the legacy Azure form, including messages without recovery guidance.
	if strings.Contains(normalized, "was created under a different azure openai resource") ||
		strings.Contains(normalized, "were created under a different azure openai resource") {
		return true
	}
	match := openAIResponsesForeignResourceMessagePattern.FindStringSubmatch(normalized)
	if len(match) != 2 {
		return false
	}
	// A qualifier must not negate or contrast the resource mismatch.
	for _, word := range strings.Fields(match[1]) {
		switch word {
		case "not", "no", "never", "but", "rather", "instead":
			return false
		}
	}
	return true
}

func openAIResponsesAzureItemType(item gjson.Result) string {
	if item.IsObject() {
		if itemType := item.Get("type"); itemType.Type == gjson.String {
			return itemType.String()
		}
	}
	return ""
}

func openAIResponsesAzureShouldCleanCompaction(upstreamURL *url.URL, mentionsCompaction bool) bool {
	// An unknown/relative/non-HTTP URL must not be treated as a third-party
	// native Responses endpoint. Never infer this policy from credentials.
	if upstreamURL == nil || upstreamURL.Host == "" || upstreamURL.Opaque != "" {
		return false
	}
	if !strings.EqualFold(upstreamURL.Scheme, "http") && !strings.EqualFold(upstreamURL.Scheme, "https") {
		return false
	}
	if _, err := url.ParseRequestURI(upstreamURL.String()); err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(upstreamURL.Hostname(), "."))
	if host == "" {
		return false
	}
	switch host {
	case "api.openai.com", "chatgpt.com", "chat.openai.com":
		return mentionsCompaction
	default:
		return true
	}
}

func openAIResponsesAzureReplaceEncryptedParts(itemBody []byte, field string) ([]byte, bool, error) {
	parts := gjson.GetBytes(itemBody, field)
	if !parts.IsArray() {
		return itemBody, false, nil
	}
	rebuilt := make([][]byte, 0)
	changed := false
	for _, part := range parts.Array() {
		encrypted := part.Get("encrypted_content")
		if openAIResponsesAzureItemType(part) == "encrypted_content" &&
			encrypted.Type == gjson.String && encrypted.String() != "" {
			// Replace the entire direct part; do not traverse nested tool data.
			rebuilt = append(rebuilt, []byte(openAIResponsesAzureEncryptedPart))
			changed = true
		} else {
			rebuilt = append(rebuilt, []byte(part.Raw))
		}
	}
	if !changed {
		return itemBody, false, nil
	}
	normalized, err := sjson.SetRawBytes(itemBody, field, openAIResponsesAzureRawArray(rebuilt))
	if err != nil {
		return itemBody, false, err
	}
	return normalized, true, nil
}

func openAIResponsesAzureCompactionText(encrypted string) string {
	if !strings.HasPrefix(encrypted, openAIResponsesAzureCompactionEnvelope) {
		return openAIResponsesAzureOpaqueNote
	}
	encoded := strings.TrimPrefix(encrypted, openAIResponsesAzureCompactionEnvelope)
	// Go's base64 decoder ignores CR/LF even in Strict mode. Reject them (and
	// padding) explicitly to match Rust's canonical URL_SAFE_NO_PAD decoding.
	for i := 0; i < len(encoded); i++ {
		c := encoded[i]
		urlSafe := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_'
		if !urlSafe {
			return openAIResponsesAzureOpaqueNote
		}
	}
	summary, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || !utf8.Valid(summary) {
		return openAIResponsesAzureOpaqueNote
	}
	return openAIResponsesAzureSummaryPrefix + "\n" + string(summary)
}

func openAIResponsesAzureRawArray(items [][]byte) []byte {
	array := []byte{'['}
	for i, item := range items {
		if i > 0 {
			array = append(array, ',')
		}
		array = append(array, item...)
	}
	return append(array, ']')
}
