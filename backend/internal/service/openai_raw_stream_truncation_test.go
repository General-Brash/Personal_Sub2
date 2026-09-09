//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRawChatStreamTruncationPreservesRetryBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, stream            string
		wantError, wantFailover bool
	}{
		{name: "empty stream before output", wantError: true, wantFailover: true},
		{name: "truncated after semantic output", stream: "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n", wantError: true},
		{name: "finish reason without done remains successful", stream: "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/chat/completions", nil)
			headers := make(http.Header)
			headers.Set("Content-Type", "text/event-stream")
			headers.Set("x-request-id", "raw-stream-regression")
			response := &http.Response{StatusCode: http.StatusOK, Header: headers, Body: io.NopCloser(strings.NewReader(tc.stream))}
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			result, err := svc.streamRawChatCompletions(c, response, &Account{ID: 1, Platform: PlatformOpenAI}, "gpt-5.6-sol", "gpt-5.6-sol", "gpt-5.6-sol", nil, nil, time.Now(), 0)
			require.Equal(t, tc.wantError, err != nil)
			var failover *UpstreamFailoverError
			require.Equal(t, tc.wantFailover, errors.As(err, &failover))
			if tc.wantFailover {
				require.Nil(t, result)
				require.False(t, c.Writer.Written())
				require.Equal(t, http.StatusBadGateway, failover.StatusCode)
			} else {
				require.NotNil(t, result)
				require.True(t, c.Writer.Written())
				require.Contains(t, recorder.Body.String(), "hello")
			}
		})
	}
}
