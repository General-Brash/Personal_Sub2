package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type CheckinAPIService interface {
	GetStatus(ctx context.Context, userID int64, requestedMonth string) (*service.CheckinStatus, error)
	CheckInAtomic(ctx context.Context, userID int64, claim *service.IdempotencyAtomicClaim) (*service.CheckinResult, error)
}

type CheckinModeAPIService interface {
	CheckInModeAtomic(ctx context.Context, userID int64, mode service.CheckinMode, claim *service.IdempotencyAtomicClaim, expectedPolicy ...string) (*service.CheckinResult, error)
	AutoCheckInAtomic(ctx context.Context, userID int64, claim *service.IdempotencyAtomicClaim) (*service.CheckinResult, error)
	GetPreference(ctx context.Context, userID int64) (*service.CheckinPreference, error)
	UpdatePreference(ctx context.Context, userID int64, autoEnabled, accepted bool, expectedPolicy string, expectedFeeBps int) (*service.CheckinPreference, error)
}

type CheckinHandler struct {
	checkinService CheckinAPIService
}

func NewCheckinHandler(checkinService CheckinAPIService) *CheckinHandler {
	return &CheckinHandler{checkinService: checkinService}
}

// GetStatus handles GET /api/v1/user/check-in.
func (h *CheckinHandler) GetStatus(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	status, err := h.checkinService.GetStatus(c.Request.Context(), subject.UserID, c.Query("month"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

// GetPreference handles GET /api/v1/user/check-in/preference.
func (h *CheckinHandler) GetPreference(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	modeService, ok := h.checkinService.(CheckinModeAPIService)
	if !ok {
		response.ErrorFrom(c, service.ErrCheckinPreferenceInvalid)
		return
	}
	preference, err := modeService.GetPreference(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, preference)
}

type updateCheckinPreferenceRequest struct {
	AutoEnabled    *bool  `json:"auto_enabled"`
	Accepted       bool   `json:"accepted"`
	ExpectedPolicy string `json:"policy_version"`
	ExpectedFeeBps int    `json:"fee_bps"`
}

// UpdatePreference handles PUT /api/v1/user/check-in/preference. Fee and policy
// version from the displayed quote must match the authoritative server policy.
func (h *CheckinHandler) UpdatePreference(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	modeService, ok := h.checkinService.(CheckinModeAPIService)
	if !ok {
		response.ErrorFrom(c, service.ErrCheckinPreferenceInvalid)
		return
	}
	var payload updateCheckinPreferenceRequest
	if err := c.ShouldBindJSON(&payload); err != nil || payload.AutoEnabled == nil {
		response.ErrorFrom(c, service.ErrCheckinPreferenceInvalid)
		return
	}
	preference, err := modeService.UpdatePreference(c.Request.Context(), subject.UserID, *payload.AutoEnabled, payload.Accepted, payload.ExpectedPolicy, payload.ExpectedFeeBps)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, preference)
}

// CheckIn handles POST /api/v1/user/check-in. Empty body and {} retain the
// legacy direct behavior; mode-aware clients may submit direct/normal/super.
func (h *CheckinHandler) CheckIn(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if strings.TrimSpace(c.GetHeader("Idempotency-Key")) == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return
	}
	payload, mode, err := parseCheckinModeBody(c)
	if err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	executeUserAtomicIdempotentJSON(c, "user.daily_checkin.create", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		if modeService, ok := h.checkinService.(CheckinModeAPIService); ok {
			var version string
			if raw, present := payload["policy_version"]; present {
				_ = json.Unmarshal(raw, &version)
			}
			return modeService.CheckInModeAtomic(ctx, subject.UserID, mode, claim, version)
		}
		if mode != service.CheckinModeDirect {
			return nil, service.ErrCheckinModeDisabled
		}
		return h.checkinService.CheckInAtomic(ctx, subject.UserID, claim)
	})
}

// AutoCheckIn handles POST /api/v1/user/check-in/auto. It is intended for the
// global entry hook only; the server still validates preference and consent.
func (h *CheckinHandler) AutoCheckIn(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if strings.TrimSpace(c.GetHeader("Idempotency-Key")) == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return
	}
	modeService, ok := h.checkinService.(CheckinModeAPIService)
	if !ok {
		response.ErrorFrom(c, service.ErrCheckinPreferenceInvalid)
		return
	}
	payload := map[string]json.RawMessage{"mode": json.RawMessage(`"direct-auto"`)}
	executeUserAtomicIdempotentJSON(c, "user.daily_checkin.auto", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		return modeService.AutoCheckInAtomic(ctx, subject.UserID, claim)
	})
}

func parseCheckinModeBody(c *gin.Context) (map[string]json.RawMessage, service.CheckinMode, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return map[string]json.RawMessage{}, service.CheckinModeDirect, nil
	}
	decoder := json.NewDecoder(c.Request.Body)
	var payload map[string]json.RawMessage
	err := decoder.Decode(&payload)
	if errors.Is(err, io.EOF) {
		return map[string]json.RawMessage{}, service.CheckinModeDirect, nil
	}
	if err != nil || payload == nil {
		return nil, "", errors.New("request body must be an empty JSON object or a mode object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, "", errors.New("request body must contain one JSON object")
		}
		return nil, "", err
	}
	if len(payload) == 0 {
		return payload, service.CheckinModeDirect, nil
	}
	rawMode, ok := payload["mode"]
	if !ok || len(payload) > 2 {
		return nil, "", errors.New("request body may only contain mode and policy_version")
	}
	for key, raw := range payload {
		if key != "mode" && key != "policy_version" {
			return nil, "", errors.New("unknown check-in field")
		}
		if key == "policy_version" {
			var version string
			if json.Unmarshal(raw, &version) != nil || strings.TrimSpace(version) == "" {
				return nil, "", errors.New("policy_version must be a nonempty string")
			}
		}
	}
	var mode service.CheckinMode
	if err := json.Unmarshal(rawMode, &mode); err != nil {
		return nil, "", errors.New("mode must be a string")
	}
	switch mode {
	case service.CheckinModeDirect, service.CheckinModeNormal, service.CheckinModeSuper:
		return payload, mode, nil
	default:
		return nil, "", errors.New("mode must be direct, normal, or super")
	}
}
