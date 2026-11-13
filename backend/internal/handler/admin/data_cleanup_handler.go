package admin

import (
	"net/http"
	"strconv"
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
	// Authorization and merging use the persisted values protected by the CAS,
	// never this instance's potentially stale runtime snapshot.
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
	if !requireCleanupCategoryPermission(c, req.Category) {
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
	if !requireCleanupCategoryPermission(c, req.Category) {
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

func requireCleanupCategoryPermission(c *gin.Context, category string) bool {
	category = strings.ToLower(strings.TrimSpace(category))
	permission := "ops.manage"
	if category == "audit_logs" || category == "audit_statistics" || category == "prompt_audit" {
		permission = "audit.manage"
	}
	if service.AdminAuthorizationService(c.Request.Context()) == nil {
		response.Forbidden(c, "Cleanup category permission is required")
		return false
	}
	if err := service.AuthorizeAdminRequest(c.Request.Context(), permission, nil); err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	return true
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

func (h *DataCleanupHandler) Overview(c *gin.Context) {
	if !h.requireAvailable(c) {
		return
	}
	result, err := h.cleanupService.Overview(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *DataCleanupHandler) CancelTask(c *gin.Context) {
	if !h.requireAvailable(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid task ID")
		return
	}
	category, err := h.cleanupService.TaskCategory(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !requireCleanupCategoryPermission(c, category) {
		return
	}
	if err = h.cleanupService.CancelTask(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"canceled": true})
}
