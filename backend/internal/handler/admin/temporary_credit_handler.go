package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type AdminTemporaryCreditAPIService interface {
	GrantAtomic(ctx context.Context, userID, adminID int64, amount float64, notes string, claim *service.IdempotencyAtomicClaim) (*service.AdminTemporaryCreditGrantResult, error)
	ListAudit(ctx context.Context, userID int64, page, pageSize int) ([]service.TemporaryCreditAuditItem, int64, error)
}

type TemporaryCreditHandler struct {
	service AdminTemporaryCreditAPIService
}

func NewTemporaryCreditHandler(service AdminTemporaryCreditAPIService) *TemporaryCreditHandler {
	return &TemporaryCreditHandler{service: service}
}

type adminTemporaryCreditRequest struct {
	Amount string `json:"amount"`
	Notes  string `json:"notes"`
}

// Grant handles POST /api/v1/admin/users/:id/temporary-credits.
func (h *TemporaryCreditHandler) Grant(c *gin.Context) {
	userID, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator not authenticated")
		return
	}
	if strings.TrimSpace(c.GetHeader("Idempotency-Key")) == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return
	}

	req, err := decodeAdminTemporaryCreditRequest(c)
	if err != nil {
		response.ErrorFrom(c, service.ErrAdminTemporaryCreditInvalidAmount)
		return
	}
	amount, err := service.ParseStrictPositiveLedgerAmount(req.Amount)
	if err != nil {
		response.ErrorFrom(c, service.ErrAdminTemporaryCreditInvalidAmount)
		return
	}
	payload := struct {
		UserID int64  `json:"user_id"`
		Amount string `json:"amount"`
		Notes  string `json:"notes"`
	}{UserID: userID, Amount: req.Amount, Notes: req.Notes}
	executeAdminAtomicIdempotentJSON(c, "admin.users.temporary_credits.grant", payload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		return h.service.GrantAtomic(ctx, userID, subject.UserID, amount, req.Notes, claim)
	})
}

// ListAudit handles GET /api/v1/admin/users/:id/temporary-credits.
func (h *TemporaryCreditHandler) ListAudit(c *gin.Context) {
	userID, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	page, pageSize, err := parseTemporaryCreditPagination(c)
	if err != nil {
		response.ErrorFrom(c, service.ErrTemporaryCreditPaginationInvalid)
		return
	}
	items, total, err := h.service.ListAudit(c.Request.Context(), userID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func decodeAdminTemporaryCreditRequest(c *gin.Context) (adminTemporaryCreditRequest, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return adminTemporaryCreditRequest{}, errors.New("request body is required")
	}
	decoder := json.NewDecoder(c.Request.Body)
	var raw map[string]json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return adminTemporaryCreditRequest{}, err
	}
	if raw == nil {
		return adminTemporaryCreditRequest{}, errors.New("request body must be an object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return adminTemporaryCreditRequest{}, errors.New("request body must contain one object")
		}
		return adminTemporaryCreditRequest{}, err
	}
	for key := range raw {
		if key != "amount" && key != "notes" {
			return adminTemporaryCreditRequest{}, errors.New("request contains an unknown field")
		}
	}
	amountRaw, ok := raw["amount"]
	if !ok || isJSONNull(amountRaw) {
		return adminTemporaryCreditRequest{}, errors.New("amount is required")
	}
	var request adminTemporaryCreditRequest
	if err := json.Unmarshal(amountRaw, &request.Amount); err != nil {
		return adminTemporaryCreditRequest{}, err
	}
	if notesRaw, exists := raw["notes"]; exists {
		if isJSONNull(notesRaw) {
			return adminTemporaryCreditRequest{}, errors.New("notes must be a string")
		}
		if err := json.Unmarshal(notesRaw, &request.Notes); err != nil {
			return adminTemporaryCreditRequest{}, err
		}
	}
	return request, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func parseTemporaryCreditPagination(c *gin.Context) (int, int, error) {
	page := 1
	pageSize := 20
	if raw, exists := c.GetQuery("page"); exists {
		value, err := parseStrictPositiveInt(raw)
		if err != nil {
			return 0, 0, err
		}
		page = value
	}
	if raw, exists := c.GetQuery("page_size"); exists {
		value, err := parseStrictPositiveInt(raw)
		if err != nil || value > 1000 {
			return 0, 0, service.ErrTemporaryCreditPaginationInvalid
		}
		pageSize = value
	}
	return page, pageSize, nil
}

func parseStrictPositiveInt(raw string) (int, error) {
	if raw == "" {
		return 0, service.ErrTemporaryCreditPaginationInvalid
	}
	for _, character := range raw {
		if character < '0' || character > '9' {
			return 0, service.ErrTemporaryCreditPaginationInvalid
		}
	}
	value, err := strconv.ParseUint(raw, 10, 31)
	if err != nil || value == 0 {
		return 0, service.ErrTemporaryCreditPaginationInvalid
	}
	return int(value), nil
}

func parsePositiveID(raw string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, errors.New("id must be positive")
	}
	return value, nil
}
