package service

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// prepareOpenAIResponsesAzureInputIDs prepares top-level input history items
// when the selected account is in configured scope.
func prepareOpenAIResponsesAzureInputIDs(c *gin.Context, body []byte) ([]byte, error) {
	retry := ServerAutoRetryFromContext(c)
	if retry == nil || !retry.Config.Enabled || !retry.Config.Azure400StripInputIDs {
		return body, nil
	}
	if retry.CurrentAccount == nil || !retry.Matches(c, retry.CurrentAccount) {
		return body, nil
	}
	if !gjson.ValidBytes(body) {
		return body, nil
	}
	request := gjson.ParseBytes(body)
	if !request.IsObject() {
		return body, nil
	}
	input := request.Get("input")
	if !input.IsArray() {
		return body, nil
	}

	items := input.Array()
	rebuilt := make([][]byte, 0, len(items))
	changed := false
	for index, item := range items {
		itemBody := []byte(item.Raw)
		if !item.IsObject() {
			rebuilt = append(rebuilt, itemBody)
			continue
		}
		itemType := openAIResponsesAzureItemType(item)
		if itemType == "reasoning" {
			message, hasSummary, err := openAIResponsesAzureReasoningSummaryMessage(item)
			if err != nil {
				return body, fmt.Errorf("preserve input.%d reasoning summary: %w", index, err)
			}
			if hasSummary {
				rebuilt = append(rebuilt, message)
			}
			changed = true
			continue
		}
		updated, itemChanged, err := openAIResponsesAzureStripInputItemID(itemBody, itemType)
		if err != nil {
			return body, fmt.Errorf("strip Responses input.%d.id: %w", index, err)
		}
		rebuilt = append(rebuilt, updated)
		changed = changed || itemChanged
	}
	if !changed {
		return body, nil
	}
	normalized, err := sjson.SetRawBytes(body, "input", openAIResponsesAzureRawArray(rebuilt))
	if err != nil {
		return body, fmt.Errorf("replace Responses input after Azure ID stripping: %w", err)
	}
	return normalized, nil
}
