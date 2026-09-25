package routes

import (
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestOIDCAdminPermissionContractIsComplete(t *testing.T) {
	permissions := []string{
		"oidc.provider.read", "oidc.clients.read", "oidc.clients.write",
		"oidc.clients.secret.rotate", "oidc.clients.disable", "oidc.consents.read",
		"oidc.consents.revoke", "oidc.keys.read", "oidc.keys.rotate",
		"oidc.keys.revoke", "oidc.audit.read",
	}
	for _, permission := range permissions {
		if !service.IsKnownAdminPermission(permission) {
			t.Errorf("permission %q missing from knownAdminPermissions", permission)
		}
	}
	paths := map[string]string{
		"GET /api/v1/admin/oidc-provider/status":                                "oidc.provider.read",
		"GET /api/v1/admin/oidc-provider/clients":                               "oidc.clients.read",
		"GET /api/v1/admin/oidc-provider/clients/:id":                           "oidc.clients.read",
		"GET /api/v1/admin/oidc-provider/consents":                              "oidc.consents.read",
		"GET /api/v1/admin/oidc-provider/keys":                                  "oidc.keys.read",
		"GET /api/v1/admin/oidc-provider/audit-events":                          "oidc.audit.read",
		"POST /api/v1/admin/oidc-provider/clients":                              "oidc.clients.write",
		"PUT /api/v1/admin/oidc-provider/clients/:id":                           "oidc.clients.write",
		"POST /api/v1/admin/oidc-provider/clients/:id/secrets":                  "oidc.clients.secret.rotate",
		"POST /api/v1/admin/oidc-provider/clients/:id/secrets/:secretID/revoke": "oidc.clients.secret.rotate",
		"POST /api/v1/admin/oidc-provider/clients/:id/disable":                  "oidc.clients.disable",
		"POST /api/v1/admin/oidc-provider/clients/:id/enable":                   "oidc.clients.disable",
		"POST /api/v1/admin/oidc-provider/consents/:id/revoke":                  "oidc.consents.revoke",
		"POST /api/v1/admin/oidc-provider/keys/rotate":                          "oidc.keys.rotate",
		"POST /api/v1/admin/oidc-provider/keys/:kid/retire":                     "oidc.keys.revoke",
		"POST /api/v1/admin/oidc-provider/keys/:kid/revoke":                     "oidc.keys.revoke",
	}
	for route, want := range paths {
		method, path := splitRoute(route)
		got, ok := middleware.LookupAdminRoutePermission(method, path)
		if !ok || got != want {
			t.Errorf("route %s = %q/%v, want %q", route, got, ok, want)
		}
	}
}

func TestOIDCAdminWriteRoutesUseUnconditionalStepUp(t *testing.T) {
	source, err := os.ReadFile("oidc_provider.go")
	if err != nil {
		t.Fatalf("read OIDC route source: %v", err)
	}
	text := string(source)
	if !strings.Contains(text, "alwaysStepUp := middleware.RequireStepUpAlways(stepUp)") {
		t.Fatal("OIDC admin routes must construct the unconditional step-up middleware")
	}
	if !strings.Contains(text, "writeGroup.Use(gin.HandlerFunc(alwaysStepUp))") {
		t.Fatal("OIDC admin write routes must use the unconditional step-up middleware")
	}
	if strings.Contains(text, "writeGroup.Use(gin.HandlerFunc(stepUp))") {
		t.Fatal("OIDC admin write routes must not use the global-switch step-up middleware directly")
	}
}

func splitRoute(route string) (string, string) {
	for i := 0; i < len(route); i++ {
		if route[i] == ' ' {
			return route[:i], route[i+1:]
		}
	}
	return "", route
}
