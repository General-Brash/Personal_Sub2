package service

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

const openAIWSHTTPBridgeClientToolStateKey = "openai_ws_http_bridge_client_tool_state"

type openAIWSHTTPBridgeClientToolState struct {
	accountID    int64
	mapping      apicompat.ResponsesClientToolMapping
	loweredTools []any
}

func adaptOpenAIWSHTTPBridgeClientTools(c *gin.Context, accountID int64, body []byte) ([]byte, apicompat.ResponsesClientToolMapping, error) {
	if c == nil {
		return adaptResponsesClientToolsForFunctionUpstream(body, "OpenAI WS HTTP bridge")
	}
	var requestBody map[string]any
	if err := decodeOpenAIJSONUseNumber(body, &requestBody); err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, fmt.Errorf("decode OpenAI WS HTTP bridge client tools: %w", err)
	}
	var prior openAIWSHTTPBridgeClientToolState
	if value, exists := c.Get(openAIWSHTTPBridgeClientToolStateKey); exists {
		if state, ok := value.(openAIWSHTTPBridgeClientToolState); ok && state.accountID == accountID {
			prior = state
		}
	}
	mapping, changed, err := apicompat.AdaptResponsesClientToolsWithInheritedMapping(requestBody, prior.mapping, prior.loweredTools)
	if err != nil {
		return body, apicompat.ResponsesClientToolMapping{}, err
	}
	if changed {
		rebuilt, err := marshalOpenAIUpstreamJSON(requestBody)
		if err != nil {
			return body, apicompat.ResponsesClientToolMapping{}, fmt.Errorf("encode OpenAI WS HTTP bridge client tools: %w", err)
		}
		body = rebuilt
	}
	// Only publish state after a valid turn has been completely adapted. Gin
	// context scopes this to one client connection, and account identity prevents
	// inheriting a different account's transformed declaration on failover.
	tools, _ := requestBody["tools"].([]any)
	c.Set(openAIWSHTTPBridgeClientToolStateKey, openAIWSHTTPBridgeClientToolState{accountID: accountID, mapping: mapping, loweredTools: tools})
	return body, mapping, nil
}
