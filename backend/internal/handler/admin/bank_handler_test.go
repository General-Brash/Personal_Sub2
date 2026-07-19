package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminBankHandlerServiceStub struct {
	updateCalls int
}

func (s *adminBankHandlerServiceStub) GetPolicy(context.Context) (service.BankPolicyDTO, error) {
	return service.DefaultBankPolicy().DTO(), nil
}

func (s *adminBankHandlerServiceStub) UpdatePolicyAtomic(context.Context, int64, service.BankPolicyDTO, *service.IdempotencyAtomicClaim) (*service.BankPolicyDTO, error) {
	s.updateCalls++
	return nil, nil
}

func TestAdminBankHandlerGetPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/admin/settings/bank", NewBankHandler(&adminBankHandlerServiceStub{}).GetPolicy)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/bank", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"advance_min_amount":"5.00000000"`)
}

func TestAdminBankHandlerUpdateRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &adminBankHandlerServiceStub{}
	router := gin.New()
	router.PUT("/api/v1/admin/settings/bank", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	}, NewBankHandler(svc).UpdatePolicy)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/bank", strings.NewReader(`{"advance_min_amount":"5.00000000","unexpected":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "admin-bank-key")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, svc.updateCalls)
}
