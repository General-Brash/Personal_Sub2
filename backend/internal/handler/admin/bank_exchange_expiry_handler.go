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

// BankExchangeExpiryAPIService is intentionally separate from the legacy
// bank-policy service so route wiring can ship the expiry policy independently.
type BankExchangeExpiryAPIService interface {
	GetExchangeExpiryPolicy(ctx context.Context) (*service.BankExchangeExpiryPolicyDTO, error)
	UpdateExchangeExpiryPolicyAtomic(ctx context.Context, actorID int64, policy service.BankExchangeExpiryPolicyDTO, claim *service.IdempotencyAtomicClaim) (*service.BankExchangeExpiryPolicyDTO, error)
}

type BankExchangeExpiryHandler struct {
	service BankExchangeExpiryAPIService
}

func NewBankExchangeExpiryHandler(bankService BankExchangeExpiryAPIService) *BankExchangeExpiryHandler {
	return &BankExchangeExpiryHandler{service: bankService}
}

// GetPolicy handles GET /api/v1/admin/settings/bank/exchange-expiry after route wiring.
func (h *BankExchangeExpiryHandler) GetPolicy(c *gin.Context) {
	policy, err := h.service.GetExchangeExpiryPolicy(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

// UpdatePolicy handles PUT /api/v1/admin/settings/bank/exchange-expiry after route wiring.
func (h *BankExchangeExpiryHandler) UpdatePolicy(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Administrator not authenticated")
		return
	}
	if strings.TrimSpace(c.GetHeader("Idempotency-Key")) == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return
	}
	policy, err := decodeBankExchangeExpiryPolicyRequest(c)
	if err != nil {
		response.ErrorFrom(c, service.ErrBankExchangeExpiryPolicyInvalid)
		return
	}
	executeAdminAtomicIdempotentJSON(c, "admin.settings.bank.exchange_expiry.update", policy, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		return h.service.UpdateExchangeExpiryPolicyAtomic(ctx, subject.UserID, policy, claim)
	})
}

func decodeBankExchangeExpiryPolicyRequest(c *gin.Context) (service.BankExchangeExpiryPolicyDTO, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return service.BankExchangeExpiryPolicyDTO{}, errors.New("request body is required")
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var policy service.BankExchangeExpiryPolicyDTO
	if err := decoder.Decode(&policy); err != nil {
		return service.BankExchangeExpiryPolicyDTO{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return service.BankExchangeExpiryPolicyDTO{}, errors.New("request body must contain one object")
		}
		return service.BankExchangeExpiryPolicyDTO{}, err
	}
	return policy, nil
}
