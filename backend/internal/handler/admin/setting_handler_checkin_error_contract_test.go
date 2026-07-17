package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettingHandler_UpdateDailyCheckinSettingsUsesInvalidPolicyErrorContract(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{"enabled":`},
		{name: "missing field", body: `{"enabled":true,"max_reward_day":1}`},
		{name: "max reward day exceeds limit", body: `{"enabled":true,"max_reward_day":366,"reward_tiers":[]}`},
		{name: "invalid tier sequence", body: `{"enabled":true,"max_reward_day":2,"reward_tiers":[{"day":1,"amount":"1.00000000"}]}`},
		{name: "invalid amount lexeme", body: `{"enabled":true,"max_reward_day":1,"reward_tiers":[{"day":1,"amount":"1e0"}]}`},
		{name: "invalid amount range", body: `{"enabled":true,"max_reward_day":1,"reward_tiers":[{"day":1,"amount":"0"}]}`},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			repo := &settingHandlerRepoStub{values: map[string]string{}}
			h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), nil, nil, nil, nil, nil, nil)

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/checkin", bytes.NewBufferString(tt.body))
			ctx.Request.Header.Set("Content-Type", "application/json")

			h.UpdateDailyCheckinSettings(ctx)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			var payload map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
			require.JSONEq(t, "400", string(payload["code"]))
			require.JSONEq(t, `"daily checkin policy is invalid"`, string(payload["message"]))
			require.JSONEq(t, `"INVALID_DAILY_CHECKIN_POLICY"`, string(payload["reason"]))
			_, hasData := payload["data"]
			require.False(t, hasData)
			require.Nil(t, repo.lastUpdates)
		})
	}
}
