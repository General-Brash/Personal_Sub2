package admin

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

type AdminBankAPIService interface {
	GetPolicy(ctx context.Context) (service.BankPolicyDTO, error)
	UpdatePolicyAtomic(ctx context.Context, actorID int64, policy service.BankPolicyDTO, claim *service.IdempotencyAtomicClaim) (*service.BankPolicyDTO, error)
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
