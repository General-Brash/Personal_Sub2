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

// CheckIn handles POST /api/v1/user/check-in.
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

	payload, err := parseEmptyCheckinJSONBody(c)
	if err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	executeUserAtomicIdempotentJSON(c, "user.daily_checkin.create", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		return h.checkinService.CheckInAtomic(ctx, subject.UserID, claim)
	})
}

func parseEmptyCheckinJSONBody(c *gin.Context) (map[string]json.RawMessage, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return map[string]json.RawMessage{}, nil
	}
	decoder := json.NewDecoder(c.Request.Body)
	var payload map[string]json.RawMessage
	err := decoder.Decode(&payload)
	if errors.Is(err, io.EOF) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil || payload == nil {
		if err == nil {
			err = errors.New("request body must be an empty JSON object")
		}
		return nil, err
	}
	if len(payload) != 0 {
		return nil, errors.New("request body must be an empty JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("request body must contain one JSON object")
		}
		return nil, err
	}
	return payload, nil
}
