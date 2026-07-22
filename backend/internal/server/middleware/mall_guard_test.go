package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type mallGuardSettingRepo struct{ enabled bool }

func (r mallGuardSettingRepo) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (r mallGuardSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != service.SettingKeyMallEnabled {
		return "", service.ErrSettingNotFound
	}
	if r.enabled {
		return "true", nil
	}
	return "false", nil
}

func (r mallGuardSettingRepo) Set(context.Context, string, string) error { return nil }
func (r mallGuardSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (r mallGuardSettingRepo) SetMultiple(context.Context, map[string]string) error { return nil }
func (r mallGuardSettingRepo) GetAll(context.Context) (map[string]string, error)    { return nil, nil }
func (r mallGuardSettingRepo) Delete(context.Context, string) error                 { return nil }

func TestMallEnabledUserGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name       string
		enabled    bool
		wantStatus int
	}{
		{name: "enabled", enabled: true, wantStatus: http.StatusNoContent},
		{name: "disabled", enabled: false, wantStatus: http.StatusForbidden},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			router := gin.New()
			settings := service.NewSettingService(mallGuardSettingRepo{enabled: testCase.enabled}, nil)
			router.GET("/mall", MallEnabledUserGuard(settings), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/mall", nil))
			require.Equal(t, testCase.wantStatus, recorder.Code)
			if !testCase.enabled {
				require.Contains(t, recorder.Body.String(), "MALL_DISABLED")
			}
		})
	}
}
