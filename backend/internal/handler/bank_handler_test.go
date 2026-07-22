package handler

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

type bankHandlerServiceStub struct {
	statusUserID  int64
	advanceCalls  int
	exchangeCalls int
	repayCalls    int
}

func (s *bankHandlerServiceStub) GetStatus(_ context.Context, userID int64) (*service.BankStatus, error) {
	s.statusUserID = userID
	return &service.BankStatus{PermanentBalance: "8.00000000"}, nil
}

func (s *bankHandlerServiceStub) AdvanceAtomic(context.Context, int64, float64, *service.IdempotencyAtomicClaim) (*service.BankAdvanceResult, error) {
	s.advanceCalls++
	return nil, nil
}

func (s *bankHandlerServiceStub) ExchangeAtomic(context.Context, int64, float64, *service.IdempotencyAtomicClaim) (*service.BankExchangeResult, error) {
	s.exchangeCalls++
	return nil, nil
}

func (s *bankHandlerServiceStub) RepayAtomic(context.Context, int64, service.BankRepaySource, float64, *service.IdempotencyAtomicClaim) (*service.BankRepayResult, error) {
	s.repayCalls++
	return nil, nil
}

func TestBankHandlerGetStatusUsesAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &bankHandlerServiceStub{}
	router := gin.New()
	router.GET("/api/v1/bank/status", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	}, NewBankHandler(svc).GetStatus)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/bank/status", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(42), svc.statusUserID)
	require.Contains(t, recorder.Body.String(), `"permanent_balance":"8.00000000"`)
}

func TestBankHandlerAdvanceValidatesRequestBeforeIdempotency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &bankHandlerServiceStub{}
	router := gin.New()
	router.POST("/api/v1/bank/advance", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	}, NewBankHandler(svc).Advance)

	for _, tc := range []struct {
		name string
		body string
		key  string
	}{
		{name: "missing idempotency key", body: `{"amount":"5.00000000"}`},
		{name: "numeric amount rejected", body: `{"amount":5}`, key: "bank-key"},
		{name: "unknown field rejected", body: `{"amount":"5.00000000","extra":true}`, key: "bank-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/bank/advance", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			if tc.key != "" {
				request.Header.Set("Idempotency-Key", tc.key)
			}
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
	require.Zero(t, svc.advanceCalls)
}

func TestBankHandlerRepayRequiresStrictSourceAmountAndIdempotency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &bankHandlerServiceStub{}
	router := gin.New()
	router.POST("/api/v1/bank/repay", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	}, NewBankHandler(svc).Repay)

	for _, tc := range []struct {
		name string
		body string
		key  string
	}{
		{name: "missing idempotency key", body: `{"source":"temporary","amount":"1.00000000"}`},
		{name: "invalid source", body: `{"source":"wallet","amount":"1.00000000"}`, key: "repay-key"},
		{name: "numeric amount rejected", body: `{"source":"permanent","amount":1}`, key: "repay-key"},
		{name: "unknown field rejected", body: `{"source":"temporary","amount":"1.00000000","extra":true}`, key: "repay-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/bank/repay", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			if tc.key != "" {
				request.Header.Set("Idempotency-Key", tc.key)
			}
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
	require.Zero(t, svc.repayCalls)
}
