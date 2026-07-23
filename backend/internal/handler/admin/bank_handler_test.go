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
	listUserID  int64
	listPage    int
}

func (s *adminBankHandlerServiceStub) GetPolicy(context.Context) (service.BankPolicyDTO, error) {
	return service.DefaultBankPolicy().DTO(), nil
}

func (s *adminBankHandlerServiceStub) UpdatePolicyAtomic(context.Context, int64, service.BankPolicyDTO, *service.IdempotencyAtomicClaim) (*service.BankPolicyDTO, error) {
	s.updateCalls++
	return nil, nil
}

func (s *adminBankHandlerServiceStub) ListAdminLedger(_ context.Context, userID int64, page int) ([]service.BankAdminLedgerItem, int64, error) {
	s.listUserID = userID
	s.listPage = page
	return []service.BankAdminLedgerItem{{
		ID: 11, UserID: userID, Username: "alice", Email: "alice@example.test",
		Operation: "exchange", TransactionAmount: "50.00000000", PermanentDelta: "-50.00000000", TemporaryDelta: "95.00000000",
		DebtDelta: "0.00000000", DebtBefore: "0.00000000", DebtAfter: "0.00000000",
	}}, 21, nil
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

func TestAdminBankHandlerListTransactionsUsesFixedPageSizeAndFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &adminBankHandlerServiceStub{}
	router := gin.New()
	router.GET("/api/v1/admin/settings/bank/transactions", NewBankHandler(svc).ListTransactions)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/bank/transactions?page=2&page_size=1000&user_id=7", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(7), svc.listUserID)
	require.Equal(t, 2, svc.listPage)
	require.Contains(t, recorder.Body.String(), `"page_size":20`)
	require.Contains(t, recorder.Body.String(), `"username":"alice"`)
	require.Contains(t, recorder.Body.String(), `"transaction_amount":"50.00000000"`)
}

func TestAdminBankHandlerListTransactionsRejectsMalformedFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &adminBankHandlerServiceStub{}
	router := gin.New()
	router.GET("/api/v1/admin/settings/bank/transactions", NewBankHandler(svc).ListTransactions)

	for _, query := range []string{"page=0", "page=-1", "user_id=0", "user_id=abc"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/bank/transactions?"+query, nil))
		require.Equal(t, http.StatusBadRequest, recorder.Code, query)
	}
	require.Zero(t, svc.listPage)
}
