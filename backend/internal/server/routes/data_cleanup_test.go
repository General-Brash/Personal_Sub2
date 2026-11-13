package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDataCleanupRetiredEndpointCannotDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/old-cleanup", retiredCleanupEndpoint)
	out := httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest(http.MethodPost, "/old-cleanup", nil))
	require.Equal(t, http.StatusGone, out.Code)
	require.Contains(t, out.Body.String(), "DATA_CLEANUP_CENTER_REQUIRED")
}
