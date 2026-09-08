package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestForwardOpenAIWSV2_MarksCyberPolicyForErrorAndResponseFailed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		upstreamEvent []byte
		wantError     bool
		wantTerminal  string
		wantInput     int
		wantOutput    int
	}{
		{
			name:          "independent_error_event",
			upstreamEvent: []byte(`{"type":"error","error":{"type":"rate_limit_error","code":"cyber_policy","message":"blocked by cyber policy"},"usage":{"input_tokens":5,"output_tokens":1}}`),
			wantError:     true,
			wantInput:     5,
			wantOutput:    1,
		},
		{
			name:          "response_failed_terminal_event",
			upstreamEvent: []byte(`{"type":"response.failed","response":{"id":"resp_cyber","status":"failed","error":{"code":"cyber_policy","message":"blocked by cyber policy"},"usage":{"input_tokens":9,"output_tokens":2}}}`),
			wantTerminal:  "response.failed",
			wantInput:     9,
			wantOutput:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)

			cfg := newOpenAIWSV2TestConfig()
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			captureConn := &openAIWSCaptureConn{events: [][]byte{append([]byte(nil), tt.upstreamEvent...)}}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: captureConn})
			svc := &OpenAIGatewayService{
				cfg:              cfg,
				httpUpstream:     &httpUpstreamRecorder{},
				cache:            &stubGatewayCache{},
				openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
				toolCorrector:    NewCodexToolCorrector(),
				openaiWSPool:     pool,
			}
			account := &Account{
				ID: 5883, Name: "openai-ws-v2-cyber", Platform: PlatformOpenAI,
				Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test"},
				Extra:       map[string]any{"responses_websockets_v2_enabled": true},
			}

			result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":false,"input":"hello"}`))
			if tt.wantError {
				require.Error(t, err)
				require.Nil(t, result)
				var failoverErr *UpstreamFailoverError
				require.False(t, errors.As(err, &failoverErr))
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, tt.wantTerminal, result.UpstreamTerminalEvent)
			}

			mark := GetOpsCyberPolicy(c)
			require.NotNil(t, mark)
			require.Equal(t, "cyber_policy", mark.Code)
			require.Equal(t, tt.wantInput, mark.UpstreamInTok)
			require.Equal(t, tt.wantOutput, mark.UpstreamOutTok)
		})
	}
}
