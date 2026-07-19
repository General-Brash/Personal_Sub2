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

type BankAPIService interface {
	GetStatus(ctx context.Context, userID int64) (*service.BankStatus, error)
	AdvanceAtomic(ctx context.Context, userID int64, amount float64, claim *service.IdempotencyAtomicClaim) (*service.BankAdvanceResult, error)
	ExchangeAtomic(ctx context.Context, userID int64, permanentAmount float64, claim *service.IdempotencyAtomicClaim) (*service.BankExchangeResult, error)
}

type BankHandler struct {
	service BankAPIService
}

func NewBankHandler(bankService BankAPIService) *BankHandler {
	return &BankHandler{service: bankService}
}

type bankAmountRequest struct {
	Amount string `json:"amount"`
}

// GetStatus handles GET /api/v1/bank/status.
func (h *BankHandler) GetStatus(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	status, err := h.service.GetStatus(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

// Advance handles POST /api/v1/bank/advance.
func (h *BankHandler) Advance(c *gin.Context) {
	h.executeAmountMutation(c, "user.bank.advance", func(ctx context.Context, userID int64, amount float64, claim *service.IdempotencyAtomicClaim) (any, error) {
		return h.service.AdvanceAtomic(ctx, userID, amount, claim)
	})
}

// Exchange handles POST /api/v1/bank/exchange.
func (h *BankHandler) Exchange(c *gin.Context) {
	h.executeAmountMutation(c, "user.bank.exchange", func(ctx context.Context, userID int64, amount float64, claim *service.IdempotencyAtomicClaim) (any, error) {
		return h.service.ExchangeAtomic(ctx, userID, amount, claim)
	})
}

func (h *BankHandler) executeAmountMutation(
	c *gin.Context,
	scope string,
	execute func(context.Context, int64, float64, *service.IdempotencyAtomicClaim) (any, error),
) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if strings.TrimSpace(c.GetHeader("Idempotency-Key")) == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return
	}
	req, err := decodeBankAmountRequest(c)
	if err != nil {
		response.ErrorFrom(c, service.ErrBankAmountInvalid)
		return
	}
	amount, err := service.ParseStrictPositiveLedgerAmount(req.Amount)
	if err != nil {
		response.ErrorFrom(c, service.ErrBankAmountInvalid)
		return
	}
	executeUserAtomicIdempotentJSON(c, scope, req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		return execute(ctx, subject.UserID, amount, claim)
	})
}

func decodeBankAmountRequest(c *gin.Context) (bankAmountRequest, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return bankAmountRequest{}, errors.New("request body is required")
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var req bankAmountRequest
	if err := decoder.Decode(&req); err != nil {
		return bankAmountRequest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return bankAmountRequest{}, errors.New("request body must contain one object")
		}
		return bankAmountRequest{}, err
	}
	if strings.TrimSpace(req.Amount) == "" {
		return bankAmountRequest{}, errors.New("amount is required")
	}
	return req, nil
}
