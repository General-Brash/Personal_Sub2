package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// InvitationHandler exposes the W08 player invitation endpoints. The main
// integration pass owns route registration because shared routes are outside
// this change's write scope.
//
// Suggested routes:
//
//	GET    /api/v1/user/invitations
//	POST   /api/v1/user/invitations/reservations
//	POST   /api/v1/user/invitations/reservations/:id/cancel
//	POST   /api/v1/admin/invitations/quota-adjust
//	POST   /api/v1/admin/affiliates/relationships
type InvitationHandler struct {
	service *service.PlayerInvitationService
}

func NewInvitationHandler(authService *service.AuthService) *InvitationHandler {
	if authService == nil {
		return &InvitationHandler{}
	}
	return &InvitationHandler{service: authService.PlayerInvitationService()}
}

func NewInvitationHandlerWithService(svc *service.PlayerInvitationService) *InvitationHandler {
	return &InvitationHandler{service: svc}
}

func (h *InvitationHandler) actorUserID(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.ErrorFrom(c, service.ErrUserNotActive)
		return 0, false
	}
	return subject.UserID, true
}

func (h *InvitationHandler) serviceOrError(c *gin.Context) (*service.PlayerInvitationService, bool) {
	if h == nil || h.service == nil {
		response.ErrorFrom(c, service.ErrServiceUnavailable)
		return nil, false
	}
	return h.service, true
}

func (h *InvitationHandler) ListMyInvitations(c *gin.Context) {
	svc, ok := h.serviceOrError(c)
	if !ok {
		return
	}
	userID, ok := h.actorUserID(c)
	if !ok {
		return
	}
	summary, err := svc.GetSummary(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	reservations, err := svc.ListReservations(c.Request.Context(), userID, 20)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"summary": summary, "reservations": reservations})
}

func (h *InvitationHandler) CreateMyInvitationReservation(c *gin.Context) {
	svc, ok := h.serviceOrError(c)
	if !ok {
		return
	}
	userID, ok := h.actorUserID(c)
	if !ok {
		return
	}
	credential, err := svc.Reserve(c.Request.Context(), userID, c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, credential)
}

func (h *InvitationHandler) CancelMyInvitationReservation(c *gin.Context) {
	svc, ok := h.serviceOrError(c)
	if !ok {
		return
	}
	userID, ok := h.actorUserID(c)
	if !ok {
		return
	}
	reservationID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || reservationID <= 0 {
		response.BadRequest(c, "invalid reservation id")
		return
	}
	if err := svc.Cancel(c.Request.Context(), userID, reservationID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"cancelled": true})
}

type invitationQuotaAdjustRequest struct {
	TargetUserID   int64  `json:"target_user_id" binding:"required"`
	Delta          int    `json:"delta" binding:"required"`
	Reason         string `json:"reason" binding:"required"`
	IdempotencyKey string `json:"idempotency_key" binding:"required"`
	RequestID      string `json:"request_id,omitempty"`
}

func (h *InvitationHandler) AdminAdjustInvitationQuota(c *gin.Context) {
	svc, ok := h.serviceOrError(c)
	if !ok {
		return
	}
	actorID, ok := h.actorUserID(c)
	if !ok {
		return
	}
	var req invitationQuotaAdjustRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request: "+err.Error())
		return
	}
	if err := svc.AdminAdjustQuota(c.Request.Context(), service.InvitationQuotaAdjustment{
		ActorUserID:    actorID,
		TargetUserID:   req.TargetUserID,
		Delta:          req.Delta,
		Reason:         req.Reason,
		IdempotencyKey: req.IdempotencyKey,
		RequestID:      req.RequestID,
	}); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"adjusted": true})
}

type invitationRelationshipCreateRequest struct {
	InviterUserID int64  `json:"inviter_user_id" binding:"required"`
	InviteeUserID int64  `json:"invitee_user_id" binding:"required"`
	EffectiveAt   string `json:"effective_at,omitempty"`
	Reason        string `json:"reason" binding:"required"`
	RequestID     string `json:"request_id,omitempty"`
}

func (h *InvitationHandler) AdminCreateInvitationRelationship(c *gin.Context) {
	svc, ok := h.serviceOrError(c)
	if !ok {
		return
	}
	actorID, ok := h.actorUserID(c)
	if !ok {
		return
	}
	var req invitationRelationshipCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request: "+err.Error())
		return
	}
	if strings.TrimSpace(req.EffectiveAt) != "" {
		response.BadRequest(c, "effective_at is assigned by the server; historical backfill is not supported")
		return
	}
	effectiveAt := time.Now().UTC()

	relationship, err := svc.AdminCreateRelationship(c.Request.Context(), service.InvitationRelationshipCreateInput{
		ActorUserID:   actorID,
		InviterUserID: req.InviterUserID,
		InviteeUserID: req.InviteeUserID,
		EffectiveAt:   effectiveAt,
		Reason:        req.Reason,
		RequestID:     req.RequestID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, relationship)
}

func readInvitationCodeFromRequest(c *gin.Context) string {
	return validatedOAuthInvitation(c).Invitation
}
func readAffiliateCodeFromRequest(c *gin.Context) string {
	return validatedOAuthInvitation(c).Affiliate
}

func (h *InvitationHandler) AdminPreviewRelationship(c *gin.Context) {
	svc, ok := h.serviceOrError(c)
	if !ok {
		return
	}
	actor, ok := h.actorUserID(c)
	if !ok {
		return
	}
	var req struct {
		Inviter int64 `json:"inviter_user_id"`
		Invitee int64 `json:"invitee_user_id"`
	}
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "Invalid relationship preview")
		return
	}
	result, err := svc.AdminPreview(c.Request.Context(), actor, req.Inviter, req.Invitee)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
