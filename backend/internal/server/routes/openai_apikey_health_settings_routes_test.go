package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type r08OpenAIHealthSettingRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r *r08OpenAIHealthSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	return r.values[key], nil
}

func (r *r08OpenAIHealthSettingRepo) Set(_ context.Context, key, value string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value
	return nil
}

func TestOpenAIAPIKeyHealthSettingsAreInExistingAdminSettingsGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &r08OpenAIHealthSettingRepo{values: map[string]string{}}
	settingService := service.NewSettingService(repo, &config.Config{})
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{Setting: adminhandler.NewSettingHandler(settingService, nil, nil, nil, nil, nil, nil)}}
	router := gin.New()
	adminAuth := servermiddleware.AdminAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer admin" {
			servermiddleware.AbortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization required")
			return
		}
		c.Next()
	})
	auditLog := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	stepUp := servermiddleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	RegisterAdminRoutes(router.Group("/api/v1"), handlers, adminAuth, auditLog, stepUp, nil, nil)

	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/openai-api-key-health", nil))
	require.Equal(t, http.StatusUnauthorized, unauthenticated.Code)

	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/openai-api-key-health", nil)
	getRequest.Header.Set("Authorization", "Bearer admin")
	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, getRequest)
	require.Equal(t, http.StatusOK, getRecorder.Code)
	var getPayload struct {
		Data service.OpenAIAPIKeyHealthBreakerSettings `json:"data"`
	}
	require.NoError(t, json.Unmarshal(getRecorder.Body.Bytes(), &getPayload))
	require.False(t, getPayload.Data.Enabled)

	putRequest := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/openai-api-key-health", bytes.NewReader([]byte(`{"enabled":true,"window_minutes":3,"failure_threshold":7,"cooldown_minutes":11}`)))
	putRequest.Header.Set("Authorization", "Bearer admin")
	putRequest.Header.Set("Content-Type", "application/json")
	putRecorder := httptest.NewRecorder()
	router.ServeHTTP(putRecorder, putRequest)
	require.Equal(t, http.StatusOK, putRecorder.Code)
	var putPayload struct {
		Data service.OpenAIAPIKeyHealthBreakerSettings `json:"data"`
	}
	require.NoError(t, json.Unmarshal(putRecorder.Body.Bytes(), &putPayload))
	require.True(t, putPayload.Data.Enabled)
	require.Equal(t, 3, putPayload.Data.WindowMinutes)
	require.Equal(t, 7, putPayload.Data.FailureThreshold)
	require.Equal(t, 11, putPayload.Data.CooldownMinutes)
}
