package handler

import (
	"context"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

func (h *GatewayHandler) CodexModels(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "invalid_request_error", "API key group is required")
		return
	}

	forcedPlatform := ""
	if value, exists := middleware2.GetForcePlatformFromContext(c); exists {
		forcedPlatform = strings.TrimSpace(value)
	}
	modelIDs := h.codexModelIDsForGroup(c.Request.Context(), apiKey.Group, forcedPlatform)
	modelIDs = service.FilterCodexModelIDsForGroup(modelIDs, apiKey.Group)
	body, err := h.gatewayService.BuildCodexModelsManifestForGroup(
		c.Request.Context(),
		apiKey.Group,
		forcedPlatform,
		modelIDs,
	)
	if err != nil {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "Failed to build Codex models manifest")
		return
	}
	etag := service.CodexModelsManifestETag(body)
	c.Header("ETag", etag)
	if service.CodexModelsManifestETagMatches(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		c.Writer.WriteHeaderNow()
		return
	}
	c.Data(http.StatusOK, "application/json", body)
}

func (h *GatewayHandler) codexModelIDsForGroup(ctx context.Context, group *service.Group, platformOverride string) []string {
	if h == nil || h.gatewayService == nil || group == nil {
		return nil
	}

	groupID := &group.ID
	platform := strings.TrimSpace(platformOverride)
	if platform == "" {
		platform = group.Platform
	}
	if platform == service.PlatformComposite {
		availableModels := h.compositeAvailableModels(ctx, groupID)
		fallbackModels := defaultCodexModelIDsForPlatform(service.PlatformComposite)
		if group.CustomModelsListEnabled() {
			return filterModelsByCustomList(availableModels, fallbackModels, group.ModelsListConfig.Models)
		}
		if len(availableModels) > 0 {
			return availableModels
		}
		return fallbackModels
	}

	availableModels := h.gatewayService.GetAvailableModels(ctx, groupID, platform)
	fallbackModels := defaultCodexModelIDsForPlatform(platform)
	if group.CustomModelsListEnabled() {
		return filterModelsByCustomList(
			customModelsListSource(platform, availableModels, fallbackModels),
			fallbackModels,
			group.ModelsListConfig.Models,
		)
	}
	if len(availableModels) > 0 {
		return availableModels
	}
	return fallbackModels
}

func defaultCodexModelIDsForPlatform(platform string) []string {
	switch platform {
	case service.PlatformDeepseek:
		return []string{"deepseek-v4-pro", "deepseek-v4-flash"}
	default:
		return defaultModelIDsForPlatform(platform)
	}
}
