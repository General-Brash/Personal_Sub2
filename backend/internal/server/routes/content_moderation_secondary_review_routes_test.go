package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestContentModerationSecondaryReviewStatusRoute(t *testing.T) {
	const classifierToken = "SECRET_CLASSIFIER_TOKEN"
	classifier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health/live":
			_, _ = w.Write([]byte(`{"status":"live"}`))
		case "/health/ready":
			_, _ = w.Write([]byte(`{"status":"ready","active_model_version":"intent-v1","preprocessing_version":"pre-v1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer classifier.Close()

	settings := &secondaryReviewRouteSettingRepo{values: map[string]string{
		service.SettingKeyContentModerationConfig: `{"secondary_review":{"mode":"off","endpoint":"` + classifier.URL + `","token":"` + classifierToken + `"}}`,
	}}
	moderationService := service.NewContentModerationService(settings, nil, nil, nil, nil, nil, nil)
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		ContentModeration: adminhandler.NewContentModerationHandler(moderationService),
	}}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerContentModerationRoutes(router.Group("/admin"), handlers)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/risk-control/secondary-review/status", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), classifierToken)
	var envelope struct {
		Code int                                            `json:"code"`
		Data service.ContentModerationSecondaryReviewStatus `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.True(t, envelope.Data.Live)
	require.True(t, envelope.Data.Ready)
	require.Equal(t, "ready", envelope.Data.Code)
	require.Equal(t, "intent-v1", requireRouteStringPointer(t, envelope.Data.ActiveModelVersion))
	require.Equal(t, "pre-v1", requireRouteStringPointer(t, envelope.Data.PreprocessingVersion))
}

func TestContentModerationSecondaryReviewStatusRouteReportsModelVersionMismatch(t *testing.T) {
	classifier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/health/live" {
			_, _ = w.Write([]byte(`{"status":"live"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ready","active_model_version":"intent-v2","preprocessing_version":"pre-v2"}`))
	}))
	defer classifier.Close()

	settings := &secondaryReviewRouteSettingRepo{values: map[string]string{
		service.SettingKeyContentModerationConfig: `{"secondary_review":{"mode":"off","endpoint":"` + classifier.URL + `","expected_model_version":"intent-v1"}}`,
	}}
	moderationService := service.NewContentModerationService(settings, nil, nil, nil, nil, nil, nil)
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		ContentModeration: adminhandler.NewContentModerationHandler(moderationService),
	}}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerContentModerationRoutes(router.Group("/admin"), handlers)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/risk-control/secondary-review/status", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Code int                                            `json:"code"`
		Data service.ContentModerationSecondaryReviewStatus `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.True(t, envelope.Data.Live)
	require.False(t, envelope.Data.Ready)
	require.Equal(t, "model_version_mismatch", envelope.Data.Code)
	require.Equal(t, "intent-v2", requireRouteStringPointer(t, envelope.Data.ActiveModelVersion))
	require.Equal(t, "pre-v2", requireRouteStringPointer(t, envelope.Data.PreprocessingVersion))
}

type secondaryReviewRouteSettingRepo struct {
	values map[string]string
}

func (r *secondaryReviewRouteSettingRepo) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (r *secondaryReviewRouteSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r *secondaryReviewRouteSettingRepo) Set(_ context.Context, key, value string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value
	return nil
}

func (r *secondaryReviewRouteSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *secondaryReviewRouteSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		if err := r.Set(context.Background(), key, value); err != nil {
			return err
		}
	}
	return nil
}

func (r *secondaryReviewRouteSettingRepo) GetAll(context.Context) (map[string]string, error) {
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *secondaryReviewRouteSettingRepo) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

func requireRouteStringPointer(t *testing.T, value *string) string {
	t.Helper()
	require.NotNil(t, value)
	return *value
}
