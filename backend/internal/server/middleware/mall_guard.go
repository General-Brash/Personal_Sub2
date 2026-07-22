package middleware

import (
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// MallEnabledUserGuard blocks user mall browsing and order creation while the mall is disabled.
func MallEnabledUserGuard(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settingService != nil && settingService.IsMallEnabled(c.Request.Context()) {
			c.Next()
			return
		}
		response.ErrorFrom(c, infraerrors.Forbidden("MALL_DISABLED", "mall is disabled"))
		c.Abort()
	}
}
