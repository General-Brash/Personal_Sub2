package admin

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type DataCleanupHandler struct {
	opsService     *service.OpsService
	cleanupService *service.DataCleanupService
	totpService    *service.TotpService
}

func NewDataCleanupHandler(opsService *service.OpsService, cleanupService *service.DataCleanupService, totpService *service.TotpService) *DataCleanupHandler {
	return &DataCleanupHandler{opsService: opsService, cleanupService: cleanupService, totpService: totpService}
}

type dataCleanupConfigResponse struct {
	DataRetention         service.OpsDataRetentionSettings `json:"data_retention"`
	AuditLogRetentionDays int                              `json:"audit_log_retention_days"`
}

func (h *DataCleanupHandler) requireAvailable(c *gin.Context) bool {
	if h == nil || h.opsService == nil || h.cleanupService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Data cleanup service not available")
		return false
	}
	if err := h.opsService.RequireMonitoringEnabled(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	return true
}

// GetConfig returns automatic cleanup policies without creating another audit scheduler.
func (h *DataCleanupHandler) GetConfig(c *gin.Context) {
	if !h.requireAvailable(c) {
		return
	}
	advanced, err := h.opsService.GetOpsAdvancedSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dataCleanupConfigResponse{
		DataRetention:         advanced.DataRetention,
		AuditLogRetentionDays: h.cleanupService.GetAuditLogRetentionDays(c.Request.Context()),
	})
}

// UpdateConfig updates ops/usage policies and the existing audit retention setting.
func (h *DataCleanupHandler) UpdateConfig(c *gin.Context) {
	if !h.requireAvailable(c) {
		return
	}
	var req dataCleanupConfigResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	updated, err := h.opsService.UpdateDataCleanupSettings(c.Request.Context(), req.DataRetention, req.AuditLogRetentionDays)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dataCleanupConfigResponse{
		DataRetention:         updated.DataRetention,
		AuditLogRetentionDays: h.cleanupService.GetAuditLogRetentionDays(c.Request.Context()),
	})
}

func (h *DataCleanupHandler) Preview(c *gin.Context) {
	if !h.requireAvailable(c) {
		return
	}
	var req service.DataCleanupFilter
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	preview, err := h.cleanupService.Preview(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, preview)
}

type dataCleanupExecuteRequest struct {
	service.DataCleanupFilter
	PreviewRows  int64  `json:"preview_rows"`
	PreviewToken string `json:"preview_token"`
	Confirmation string `json:"confirmation"`
	TOTPCode     string `json:"totp_code"`
}

func (h *DataCleanupHandler) Execute(c *gin.Context) {
	if !h.requireAvailable(c) {
		return
	}
	var req dataCleanupExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	preview, err := h.cleanupService.Preview(c.Request.Context(), req.DataCleanupFilter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if preview.RequiresTOTP {
		if c.GetString("auth_method") == service.AuditAuthMethodAdminAPIKey {
			response.ErrorWithDetails(c, http.StatusForbidden,
				"Admin API key cannot execute sensitive cleanup; a two-factor verified admin session is required",
				"STEP_UP_ADMIN_API_KEY_FORBIDDEN", nil)
			return
		}
		if h.totpService == nil {
			response.Error(c, http.StatusServiceUnavailable, "TOTP service not available")
			return
		}
		if err := h.totpService.VerifyCode(c.Request.Context(), subject.UserID, strings.TrimSpace(req.TOTPCode)); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	result, err := h.cleanupService.Execute(c.Request.Context(), req.DataCleanupFilter, req.PreviewRows, req.Confirmation, req.PreviewToken, service.DataCleanupOperator{
		UserID:     subject.UserID,
		Email:      c.GetString(middleware.ContextKeyAuthEmail),
		AuthMethod: c.GetString("auth_method"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *DataCleanupHandler) ListAudits(c *gin.Context) {
	if !h.requireAvailable(c) {
		return
	}
	items, err := h.cleanupService.ListAudits(c.Request.Context(), 20)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}
