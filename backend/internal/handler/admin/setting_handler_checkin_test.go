package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettingHandler_CheckinSettingsRejectInvalidTiersAndRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	settingService := service.NewSettingService(repo, &config.Config{})
	h := NewSettingHandler(settingService, nil, nil, nil, nil, nil, nil)

	invalid := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/checkin", bytes.NewBufferString(`{
		"enabled": true,
		"max_reward_day": 3,
		"reward_tiers": [
			{"day": 1, "amount": "1.00000000"},
			{"day": 2, "amount": "2.00000000"}
		]
	}`))
	invalid.Header.Set("Content-Type", "application/json")
	invalidRecorder := httptest.NewRecorder()
	invalidContext, _ := gin.CreateTestContext(invalidRecorder)
	invalidContext.Request = invalid
	h.UpdateDailyCheckinSettings(invalidContext)
	require.Equal(t, http.StatusBadRequest, invalidRecorder.Code)
	require.Nil(t, repo.lastUpdates)

	valid := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/checkin", bytes.NewBufferString(`{
		"enabled": true,
		"max_reward_day": 3,
		"reward_tiers": [
			{"day": 1, "amount": "1.00000000"},
			{"day": 2, "amount": "2.50000000"},
			{"day": 3, "amount": "4.00000000"}
		]
	}`))
	valid.Header.Set("Content-Type", "application/json")
	validRecorder := httptest.NewRecorder()
	validContext, _ := gin.CreateTestContext(validRecorder)
	validContext.Request = valid
	h.UpdateDailyCheckinSettings(validContext)
	require.Equal(t, http.StatusOK, validRecorder.Code)
	require.Len(t, repo.lastUpdates, 3)
	require.Equal(t, "true", repo.values[service.SettingKeyDailyCheckinEnabled])
	require.Equal(t, "3", repo.values[service.SettingKeyDailyCheckinMaxRewardDay])
	require.JSONEq(t, `[{"day":1,"amount":"1.00000000"},{"day":2,"amount":"2.50000000"},{"day":3,"amount":"4.00000000"}]`, repo.values[service.SettingKeyDailyCheckinRewardTiers])

	var putResponse struct {
		Data struct {
			RewardTiers []struct {
				Day    int    `json:"day"`
				Amount string `json:"amount"`
			} `json:"reward_tiers"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(validRecorder.Body.Bytes(), &putResponse))
	require.Equal(t, "1.00000000", putResponse.Data.RewardTiers[0].Amount)
	require.Equal(t, "2.50000000", putResponse.Data.RewardTiers[1].Amount)

	getRecorder := httptest.NewRecorder()
	getContext, _ := gin.CreateTestContext(getRecorder)
	getContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/checkin", nil)
	h.GetDailyCheckinSettings(getContext)
	require.Equal(t, http.StatusOK, getRecorder.Code)

	var resp response.Response
	require.NoError(t, json.Unmarshal(getRecorder.Body.Bytes(), &resp))
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, data["enabled"])
	require.Equal(t, float64(3), data["max_reward_day"])
	tiers, ok := data["reward_tiers"].([]any)
	require.True(t, ok)
	require.Len(t, tiers, 3)
	firstTier, ok := tiers[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "1.00000000", firstTier["amount"])
}

func TestSettingHandler_CheckinSettingsRejectMissingRequiredFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name string
		body string
	}{
		{
			name: "enabled",
			body: `{"max_reward_day":1,"reward_tiers":[{"day":1,"amount":"1.00000000"}]}`,
		},
		{
			name: "max reward day",
			body: `{"enabled":false,"reward_tiers":[{"day":1,"amount":"1.00000000"}]}`,
		},
		{
			name: "reward tiers",
			body: `{"enabled":false,"max_reward_day":1}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &settingHandlerRepoStub{values: map[string]string{}}
			settingService := service.NewSettingService(repo, &config.Config{})
			h := NewSettingHandler(settingService, nil, nil, nil, nil, nil, nil)

			req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/checkin", bytes.NewBufferString(testCase.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = req

			h.UpdateDailyCheckinSettings(context)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Nil(t, repo.lastUpdates)
		})
	}
}

func TestSettingHandler_CheckinSettingsRejectNonCanonicalAmounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		amount string
	}{
		{name: "JSON number", amount: `1`},
		{name: "scientific notation", amount: `"1e0"`},
		{name: "positive sign", amount: `"+1"`},
		{name: "negative sign", amount: `"-1"`},
		{name: "leading zero", amount: `"01"`},
		{name: "leading whitespace", amount: `" 1"`},
		{name: "trailing whitespace", amount: `"1 "`},
		{name: "too many decimal places", amount: `"1.000000001"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &settingHandlerRepoStub{values: map[string]string{}}
			settingService := service.NewSettingService(repo, &config.Config{})
			h := NewSettingHandler(settingService, nil, nil, nil, nil, nil, nil)

			body := `{"enabled":true,"max_reward_day":1,"reward_tiers":[{"day":1,"amount":` + tt.amount + `}]}`
			req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/checkin", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = req

			h.UpdateDailyCheckinSettings(context)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Nil(t, repo.lastUpdates)
		})
	}
}

func TestSettingHandler_CheckinSettingsExactAmountBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		amount     string
		statusCode int
	}{
		{name: "zero", amount: `"0"`, statusCode: http.StatusBadRequest},
		{name: "smallest positive amount", amount: `"0.00000001"`, statusCode: http.StatusOK},
		{name: "unrepresentable numeric upper boundary", amount: `"999999999999.99999999"`, statusCode: http.StatusBadRequest},
		{name: "thirteen digit integer", amount: `"1000000000000"`, statusCode: http.StatusBadRequest},
		{name: "thousand separators", amount: `"999,999,999,999.99999999"`, statusCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &settingHandlerRepoStub{values: map[string]string{}}
			settingService := service.NewSettingService(repo, &config.Config{})
			h := NewSettingHandler(settingService, nil, nil, nil, nil, nil, nil)

			body := `{"enabled":true,"max_reward_day":1,"reward_tiers":[{"day":1,"amount":` + tt.amount + `}]}`
			req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/checkin", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = req

			h.UpdateDailyCheckinSettings(context)

			require.Equal(t, tt.statusCode, recorder.Code)
			if tt.statusCode == http.StatusOK {
				require.Contains(t, repo.values[service.SettingKeyDailyCheckinRewardTiers], `"amount":`+tt.amount)
			} else {
				require.Nil(t, repo.lastUpdates)
			}
		})
	}
}

func TestSettingHandler_GenericSettingsExcludeDailyCheckinPolicy(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(service.SystemSettings{}),
		reflect.TypeOf(dto.SystemSettings{}),
		reflect.TypeOf(UpdateSettingsRequest{}),
	} {
		_, found := typ.FieldByName("DailyCheckinPolicy")
		require.False(t, found, "%s must not expose daily checkin policy", typ.Name())
	}

	payload, err := json.Marshal(dto.SystemSettings{})
	require.NoError(t, err)
	require.NotContains(t, string(payload), "daily_checkin_policy")
}
