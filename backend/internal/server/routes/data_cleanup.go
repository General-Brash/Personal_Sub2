package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Old destructive endpoints must not bypass the unified preview/confirmation flow.
func retiredCleanupEndpoint(c *gin.Context) {
	response.ErrorWithDetails(c, http.StatusGone, "Use the unified Data Cleanup center", "DATA_CLEANUP_CENTER_REQUIRED", nil)
}
