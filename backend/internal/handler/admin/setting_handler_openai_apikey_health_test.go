package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIAPIKeyHealthAdminSettingRepo struct {
	service.SettingRepository
	values   map[string]string
	setCalls int
	setKey   string
	setValue string
}

func (r *openAIAPIKeyHealthAdminSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", nil
}

func (r *openAIAPIKeyHealthAdminSettingRepo) Set(_ context.Context, key, value string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value
	r.setCalls++
	r.setKey = key
	r.setValue = value
	return nil
}

func newOpenAIAPIKeyHealthAdminHandler(t *testing.T, values map[string]string) (*SettingHandler, *openAIAPIKeyHealthAdminSettingRepo) {
	t.Helper()
	repo := &openAIAPIKeyHealthAdminSettingRepo{values: values}
	settings := service.NewSettingService(repo, &config.Config{})
	return NewSettingHandler(settings, nil, nil, nil, nil, nil, nil), repo
}

func callOpenAIAPIKeyHealthAdminHandler(t *testing.T, h *SettingHandler, method string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, "/api/v1/admin/settings/openai-api-key-health", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if method == http.MethodGet {
		h.GetOpenAIAPIKeyHealthBreakerSettings(c)
	} else {
		h.UpdateOpenAIAPIKeyHealthBreakerSettings(c)
	}
	return recorder
}

type openAIAPIKeyHealthSettingsResponse struct {
	Code int                                       `json:"code"`
	Data service.OpenAIAPIKeyHealthBreakerSettings `json:"data"`
}

func decodeOpenAIAPIKeyHealthSettingsResponse(t *testing.T, recorder *httptest.ResponseRecorder) openAIAPIKeyHealthSettingsResponse {
	t.Helper()
	var response openAIAPIKeyHealthSettingsResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestOpenAIAPIKeyHealthSettingsHandlerGetDefaultsDisabled(t *testing.T) {
	h, repo := newOpenAIAPIKeyHealthAdminHandler(t, map[string]string{})
	recorder := callOpenAIAPIKeyHealthAdminHandler(t, h, http.MethodGet, nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	got := decodeOpenAIAPIKeyHealthSettingsResponse(t, recorder)
	require.Equal(t, 0, got.Code)
	require.False(t, got.Data.Enabled)
	require.Equal(t, 2, got.Data.WindowMinutes)
	require.Equal(t, 10, got.Data.FailureThreshold)
	require.Equal(t, 5, got.Data.CooldownMinutes)
	require.Zero(t, repo.setCalls)
}

func TestOpenAIAPIKeyHealthSettingsHandlerPutNormalizesAndRefreshesCache(t *testing.T) {
	h, repo := newOpenAIAPIKeyHealthAdminHandler(t, map[string]string{})
	recorder := callOpenAIAPIKeyHealthAdminHandler(t, h, http.MethodPut, []byte(`{"enabled":true,"window_minutes":0,"failure_threshold":10001,"cooldown_minutes":61}`))
	require.Equal(t, http.StatusOK, recorder.Code)
	got := decodeOpenAIAPIKeyHealthSettingsResponse(t, recorder)
	require.True(t, got.Data.Enabled)
	require.Equal(t, 1, got.Data.WindowMinutes)
	require.Equal(t, 10000, got.Data.FailureThreshold)
	require.Equal(t, 60, got.Data.CooldownMinutes)
	require.Equal(t, 1, repo.setCalls)
	require.Equal(t, service.SettingKeyOpenAIAPIKeyHealthBreakerSettings, repo.setKey)
	var stored service.OpenAIAPIKeyHealthBreakerSettings
	require.NoError(t, json.Unmarshal([]byte(repo.setValue), &stored))
	require.Equal(t, got.Data, stored)

	getRecorder := callOpenAIAPIKeyHealthAdminHandler(t, h, http.MethodGet, nil)
	require.Equal(t, http.StatusOK, getRecorder.Code)
	require.Equal(t, got.Data, decodeOpenAIAPIKeyHealthSettingsResponse(t, getRecorder).Data)
	require.Equal(t, 1, repo.setCalls, "GET must not add another write")
}

func TestOpenAIAPIKeyHealthSettingsHandlerPutRejectsInvalidJSONTypes(t *testing.T) {
	h, repo := newOpenAIAPIKeyHealthAdminHandler(t, map[string]string{})
	recorder := callOpenAIAPIKeyHealthAdminHandler(t, h, http.MethodPut, []byte(`{"enabled":true,"window_minutes":"two","failure_threshold":3,"cooldown_minutes":5}`))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, repo.setCalls)
}
