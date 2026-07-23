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

type AdminBankAPIService interface {
	GetPolicy(ctx context.Context) (service.BankPolicyDTO, error)
	UpdatePolicyAtomic(ctx context.Context, actorID int64, policy service.BankPolicyDTO, claim *service.IdempotencyAtomicClaim) (*service.BankPolicyDTO, error)
	ListAdminLedger(ctx context.Context, userID int64, page int) ([]service.BankAdminLedgerItem, int64, error)
}

type BankHandler struct {
	service AdminBankAPIService
}

func NewBankHandler(bankService AdminBankAPIService) *BankHandler {
	return &BankHandler{service: bankService}
}

// GetPolicy handles GET /api/v1/admin/settings/bank.
func (h *BankHandler) GetPolicy(c *gin.Context) {
	policy, err := h.service.GetPolicy(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

// ListTransactions returns administrator-only bank transaction history.
// GET /api/v1/admin/settings/bank/transactions
//
// The service owns the fixed page size and stable ordering.  The handler only
// accepts a positive page number and an optional positive user id so malformed
// filters cannot accidentally broaden a scoped query into a site-wide query.
func (h *BankHandler) ListTransactions(c *gin.Context) {
	page, err := parseFixedLedgerPage(c.Query("page"))
	if err != nil {
		response.BadRequest(c, "Invalid page")
		return
	}
	userID, err := parseOptionalPositiveID(c.Query("user_id"))
	if err != nil {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	items, total, err := h.service.ListAdminLedger(c.Request.Context(), userID, page)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, 20)
}

func parseFixedLedgerPage(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 1, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, errors.New("page must be positive")
	}
	return value, nil
}

func parseOptionalPositiveID(raw string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return 0, errors.New("id must be positive")
	}
	return value, nil
}

// UpdatePolicy handles PUT /api/v1/admin/settings/bank.
func (h *BankHandler) UpdatePolicy(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator not authenticated")
		return
	}
	if strings.TrimSpace(c.GetHeader("Idempotency-Key")) == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return
	}
	policy, err := decodeBankPolicyRequest(c)
	if err != nil {
		response.ErrorFrom(c, service.ErrBankPolicyInvalid)
		return
	}
	executeAdminAtomicIdempotentJSON(c, "admin.settings.bank.update", policy, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		return h.service.UpdatePolicyAtomic(ctx, subject.UserID, policy, claim)
	})
}

func decodeBankPolicyRequest(c *gin.Context) (service.BankPolicyDTO, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return service.BankPolicyDTO{}, errors.New("request body is required")
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var policy service.BankPolicyDTO
	if err := decoder.Decode(&policy); err != nil {
		return service.BankPolicyDTO{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return service.BankPolicyDTO{}, errors.New("request body must contain one object")
		}
		return service.BankPolicyDTO{}, err
	}
	return policy, nil
}
