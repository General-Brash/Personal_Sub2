package handler

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// DynamicRateAdminService is the narrow admin surface for W09. GatewayService
// implements it; no shared Handler/Wire mutation is required.
type DynamicRateAdminService interface {
	DynamicRatePolicy(ctx context.Context, groupID int64) (*service.DynamicRatePolicy, error)
	UpsertDynamicRatePolicy(ctx context.Context, policy *service.DynamicRatePolicy) (*service.DynamicRatePolicy, error)
	PreviewDynamicRate(ctx context.Context, policy *service.DynamicRatePolicy, userID int64, at time.Time, staticFactor, peakFactor float64, subscriptionGroup bool) (*service.DynamicRatePreview, error)
	DynamicRateStatus(ctx context.Context, userID, groupID int64, at time.Time) (*service.DynamicRateUsageStatus, *service.DynamicRatePolicy, error)
}

type DynamicRateHandler struct {
	keys    *service.APIKeyService
	service DynamicRateAdminService
}

func NewDynamicRateHandler(service DynamicRateAdminService) *DynamicRateHandler {
	return &DynamicRateHandler{service: service}
}

// RegisterRoutes registers the W09 admin endpoints. The caller owns the
// /admin/groups route group and admin middleware. Exact paths are:
// GET  /api/v1/admin/groups/:id/dynamic-rate
// PUT  /api/v1/admin/groups/:id/dynamic-rate
// POST /api/v1/admin/groups/:id/dynamic-rate/preview
// GET  /api/v1/admin/groups/:id/dynamic-rate/status
func (h *DynamicRateHandler) RegisterRoutes(groups *gin.RouterGroup) {
	groups.GET("/:id/dynamic-rate", h.GetPolicy)
	groups.PUT("/:id/dynamic-rate", h.PutPolicy)
	groups.POST("/:id/dynamic-rate/preview", h.Preview)
	groups.GET("/:id/dynamic-rate/status", h.Status)
}

func (h *DynamicRateHandler) GetPolicy(c *gin.Context) {
	groupID, ok := dynamicRateGroupID(c)
	if !ok {
		return
	}
	policy, err := h.service.DynamicRatePolicy(c.Request.Context(), groupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

type dynamicRatePutPolicyRequest struct {
	service.DynamicRatePolicy
	ExpectedPolicyVersion *int64 `json:"expected_policy_version"`
}

func (h *DynamicRateHandler) PutPolicy(c *gin.Context) {
	groupID, ok := dynamicRateGroupID(c)
	if !ok {
		return
	}
	var req dynamicRatePutPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}
	policy := req.DynamicRatePolicy
	policy.GroupID = groupID
	policy.ExpectedPolicyVersion = req.ExpectedPolicyVersion
	saved, err := h.service.UpsertDynamicRatePolicy(c.Request.Context(), &policy)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, saved)
}

type dynamicRatePreviewRequest struct {
	service.DynamicRatePolicy
	UserID            int64      `json:"user_id"`
	At                *time.Time `json:"at"`
	StaticFactor      *float64   `json:"static_factor"`
	PeakFactor        *float64   `json:"peak_factor"`
	SubscriptionGroup bool       `json:"subscription_group"`
}

func (h *DynamicRateHandler) Preview(c *gin.Context) {
	groupID, ok := dynamicRateGroupID(c)
	if !ok {
		return
	}
	var req dynamicRatePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body: "+err.Error())
		return
	}
	req.GroupID = groupID
	at := time.Now()
	if req.At != nil {
		at = req.At.UTC()
	}
	staticFactor, peakFactor := 1.0, 1.0
	if req.StaticFactor != nil {
		staticFactor = *req.StaticFactor
	}
	if req.PeakFactor != nil {
		peakFactor = *req.PeakFactor
	}
	preview, err := h.service.PreviewDynamicRate(c.Request.Context(), &req.DynamicRatePolicy, req.UserID, at, staticFactor, peakFactor, req.SubscriptionGroup)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, preview)
}

func (h *DynamicRateHandler) Status(c *gin.Context) {
	groupID, ok := dynamicRateGroupID(c)
	if !ok {
		return
	}
	userID, err := strconv.ParseInt(c.Query("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "user_id must be a positive integer")
		return
	}
	at := time.Now()
	if raw := c.Query("at"); raw != "" {
		parsed, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			response.BadRequest(c, "at must be RFC3339")
			return
		}
		at = parsed
	}
	status, policy, err := h.service.DynamicRateStatus(c.Request.Context(), userID, groupID, at)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"status": status, "policy": policy})
}

func dynamicRateGroupID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "group id must be a positive integer")
		return 0, false
	}
	return id, true
}

func (h *DynamicRateHandler) MyStatus(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	groupID, ok := dynamicRateGroupID(c)
	if !ok {
		return
	}
	if h.keys == nil {
		response.Forbidden(c, "Group access unavailable")
		return
	}
	groups, err := h.keys.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	allowed := false
	for _, group := range groups {
		if group.ID == groupID {
			allowed = true
			break
		}
	}
	if !allowed {
		response.Forbidden(c, "Group not available")
		return
	}
	status, policy, err := h.service.DynamicRateStatus(c.Request.Context(), subject.UserID, groupID, time.Now())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"status": status, "enabled": policy != nil && policy.Enabled})
}
