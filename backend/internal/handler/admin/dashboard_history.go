package admin

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/gin-gonic/gin"
)

func (h *DashboardHandler) usageRangeMetadata(c *gin.Context, start, end time.Time) (usagestats.HistoryMetadata, bool) {
	m, err := h.aggregationService.GetUsageHistoryMetadata(c.Request.Context(), start, end)
	if err != nil {
		response.Error(c, 500, "Failed to get usage history metadata")
		return m, false
	}
	return m, true
}
