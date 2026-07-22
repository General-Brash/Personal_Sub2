package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCurrencyProductAdminRoutesRequireAdminAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	RegisterPaymentRoutes(
		v1,
		handler.NewPaymentHandler(nil, nil),
		&handler.PaymentWebhookHandler{},
		adminhandler.NewPaymentHandler(nil, nil),
		middleware.JWTAuthMiddleware(func(c *gin.Context) { c.Next() }),
		middleware.AdminAuthMiddleware(func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) }),
		middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }),
		nil,
	)

	for _, testCase := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/admin/payment/currency-products"},
		{http.MethodPost, "/api/v1/admin/payment/currency-products"},
		{http.MethodPut, "/api/v1/admin/payment/currency-products/1"},
		{http.MethodDelete, "/api/v1/admin/payment/currency-products/1"},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusUnauthorized, recorder.Code, "%s %s", testCase.method, testCase.path)
	}
}
