package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var ErrUsageBillingRequestIDRequired = errors.New("usage billing request_id is required")
var ErrUsageBillingRequestConflict = errors.New("usage billing request fingerprint conflict")
var ErrUsageBillingAmountInvalid = errors.New("usage billing amount is invalid")

// UsageBillingCommand describes one billable request that must be applied at most once.
type UsageBillingCommand struct {
	RequestID          string
	APIKeyID           int64
	RequestFingerprint string
	RequestPayloadHash string
	UsageLog           *UsageLog

	UserID              int64
	AccountID           int64
	SubscriptionID      *int64
	AccountType         string
	Model               string
	ServiceTier         string
	ReasoningEffort     string
	BillingType         int8
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	ImageCount          int
	MediaType           string

	BalanceCost         float64
	SubscriptionCost    float64
	APIKeyQuotaCost     float64
	APIKeyRateLimitCost float64
	AccountQuotaCost    float64
}

func (c *UsageBillingCommand) Normalize() error {
	if c == nil {
		return nil
	}
	c.RequestID = strings.TrimSpace(c.RequestID)
	amounts := []*float64{
		&c.BalanceCost,
		&c.SubscriptionCost,
		&c.APIKeyQuotaCost,
		&c.APIKeyRateLimitCost,
		&c.AccountQuotaCost,
	}
	for _, amount := range amounts {
		normalized, err := normalizeUsageBillingAmount(*amount)
		if err != nil {
			return err
		}
		*amount = normalized
	}
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = buildUsageBillingFingerprint(c)
	}
	return nil
}

func normalizeUsageBillingAmount(amount float64) (float64, error) {
	normalized, err := normalizeLedgerAmount(amount)
	if err != nil || normalized < 0 {
		return 0, ErrUsageBillingAmountInvalid
	}
	return normalized, nil
}

// usageBillingLedgerAmountFromFloat64 normalizes legacy cost calculations once
// before temporary and permanent credit are split.
func usageBillingLedgerAmountFromFloat64(amount float64) float64 {
	normalized, err := normalizeLedgerAmount(amount)
	if err != nil {
		return amount
	}
	return normalized
}

func buildUsageBillingFingerprint(c *UsageBillingCommand) string {
	if c == nil {
		return ""
	}
	raw := fmt.Sprintf(
		"%d|%d|%d|%s|%s|%s|%s|%d|%d|%d|%d|%d|%d|%s|%d|%s|%s|%s|%s|%s",
		c.UserID,
		c.AccountID,
		c.APIKeyID,
		strings.TrimSpace(c.AccountType),
		strings.TrimSpace(c.Model),
		strings.TrimSpace(c.ServiceTier),
		strings.TrimSpace(c.ReasoningEffort),
		c.BillingType,
		c.InputTokens,
		c.OutputTokens,
		c.CacheCreationTokens,
		c.CacheReadTokens,
		c.ImageCount,
		strings.TrimSpace(c.MediaType),
		valueOrZero(c.SubscriptionID),
		formatLedgerAmount(c.BalanceCost),
		formatLedgerAmount(c.SubscriptionCost),
		formatLedgerAmount(c.APIKeyQuotaCost),
		formatLedgerAmount(c.APIKeyRateLimitCost),
		formatLedgerAmount(c.AccountQuotaCost),
	)
	if payloadHash := strings.TrimSpace(c.RequestPayloadHash); payloadHash != "" {
		raw += "|" + payloadHash
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func HashUsageRequestPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func valueOrZero(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// AccountQuotaState holds the post-increment quota state returned by the DB transaction.
// All values are post-update (i.e., already include the increment).
type AccountQuotaState struct {
	TotalUsed   float64
	TotalLimit  float64
	DailyUsed   float64
	DailyLimit  float64
	WeeklyUsed  float64
	WeeklyLimit float64
}

type UsageBillingApplyResult struct {
	Applied                   bool
	APIKeyQuotaExhausted      bool
	NewBalance                *float64           // post-deduction balance (nil = no balance deduction)
	PermanentBalanceDeduction *float64           // amount not covered by temporary credit and deducted from permanent balance
	BalanceOverdrafted        bool               // true when the sufficient-balance guard missed and debt was still recorded
	QuotaState                *AccountQuotaState // post-increment quota state (nil = no quota increment)
}

func permanentBalanceCacheDeduction(result *UsageBillingApplyResult) (float64, bool) {
	if result == nil || result.PermanentBalanceDeduction == nil {
		return 0, false
	}
	return *result.PermanentBalanceDeduction, true
}

func permanentBalanceNotificationInputs(result *UsageBillingApplyResult, fallbackBalance float64) (float64, float64, bool) {
	deduction, known := permanentBalanceCacheDeduction(result)
	if !known || deduction <= 0 {
		return 0, 0, false
	}
	if result.NewBalance != nil {
		return *result.NewBalance + deduction, deduction, true
	}
	return fallbackBalance, deduction, true
}

// BatchImageBalanceHoldCommand describes an idempotent balance hold operation.
type BatchImageBalanceHoldCommand struct {
	RequestID          string
	APIKeyID           int64
	RequestFingerprint string
	RequestPayloadHash string
	UserID             int64
	BatchID            string
	HoldAmount         float64
	ActualAmount       float64
}

func (c *BatchImageBalanceHoldCommand) Normalize() {
	if c == nil {
		return
	}
	c.RequestID = strings.TrimSpace(c.RequestID)
	c.BatchID = strings.TrimSpace(c.BatchID)
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = buildBatchImageBalanceHoldFingerprint(c)
	}
}

func buildBatchImageBalanceHoldFingerprint(c *BatchImageBalanceHoldCommand) string {
	if c == nil {
		return ""
	}
	raw := fmt.Sprintf(
		"%d|%d|%s|%0.10f|%0.10f",
		c.UserID,
		c.APIKeyID,
		strings.TrimSpace(c.BatchID),
		c.HoldAmount,
		c.ActualAmount,
	)
	if payloadHash := strings.TrimSpace(c.RequestPayloadHash); payloadHash != "" {
		raw += "|" + payloadHash
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type BatchImageBalanceHoldResult struct {
	Applied       bool
	NewBalance    *float64
	FrozenBalance *float64
}

type UsageBillingRepository interface {
	Apply(ctx context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error)
	ReserveBatchImageBalance(ctx context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error)
	CaptureBatchImageBalance(ctx context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error)
	ReleaseBatchImageBalance(ctx context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error)
}
