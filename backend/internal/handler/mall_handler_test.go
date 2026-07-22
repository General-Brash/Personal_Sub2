package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMallPurchaseRequiresIdempotencyKeyBeforeSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewPaymentHandler(nil, nil)
	h.SetMallService(service.NewMallService(nil, nil, nil))
	router := gin.New()
	router.POST("/api/v1/mall/purchases", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	}, h.PurchaseMallProduct)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mall/purchases", strings.NewReader(`{"product_type":"currency","product_id":1}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "IDEMPOTENCY_KEY_REQUIRED")
}
