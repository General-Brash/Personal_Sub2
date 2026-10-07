package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type permissionStepUpSettingRepo struct {
	values map[string]string
}

func (r *permissionStepUpSettingRepo) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}
func (r *permissionStepUpSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	return r.values[key], nil
}
func (r *permissionStepUpSettingRepo) Set(context.Context, string, string) error { return nil }
func (r *permissionStepUpSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = r.values[key]
	}
	return out, nil
}
func (r *permissionStepUpSettingRepo) SetMultiple(context.Context, map[string]string) error {
	return nil
}
func (r *permissionStepUpSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}
func (r *permissionStepUpSettingRepo) Delete(context.Context, string) error { return nil }

// 发证路由（授予/撤销管理员权限）必须无条件 step-up：全局开关关闭时，
// 未认证请求也要被 step-up 门控以 401 拦下，而不是落到 handler（handler 缺依赖时返回 403）。
func TestAdminPermissionGrantRoutesAlwaysRequireStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settingService := service.NewSettingService(&permissionStepUpSettingRepo{
		values: map[string]string{service.SettingKeyStepUpEnabled: "false"},
	}, &config.Config{})
	if settingService.IsStepUpEnabled(context.Background()) {
		t.Fatal("step-up switch must be disabled for this test")
	}
	stepUp := middleware.NewStepUpAuthMiddleware(nil, nil, settingService)

	router := gin.New()
	registerAdminPermissionRoutes(router.Group("/api/v1/admin"), handler.NewFeatureManagementHandler(nil, nil, nil), stepUp)

	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodPut, "/api/v1/admin/users/2/permissions/oidc.keys.rotate", http.StatusUnauthorized},
		{http.MethodDelete, "/api/v1/admin/users/2/permissions/oidc.keys.rotate", http.StatusUnauthorized},
		// 读接口不挂 step-up，直接到达 handler。
		{http.MethodGet, "/api/v1/admin/permissions", http.StatusForbidden},
		{http.MethodGet, "/api/v1/admin/users/2/permissions", http.StatusForbidden},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
		if recorder.Code != tc.want {
			t.Errorf("%s %s status = %d, want %d (body %s)", tc.method, tc.path, recorder.Code, tc.want, recorder.Body.String())
		}
	}
}
