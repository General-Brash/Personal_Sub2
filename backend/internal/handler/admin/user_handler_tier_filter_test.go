package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

func TestUserHandlerListUsesCanonicalTierQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := newStubAdminService()
	h := NewUserHandler(stub, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/api/v1/admin/users", h.List)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?tier=premium", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, service.EntitlementTierPremium, stub.lastListUsers.filters.Tier)
	require.Equal(t, 1, stub.lastListUsers.calls)
}

func TestUserHandlerListRejectsInvalidTierWithoutRepositoryCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := newStubAdminService()
	h := NewUserHandler(stub, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/api/v1/admin/users", h.List)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?tier=gold", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, 0, stub.lastListUsers.calls)
}
