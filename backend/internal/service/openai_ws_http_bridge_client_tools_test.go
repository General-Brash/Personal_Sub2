package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIWSHTTPBridgeClientToolsPreserveContinuationAndAccountIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	first := []byte(`{"tools":[{"type":"custom","name":"write_file"}],"input":[]}`)
	_, _, err := adaptOpenAIWSHTTPBridgeClientTools(c, 1, first)
	require.NoError(t, err)
	continuation := []byte(`{"input":[{"type":"custom_tool_call","name":"write_file","input":"hello"}]}`)
	body, mapping, err := adaptOpenAIWSHTTPBridgeClientTools(c, 1, continuation)
	require.NoError(t, err)
	require.True(t, mapping.CustomTools["write_file"])
	require.Equal(t, "function", gjson.GetBytes(body, "tools.0.type").String())
	require.Equal(t, "function_call", gjson.GetBytes(body, "input.0.type").String())
	isolated, mapping, err := adaptOpenAIWSHTTPBridgeClientTools(c, 2, continuation)
	require.NoError(t, err)
	require.Empty(t, mapping.CustomTools)
	require.False(t, gjson.GetBytes(isolated, "tools").Exists())
	require.Equal(t, "custom_tool_call", gjson.GetBytes(isolated, "input.0.type").String())
}
