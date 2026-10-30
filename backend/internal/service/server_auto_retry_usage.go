package service

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Preserve attempt-local snapshots only when an empty terminal contradicts
// positive usage evidence. Ordinary terminal usage remains authoritative.
func (s *OpenAIGatewayService) preserveServerAutoRetrySSEUsage(c *gin.Context, resp *http.Response, body string, finalResponse []byte) ([]byte, error) {
	retry := ServerAutoRetryFromContext(c)
	if retry == nil || !retry.AttemptEligible || retry.HasSemanticOutput || retry.UsageEvidence != "nonzero" ||
		serverAutoRetryUsageEvidence(finalResponse) != "zero" || serverAutoRetryHasError(finalResponse) {
		return finalResponse, nil
	}
	usageSnapshot := make(map[string]any)
	var peak OpenAIUsage
	var snapshotErr error
	terminalSeen := false
	forEachOpenAISSEDataPayload(body, func(payload []byte) {
		if terminalSeen || snapshotErr != nil || !gjson.ValidBytes(payload) {
			return
		}
		eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
		if (eventType == "response.completed" || eventType == "response.done") && gjson.GetBytes(payload, "response").IsObject() {
			terminalSeen = true
			return
		}
		for _, candidate := range []struct{ usagePath, imageUsagePath string }{
			{"usage", "tool_usage.image_gen"},
			{"response.usage", "response.tool_usage.image_gen"},
			{"data.usage", "data.tool_usage.image_gen"},
			{"data.response.usage", "data.response.tool_usage.image_gen"},
		} {
			value := gjson.GetBytes(payload, candidate.usagePath)
			parsed, ok := openAIUsageFromGJSON(value)
			if !ok {
				continue
			}
			mergeHostedImageGenToolUsage(gjson.GetBytes(payload, candidate.imageUsagePath), &parsed)
			peak.InputTokens = max(peak.InputTokens, parsed.InputTokens)
			peak.OutputTokens = max(peak.OutputTokens, parsed.OutputTokens)
			peak.CacheReadInputTokens = max(peak.CacheReadInputTokens, parsed.CacheReadInputTokens)
			peak.CacheCreationInputTokens = max(peak.CacheCreationInputTokens, parsed.CacheCreationInputTokens)
			peak.ImageInputTokens = max(peak.ImageInputTokens, parsed.ImageInputTokens)
			peak.ImageOutputTokens = max(peak.ImageOutputTokens, parsed.ImageOutputTokens)
			var snapshot map[string]any
			decoder := json.NewDecoder(strings.NewReader(value.Raw))
			decoder.UseNumber()
			if snapshotErr = decoder.Decode(&snapshot); snapshotErr != nil {
				return
			}
			mergeServerAutoRetryUsageSnapshot(usageSnapshot, snapshot)
		}
	})
	if snapshotErr != nil || peak == (OpenAIUsage{}) {
		return nil, s.serverAutoRetrySSEUsageUnknown(c, resp, finalResponse)
	}
	usageJSON, err := json.Marshal(usageSnapshot)
	if err != nil {
		return nil, s.serverAutoRetrySSEUsageUnknown(c, resp, finalResponse)
	}
	merged, _ := openAIUsageFromGJSON(gjson.ParseBytes(usageJSON))
	// Aliases can be masked by explicit zero fields in another snapshot.
	// Restore only known positive quantities; never invent absent zero fields.
	for _, field := range []struct {
		path         string
		peak, merged int
	}{
		{"input_tokens", peak.InputTokens, merged.InputTokens},
		{"output_tokens", peak.OutputTokens, merged.OutputTokens},
		{"input_tokens_details.cached_tokens", peak.CacheReadInputTokens, merged.CacheReadInputTokens},
		{"input_tokens_details.cache_write_tokens", peak.CacheCreationInputTokens, merged.CacheCreationInputTokens},
		{"input_tokens_details.image_tokens", peak.ImageInputTokens, merged.ImageInputTokens},
		{"output_tokens_details.image_tokens", peak.ImageOutputTokens, merged.ImageOutputTokens},
	} {
		if field.peak > 0 && field.peak != field.merged {
			usageJSON, err = sjson.SetBytes(usageJSON, field.path, field.peak)
			if err != nil {
				return nil, s.serverAutoRetrySSEUsageUnknown(c, resp, finalResponse)
			}
		}
	}
	updated, err := sjson.SetRawBytes(finalResponse, "usage", usageJSON)
	if err != nil {
		return nil, s.serverAutoRetrySSEUsageUnknown(c, resp, finalResponse)
	}
	return updated, nil
}

// Usage frames are cumulative snapshots, not independent billable increments.
func mergeServerAutoRetryUsageSnapshot(dst, snapshot map[string]any) {
	for key, value := range snapshot {
		previous, exists := dst[key]
		if nested, ok := value.(map[string]any); ok {
			if prior, ok := previous.(map[string]any); ok {
				mergeServerAutoRetryUsageSnapshot(prior, nested)
			} else {
				dst[key] = nested
			}
			continue
		}
		if number, ok := value.(json.Number); ok {
			current, _ := number.Float64()
			if prior, ok := previous.(json.Number); ok {
				old, _ := prior.Float64()
				if current > old {
					dst[key] = value
				}
				continue
			}
			if current > 0 {
				dst[key] = value
				continue
			}
		}
		if !exists {
			dst[key] = value
		}
	}
}

func (s *OpenAIGatewayService) serverAutoRetrySSEUsageUnknown(c *gin.Context, resp *http.Response, payload []byte) *UpstreamFailoverError {
	retry := ServerAutoRetryFromContext(c)
	err := s.serverAutoRetryTarget(c, retry.CurrentAccount, resp, payload, "empty_usage_unknown", nil)
	err.ServerAutoRetry.UsageEvidence = "unknown"
	err.ServerAutoRetry.StopReason = "usage_unknown"
	err.NextAccountAction = NextAccountStop
	err.SafeToFailoverAfterWrite = false
	err.ResponseBody = []byte(`{"error":{"type":"upstream_error","message":"Upstream positive usage could not be reconstructed after an empty zero-usage terminal response","code":"empty_usage_unknown"}}`)
	return err
}
