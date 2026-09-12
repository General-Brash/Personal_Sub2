package handler

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// FeatureManagementHandler keeps management identity separate from consumer
// entitlements. Every mutation checks its live, possibly key-scoped principal.
type FeatureManagementHandler struct {
	settings     *service.SettingService
	permissions  *service.AdminPermissionService
	entitlements *service.EntitlementService
}

func NewFeatureManagementHandler(permissions *service.AdminPermissionService, entitlements *service.EntitlementService, settings *service.SettingService) *FeatureManagementHandler {
	return &FeatureManagementHandler{permissions: permissions, entitlements: entitlements, settings: settings}
}

func (h *FeatureManagementHandler) authorize(c *gin.Context, permission string, write bool) (*service.AdminPrincipal, bool) {
	principal, ok := middleware.GetAdminPrincipalFromContext(c)
	if !ok || h == nil || h.permissions == nil {
		response.Forbidden(c, "Explicit administrator principal required")
		return nil, false
	}
	if write && !h.permissions.EnabledInEnforceMode() {
		response.Forbidden(c, "Administrator permission enforcement must be enabled before management writes")
		return nil, false
	}
	allowed, err := h.permissions.CheckPermission(c.Request.Context(), principal, permission, nil)
	if err != nil || !allowed {
		response.Forbidden(c, "Administrator permission denied")
		return nil, false
	}
	return principal, true
}

func (h *FeatureManagementHandler) capability(c *gin.Context, permission string) *service.AdminCapabilities {
	if h == nil || h.permissions == nil {
		return &service.AdminCapabilities{Mode: service.AdminPermissionModeDisabled, DenyReason: "permission_service_unavailable"}
	}
	principal, ok := middleware.GetAdminPrincipalFromContext(c)
	if !ok || principal == nil {
		return &service.AdminCapabilities{WritesEnabled: h.permissions.EnabledInEnforceMode(), Mode: h.permissions.Mode(), DenyReason: "principal_required"}
	}
	cap := h.permissions.Capabilities(c.Request.Context(), principal, permission)
	return &cap
}

func featureTargetID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid user id")
		return 0, false
	}
	return id, true
}

func (h *FeatureManagementHandler) ListPermissions(c *gin.Context) {
	if _, ok := h.authorize(c, "users.read", false); !ok {
		return
	}
	items, err := h.permissions.ListPermissionCatalog(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *FeatureManagementHandler) GetUserPermissions(c *gin.Context) {
	if _, ok := h.authorize(c, "users.read", false); !ok {
		return
	}
	id, ok := featureTargetID(c)
	if !ok {
		return
	}
	state, err := h.permissions.GetUserPermissionState(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if state != nil {
		state.Capabilities = h.capability(c, "security.permissions.grant")
	}
	response.Success(c, state)
}

func (h *FeatureManagementHandler) GrantUserPermission(c *gin.Context) {
	principal, ok := h.authorize(c, "security.permissions.grant", true)
	if !ok {
		return
	}
	id, ok := featureTargetID(c)
	if !ok {
		return
	}
	var req struct {
		Permission string         `json:"permission"`
		Effect     string         `json:"effect"`
		Scope      map[string]any `json:"scope"`
		Reason     string         `json:"reason"`
		RequestID  string         `json:"request_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Reason) == "" || (req.Permission != "" && req.Permission != c.Param("permission")) {
		response.BadRequest(c, "A matching permission and audit reason are required")
		return
	}
	if err := h.permissions.GrantPermission(c.Request.Context(), principal, id, c.Param("permission"), req.Effect, req.Scope, req.Reason); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"updated": true})
}

func (h *FeatureManagementHandler) RevokeUserPermission(c *gin.Context) {
	principal, ok := h.authorize(c, "security.permissions.grant", true)
	if !ok {
		return
	}
	id, ok := featureTargetID(c)
	if !ok {
		return
	}
	var req struct {
		Reason    string `json:"reason"`
		RequestID string `json:"request_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Reason) == "" {
		response.BadRequest(c, "An audit reason is required")
		return
	}
	if err := h.permissions.RevokePermission(c.Request.Context(), principal, id, c.Param("permission"), req.Reason); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"updated": true})
}

func (h *FeatureManagementHandler) GetUserEntitlement(c *gin.Context) {
	if _, ok := h.authorize(c, "users.read", false); !ok {
		return
	}
	id, ok := featureTargetID(c)
	if !ok {
		return
	}
	state, err := h.entitlements.Resolve(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	state.Capabilities = h.capability(c, "users.entitlement.manage")
	response.Success(c, state)
}

type entitlementChangeRequest struct {
	UserIDs   []int64 `json:"user_ids"`
	Tier      string  `json:"tier"`
	Reason    string  `json:"reason"`
	RequestID string  `json:"request_id"`
}

func readEntitlementChange(c *gin.Context, requireReason bool) (entitlementChangeRequest, bool) {
	var req entitlementChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.UserIDs) == 0 || len(req.UserIDs) > 1000 || service.NormalizeEntitlementTier(req.Tier) == "" {
		response.BadRequest(c, "Select a valid tier and between 1 and 1000 users")
		return req, false
	}
	if requireReason && strings.TrimSpace(req.Reason) == "" {
		response.BadRequest(c, "An audit reason is required")
		return req, false
	}
	if strings.TrimSpace(req.RequestID) == "" {
		req.RequestID = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	if requireReason && strings.TrimSpace(req.RequestID) == "" {
		response.BadRequest(c, "An idempotency request_id is required")
		return req, false
	}
	seen := make(map[int64]struct{}, len(req.UserIDs))
	clean := req.UserIDs[:0]
	for _, id := range req.UserIDs {
		if id <= 0 {
			response.BadRequest(c, "Invalid user id")
			return req, false
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		clean = append(clean, id)
	}
	req.UserIDs = clean
	return req, true
}

func (h *FeatureManagementHandler) PreviewEntitlements(c *gin.Context) {
	if _, ok := h.authorize(c, "users.entitlement.manage", false); !ok {
		return
	}
	req, ok := readEntitlementChange(c, false)
	if !ok {
		return
	}
	result, err := h.entitlements.PreviewTierChange(c.Request.Context(), req.UserIDs, req.Tier)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *FeatureManagementHandler) ApplyEntitlements(c *gin.Context) {
	principal, ok := h.authorize(c, "users.entitlement.manage", true)
	if !ok {
		return
	}
	req, ok := readEntitlementChange(c, true)
	if !ok {
		return
	}
	result, err := h.entitlements.ApplyTierChange(c.Request.Context(), req.UserIDs, req.Tier, principal.UserID, req.Reason, req.RequestID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

type entitlementCatalogResponse struct {
	Tiers        []service.EntitlementTierPolicy `json:"tiers"`
	Capabilities *service.AdminCapabilities      `json:"capabilities"`
}

func (h *FeatureManagementHandler) GetEntitlementCatalog(c *gin.Context) {
	if _, ok := h.authorize(c, "users.entitlement.manage", false); !ok {
		return
	}
	tiers, err := h.entitlements.ListTierPolicies(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, entitlementCatalogResponse{Tiers: tiers, Capabilities: h.capability(c, "users.entitlement.manage")})
}

func (h *FeatureManagementHandler) UpdateEntitlementPolicy(c *gin.Context) {
	principal, ok := h.authorize(c, "users.entitlement.manage", true)
	if !ok {
		return
	}
	var req service.UpdateEntitlementTierPolicyInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid entitlement tier policy")
		return
	}
	if strings.TrimSpace(req.RequestID) == "" {
		req.RequestID = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	policy, err := h.entitlements.UpdateTierPolicy(c.Request.Context(), req, principal.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

func (h *FeatureManagementHandler) UpdateUserEntitlement(c *gin.Context) {
	principal, ok := h.authorize(c, "users.entitlement.manage", true)
	if !ok {
		return
	}
	id, ok := featureTargetID(c)
	if !ok {
		return
	}
	var req struct {
		Tier      string `json:"tier"`
		Reason    string `json:"reason"`
		RequestID string `json:"request_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || service.NormalizeEntitlementTier(req.Tier) == "" || strings.TrimSpace(req.Reason) == "" {
		response.BadRequest(c, "A valid tier and audit reason are required")
		return
	}
	if strings.TrimSpace(req.RequestID) == "" {
		req.RequestID = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	if strings.TrimSpace(req.RequestID) == "" {
		response.BadRequest(c, "An idempotency request_id is required")
		return
	}
	result, err := h.entitlements.ApplyTierChange(c.Request.Context(), []int64{id}, req.Tier, principal.UserID, req.Reason, req.RequestID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *FeatureManagementHandler) GetFeatureSettings(c *gin.Context) {
	if _, ok := h.authorize(c, "system.settings.manage", false); !ok {
		return
	}
	config, err := h.settings.GetPersonalFeatureSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}
func (h *FeatureManagementHandler) UpdateFeatureSettings(c *gin.Context) {
	if _, ok := h.authorize(c, "system.settings.manage", true); !ok {
		return
	}
	var config service.PersonalFeatureSettings
	if err := c.ShouldBindJSON(&config); err != nil {
		response.BadRequest(c, "Invalid feature policy")
		return
	}
	result, err := h.settings.UpdatePersonalFeatureSettings(c.Request.Context(), config)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
