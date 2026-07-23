package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type mallSalesServiceStub struct {
	counts map[service.MallSalesKey]int64
	err    error
}

func (s *mallSalesServiceStub) GetMallSalesCounts(context.Context) (map[service.MallSalesKey]int64, error) {
	return s.counts, s.err
}

type financialHandlerServiceStub struct {
	ledgerUserID, ledgerPage, ledgerDays int64
	ledgerCategory                       string
	adminUserID, adminPage, adminDays    int64
	adminCategory                        string
	mallUserID, mallPage                 int64
	mallProductType                      string
	analyticsDays                        int
}

func (s *financialHandlerServiceStub) ListMallTransactions(_ context.Context, userID int64, page int, productType string) ([]service.MallTransactionItem, int64, error) {
	s.mallUserID, s.mallPage, s.mallProductType = userID, int64(page), productType
	return []service.MallTransactionItem{{ID: 1, UserID: userID, Username: "alice", ProductType: productType, Price: "1.00000000"}}, 21, nil
}

func (s *financialHandlerServiceStub) GetMallSalesAnalytics(_ context.Context, days int) (*service.MallSalesAnalytics, error) {
	s.analyticsDays = days
	return &service.MallSalesAnalytics{Days: days, TotalSales: 1, TotalRevenue: "1.00000000"}, nil
}

func (s *financialHandlerServiceStub) GetFinancialLedger(_ context.Context, userID int64, page int, days int, category string) (*service.FinancialLedgerResponse, error) {
	s.ledgerUserID, s.ledgerPage, s.ledgerDays, s.ledgerCategory = userID, int64(page), int64(days), category
	return &service.FinancialLedgerResponse{UserID: userID, Page: page, PageSize: 20, Pages: 2, Total: 21}, nil
}

func (s *financialHandlerServiceStub) GetAdminFinancialLedger(_ context.Context, userID int64, page int, days int, category string) (*service.FinancialLedgerResponse, error) {
	s.adminUserID, s.adminPage, s.adminDays, s.adminCategory = userID, int64(page), int64(days), category
	return &service.FinancialLedgerResponse{UserID: userID, Page: page, PageSize: 20, Pages: 2, Total: 21}, nil
}

func financialHandlerRouter(h *PaymentHandler, withUser bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if withUser {
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
			c.Next()
		})
	}
	return router
}

func TestGetFinancialLedgerUsesAuthenticatedUserAndFixedContract(t *testing.T) {
	svc := &financialHandlerServiceStub{}
	h := NewPaymentHandler(nil, nil)
	h.SetFinancialService(svc)
	router := financialHandlerRouter(h, true)
	router.GET("/user/ledger", h.GetFinancialLedger)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/user/ledger?page=2&page_size=1000&days=1&category=model", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(42), svc.ledgerUserID)
	require.Equal(t, int64(2), svc.ledgerPage)
	require.Equal(t, int64(1), svc.ledgerDays)
	require.Equal(t, "model", svc.ledgerCategory)
	require.Contains(t, rec.Body.String(), `"page_size":20`)
}

func TestGetFinancialLedgerRequiresAuthentication(t *testing.T) {
	svc := &financialHandlerServiceStub{}
	h := NewPaymentHandler(nil, nil)
	h.SetFinancialService(svc)
	router := financialHandlerRouter(h, false)
	router.GET("/user/ledger", h.GetFinancialLedger)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/user/ledger", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetAdminFinancialLedgerRejectsInvalidScopeAndForwardsFilter(t *testing.T) {
	svc := &financialHandlerServiceStub{}
	h := NewPaymentHandler(nil, nil)
	h.SetFinancialService(svc)
	router := financialHandlerRouter(h, false)
	router.GET("/admin/payment/ledger", h.GetAdminFinancialLedger)

	for _, query := range []string{"user_id=0", "user_id=bad", "days=2", "category=unknown", "page=0"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/payment/ledger?"+query, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code, query)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/payment/ledger?user_id=9&page=3&days=15&category=bank", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(9), svc.adminUserID)
	require.Equal(t, int64(3), svc.adminPage)
	require.Equal(t, int64(15), svc.adminDays)
	require.Equal(t, "bank", svc.adminCategory)
}

func TestListAdminMallTransactionsUsesFixedPageSize(t *testing.T) {
	svc := &financialHandlerServiceStub{}
	h := NewPaymentHandler(nil, nil)
	h.SetFinancialService(svc)
	router := financialHandlerRouter(h, false)
	router.GET("/admin/payment/mall/transactions", h.ListAdminMallTransactions)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/payment/mall/transactions?page=2&page_size=999&user_id=7&product_type=subscription", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(7), svc.mallUserID)
	require.Equal(t, int64(2), svc.mallPage)
	require.Equal(t, "subscription", svc.mallProductType)
	require.Contains(t, rec.Body.String(), `"page_size":20`)

	for _, query := range []string{"product_type=invalid", "user_id=-1", "page=0"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/payment/mall/transactions?"+query, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code, query)
	}
}

func TestGetAdminMallAnalyticsBoundsDays(t *testing.T) {
	svc := &financialHandlerServiceStub{}
	h := NewPaymentHandler(nil, nil)
	h.SetFinancialService(svc)
	router := financialHandlerRouter(h, false)
	router.GET("/admin/payment/mall/analytics", h.GetAdminMallAnalytics)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/payment/mall/analytics?days=30", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 30, svc.analyticsDays)

	for _, query := range []string{"days=0", "days=366", "days=bad"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/payment/mall/analytics?"+query, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code, query)
	}
}

func TestMallSalesCountsAreExposedByUserAndAdminProductShapes(t *testing.T) {
	counts := map[service.MallSalesKey]int64{
		{ProductType: service.MallProductTypeSubscription, ProductID: 7}: 12,
		{ProductType: service.MallProductTypeCurrency, ProductID: 9}:     34,
	}
	h := NewPaymentHandler(nil, nil)
	h.SetMallSalesService(&mallSalesServiceStub{counts: counts})

	loaded, err := h.loadMallSalesCounts(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(12), mallSalesCount(loaded, service.MallProductTypeSubscription, 7))
	require.Equal(t, int64(34), mallSalesCount(loaded, service.MallProductTypeCurrency, 9))
	require.Zero(t, mallSalesCount(loaded, service.MallProductTypeCurrency, 999))

	payload, err := json.Marshal(struct {
		Plans    []adminSubscriptionPlanWithSalesCount `json:"plans"`
		Products []adminCurrencyProductWithSalesCount  `json:"products"`
		Checkout checkoutInfoResponse                  `json:"checkout"`
	}{
		Plans: []adminSubscriptionPlanWithSalesCount{{
			SubscriptionPlan: &dbent.SubscriptionPlan{ID: 7, Name: "plan"},
			SalesCount:       mallSalesCount(loaded, service.MallProductTypeSubscription, 7),
		}},
		Products: []adminCurrencyProductWithSalesCount{{
			CurrencyProduct: &dbent.CurrencyProduct{ID: 9, Name: "credits"},
			SalesCount:      mallSalesCount(loaded, service.MallProductTypeCurrency, 9),
		}},
		Checkout: checkoutInfoResponse{
			Plans:            []checkoutPlan{{ID: 7, SalesCount: 12}},
			CurrencyProducts: []checkoutCurrencyProduct{{ID: 9, SalesCount: 34}},
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(payload), `"name":"plan"`)
	require.Contains(t, string(payload), `"name":"credits"`)
	require.Contains(t, string(payload), `"sales_count":12`)
	require.Contains(t, string(payload), `"sales_count":34`)
}

func TestLoadMallSalesCountsKeepsLegacyFallbackAndPropagatesFailures(t *testing.T) {
	h := NewPaymentHandler(nil, nil)
	counts, err := h.loadMallSalesCounts(context.Background())
	require.NoError(t, err)
	require.Empty(t, counts)

	wantErr := errors.New("sales unavailable")
	h.SetMallSalesService(&mallSalesServiceStub{err: wantErr})
	_, err = h.loadMallSalesCounts(context.Background())
	require.ErrorIs(t, err, wantErr)
	require.Empty(t, h.loadMallSalesCountsBestEffort(context.Background()))
}
