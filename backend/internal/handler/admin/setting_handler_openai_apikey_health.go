package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// OpenAIAPIKeyHealthBreakerSettingsRequest is the admin settings payload for
// the opt-in OpenAI pool API-key health breaker. The service owns bounds
// normalization so GET after PUT always returns the effective cached values.
type OpenAIAPIKeyHealthBreakerSettingsRequest struct {
	Enabled          bool `json:"enabled"`
	WindowMinutes    int  `json:"window_minutes"`
	FailureThreshold int  `json:"failure_threshold"`
	CooldownMinutes  int  `json:"cooldown_minutes"`
}

// GetOpenAIAPIKeyHealthBreakerSettings returns the effective breaker settings.
// GET /api/v1/admin/settings/openai-api-key-health
func (h *SettingHandler) GetOpenAIAPIKeyHealthBreakerSettings(c *gin.Context) {
	settings, err := h.settingService.GetOpenAIAPIKeyHealthBreakerSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, settings)
}

// UpdateOpenAIAPIKeyHealthBreakerSettings updates the breaker settings and
// returns the post-normalization value held by the process cache.
// PUT /api/v1/admin/settings/openai-api-key-health
func (h *SettingHandler) UpdateOpenAIAPIKeyHealthBreakerSettings(c *gin.Context) {
	var req OpenAIAPIKeyHealthBreakerSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	if err := h.settingService.SetOpenAIAPIKeyHealthBreakerSettings(c.Request.Context(), &service.OpenAIAPIKeyHealthBreakerSettings{
		Enabled:          req.Enabled,
		WindowMinutes:    req.WindowMinutes,
		FailureThreshold: req.FailureThreshold,
		CooldownMinutes:  req.CooldownMinutes,
	}); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	updated, err := h.settingService.GetOpenAIAPIKeyHealthBreakerSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, updated)
}
