//go:build unit

package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type pricingPermissionRepo struct {
	service.AdminPermissionRepository
	record *service.AdminPrincipalRecord
}

func (r *pricingPermissionRepo) GetAdminPrincipal(context.Context, int64) (*service.AdminPrincipalRecord, error) {
	return r.record, nil
}

func TestAdminModelPricingRoutePermissionsAndAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, role, mode, method, path, permission string
		anonymous                                  bool
		want                                       int
	}{
		{"anonymous", service.RoleAdmin, "enforce", "PUT", "/api/v1/admin/model-plaza/pricing", "", true, 401},
		{"ordinary user", service.RoleUser, "enforce", "PUT", "/api/v1/admin/model-plaza/pricing", "models.pricing.write", false, 403},
		{"pricing reader", service.RoleAdmin, "enforce", "GET", "/api/v1/admin/model-plaza/pricing", "models.pricing.read", false, 200},
		{"reader cannot write", service.RoleAdmin, "enforce", "PUT", "/api/v1/admin/model-plaza/pricing", "models.pricing.read", false, 403},
		{"display editor cannot write", service.RoleAdmin, "enforce", "PUT", "/api/v1/admin/model-plaza/pricing", "models.catalog.write", false, 403},
		{"pricing writer", service.RoleAdmin, "enforce", "PUT", "/api/v1/admin/model-plaza/pricing", "models.pricing.write", false, 200},
		{"reader cannot reset", service.RoleAdmin, "enforce", "POST", "/api/v1/admin/model-plaza/pricing/reset", "models.pricing.read", false, 403},
		{"super admin", service.RoleSuperAdmin, "enforce", "PUT", "/api/v1/admin/model-plaza/pricing", "", false, 200},
		{"disabled compatibility", service.RoleAdmin, "disabled", "PUT", "/api/v1/admin/model-plaza/pricing", "", false, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "pricing-test-secret", ExpireHour: 1}}
			auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
			user := &service.User{ID: 11, Email: "pricing@example.test", Role: tc.role, Status: service.StatusActive, TokenVersion: 1}
			users := service.NewUserService(&stubUserRepo{getByID: func(context.Context, int64) (*service.User, error) { clone := *user; return &clone, nil }}, nil, nil, nil)
			grants := []service.AdminGrant{}
			if tc.permission != "" {
				grants = append(grants, service.AdminGrant{Permission: tc.permission, Effect: service.AdminGrantAllow, Scope: map[string]any{"*": "*"}})
			}
			permissions := service.NewAdminPermissionService(&pricingPermissionRepo{record: &service.AdminPrincipalRecord{UserID: 11, Role: tc.role, Status: service.StatusActive, Version: 1, Grants: grants}}, tc.mode)
			router := gin.New()
			router.Use(gin.HandlerFunc(NewAdminAuthMiddleware(auth, users, nil, nil, permissions)))
			router.Handle(tc.method, tc.path, func(c *gin.Context) { c.Status(200) })
			request := httptest.NewRequest(tc.method, tc.path, nil)
			if !tc.anonymous {
				token, err := auth.GenerateToken(context.Background(), user)
				require.NoError(t, err)
				request.Header.Set("Authorization", "Bearer "+token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, tc.want, response.Code, response.Body.String())
		})
	}
}

func TestAdminModelPricingAuditUsesTypedSingleModelFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	zero := 0.0
	price := 2e-6
	SetAuditExtra(c, map[string]any{"secret": "not allowed", "result": "failed"})
	SetDefaultPricingAudit(c, service.DefaultPricingChange{ModelID: "vendor/model", OldVersion: "1", NewVersion: "2", Before: service.DefaultPricingFields{InputPrice: &zero}, After: service.DefaultPricingFields{InputPrice: &price}})
	value, ok := c.Get(auditCtxKeyExtra)
	require.True(t, ok)
	extra := value.(map[string]any)
	require.NotContains(t, extra, "secret")
	require.Equal(t, "saved", extra["result"])
	require.Equal(t, "vendor/model", extra["pricing_model"])
	require.Equal(t, "1", extra["old_pricing_revision"])
	require.Equal(t, "2", extra["new_pricing_revision"])
	require.Equal(t, 0.0, *extra["pricing_before"].(service.DefaultPricingFields).InputPrice)
	require.Equal(t, 2e-6, *extra["pricing_after"].(service.DefaultPricingFields).InputPrice)
}
