package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminUpdateBalanceUsesActorScopedAtomicReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newMemoryIdempotencyRepoStub()
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	previousCoordinator := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(repo, cfg))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previousCoordinator) })

	adminService := newStubAdminService()
	adminService.adminBalanceAtomicStore = &adminAtomicSuccessStore{repo: repo}
	handler := NewUserHandler(adminService, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99})
		c.Next()
	})
	router.POST("/api/v1/admin/users/:id/balance", handler.UpdateBalance)

	post := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/42/balance", bytes.NewBufferString(`{"balance":1.25,"operation":"add","notes":"atomic"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "admin-balance-42")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}

	first := post()
	second := post()
	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, http.StatusOK, second.Code)
	require.Equal(t, "true", second.Header().Get("X-Idempotency-Replayed"))
	require.Equal(t, 1, adminService.adminBalanceAtomicCalls)
	require.JSONEq(t, first.Body.String(), second.Body.String())
	require.Contains(t, first.Body.String(), `"balance":1.25`)

	record, err := repo.GetByScopeActorScopeAndKeyHash(
		context.Background(),
		"admin.users.balance.update",
		"admin:99",
		service.HashIdempotencyKey("admin-balance-42"),
	)
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, service.IdempotencyStatusSucceeded, record.Status)
}

func TestAdminUpdateBalanceReturnsConflictWhenPermanentBalanceIsInsufficient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	previousCoordinator := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newMemoryIdempotencyRepoStub(), cfg))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previousCoordinator) })

	adminService := newStubAdminService()
	adminService.adminBalanceAtomicErr = service.ErrAdminBalanceInsufficient
	handler := NewUserHandler(adminService, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99})
		c.Next()
	})
	router.POST("/api/v1/admin/users/:id/balance", handler.UpdateBalance)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/42/balance", bytes.NewBufferString(`{"balance":2,"operation":"subtract","notes":"concurrent withdrawal"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "admin-balance-42-insufficient")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.JSONEq(t, `{
		"code": 409,
		"message": "insufficient permanent balance",
		"reason": "INSUFFICIENT_BALANCE"
	}`, recorder.Body.String())
	require.Equal(t, 1, adminService.adminBalanceAtomicCalls)
}

func TestAdminUpdateBalanceReturnsBadRequestForInvalidLedgerPrecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	previousCoordinator := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newMemoryIdempotencyRepoStub(), cfg))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previousCoordinator) })

	adminService := newStubAdminService()
	adminService.adminBalanceAtomicErr = service.ErrInvalidAdminBalanceAdjustment
	handler := NewUserHandler(adminService, nil, nil, nil, nil, nil)
	router := gin.New()
	router.POST("/api/v1/admin/users/:id/balance", handler.UpdateBalance)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/42/balance", bytes.NewBufferString(`{"balance":0.000000001,"operation":"add","notes":"invalid precision"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "admin-balance-42-invalid")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.JSONEq(t, `{
		"code": 400,
		"message": "invalid balance adjustment",
		"reason": "INVALID_BALANCE_ADJUSTMENT"
	}`, recorder.Body.String())
	require.Equal(t, 1, adminService.adminBalanceAtomicCalls)
}
