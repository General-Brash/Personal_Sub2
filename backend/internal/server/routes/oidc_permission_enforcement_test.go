package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type oidcRoutePermissionRepo struct {
	record *service.AdminPrincipalRecord
}

func (r *oidcRoutePermissionRepo) GetAdminPrincipal(context.Context, int64) (*service.AdminPrincipalRecord, error) {
	if r.record == nil {
		return nil, nil
	}
	copy := *r.record
	copy.Grants = append([]service.AdminGrant(nil), r.record.Grants...)
	return &copy, nil
}
func (*oidcRoutePermissionRepo) ResolveAdminAPIKeyBinding(context.Context, string) (*service.AdminPrincipalRecord, error) {
	return nil, nil
}
func (*oidcRoutePermissionRepo) ListAdminPermissionDefinitions(context.Context) ([]service.AdminPermissionDefinition, error) {
	return nil, nil
}
func (*oidcRoutePermissionRepo) UpsertAdminGrant(context.Context, int64, string, string, map[string]any, int64, string) error {
	return nil
}
func (*oidcRoutePermissionRepo) DeleteAdminGrant(context.Context, int64, string, int64, string) error {
	return nil
}
func (*oidcRoutePermissionRepo) BumpAdminPermissionVersion(context.Context, int64) (int64, error) {
	return 1, nil
}
func (*oidcRoutePermissionRepo) CreateAdminPermissionAudit(context.Context, service.AdminPermissionAudit) error {
	return nil
}
func (*oidcRoutePermissionRepo) ApplyAdminPermissionChange(context.Context, service.AdminPermissionChange) (int64, error) {
	return 1, nil
}

func TestOIDCAdminRoutePermissionFailsClosedOutsideGlobalEnforce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{service.AdminPermissionModeDisabled, service.AdminPermissionModeShadow} {
		t.Run(mode, func(t *testing.T) {
			repo := &oidcRoutePermissionRepo{record: &service.AdminPrincipalRecord{UserID: 7, Role: service.RoleAdmin, Status: service.StatusActive, Version: 1}}
			permissionService := service.NewAdminPermissionService(repo, mode)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				principal, err := permissionService.ResolvePrincipal(c.Request.Context(), 7)
				if err != nil {
					t.Fatal(err)
				}
				c.Set(string(middleware.ContextKeyAdminPrincipal), principal)
				ctx := service.ContextWithAdminAuthorization(c.Request.Context(), permissionService)
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			})
			router.GET("/oidc", middleware.RequireOIDCAdminPermission("oidc.provider.read"), func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodGet, "/oidc", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("ungranted OIDC route status = %d, want %d", response.Code, http.StatusForbidden)
			}

			repo.record.Grants = []service.AdminGrant{{Permission: "oidc.provider.read", Effect: service.AdminGrantAllow, Scope: map[string]any{"*": "*"}}}
			request = httptest.NewRequest(http.MethodGet, "/oidc", nil)
			response = httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("granted OIDC route status = %d, want %d", response.Code, http.StatusNoContent)
			}
		})
	}
}
