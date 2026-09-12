package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrMallQuoteChanged = infraerrors.Conflict("MALL_QUOTE_CHANGED", "mall product quote changed; refresh before purchasing")

// mallQuerier is shared by *sql.DB and *sql.Tx. Keeping the loader generic lets
// the quote endpoint and the atomic purchase path use identical row contracts.
type mallQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type MallQuote struct {
	ProductType       MallProductType          `json:"product_type"`
	ProductID         int64                    `json:"product_id"`
	Name              string                   `json:"name"`
	Price             string                   `json:"price"`
	Currency          string                   `json:"currency"`
	PaymentCreditType MallCreditType           `json:"payment_credit_type"`
	CreditedType      *MallCreditType          `json:"credited_type,omitempty"`
	CreditedAmount    *string                  `json:"credited_amount,omitempty"`
	BenefitType       *SubscriptionBenefitType `json:"benefit_type,omitempty"`
	ValidityDays      int                      `json:"validity_days,omitempty"`
	QuoteVersion      string                   `json:"quote_version"`
	PricedAt          time.Time                `json:"priced_at"`
}

func (s *MallService) GetMallQuote(ctx context.Context, userID int64, productType MallProductType, productID int64) (*MallQuote, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("mall service database is nil")
	}
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	if productID <= 0 || (productType != MallProductTypeCurrency && productType != MallProductTypeSubscription) {
		return nil, ErrMallProductNotAvailable
	}
	now := time.Now().UTC()
	switch productType {
	case MallProductTypeCurrency:
		product, err := loadMallCurrencyProduct(ctx, s.db, productID)
		if err != nil {
			return nil, err
		}
		creditedType := product.creditedType
		creditedAmount := formatLedgerAmount(product.creditedAmount)
		return &MallQuote{
			ProductType: productType, ProductID: product.id, Name: product.name,
			Price: formatLedgerAmount(product.price), Currency: "USD", PaymentCreditType: product.paymentType,
			CreditedType: &creditedType, CreditedAmount: &creditedAmount,
			QuoteVersion: mallCurrencyProductQuoteVersion(product), PricedAt: now,
		}, nil
	case MallProductTypeSubscription:
		plan, err := loadMallSubscriptionPlan(ctx, s.db, productID)
		if err != nil {
			return nil, err
		}
		benefitType := plan.benefitType
		currency := strings.TrimSpace(plan.currency)
		if currency == "" {
			currency = "USD"
		}
		return &MallQuote{
			ProductType: productType, ProductID: plan.id, Name: plan.name,
			Price: formatLedgerAmount(plan.price), Currency: currency, PaymentCreditType: plan.paymentType,
			BenefitType: &benefitType, ValidityDays: plan.validityDays,
			QuoteVersion: mallSubscriptionPlanQuoteVersion(plan), PricedAt: now,
		}, nil
	default:
		return nil, ErrMallProductNotAvailable
	}
}

func validateMallExpectedQuoteVersion(expected, actual string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil // legacy clients remain compatible
	}
	if actual == "" || expected != actual {
		return ErrMallQuoteChanged
	}
	return nil
}

func mallCurrencyProductQuoteVersion(product *mallCurrencyProduct) string {
	if product == nil {
		return ""
	}
	return mallQuoteDigest(struct {
		Kind            string `json:"kind"`
		ID              int64  `json:"id"`
		Name            string `json:"name"`
		Price           string `json:"price"`
		PaymentType     string `json:"payment_type"`
		CreditedType    string `json:"credited_type"`
		CreditedAmount  string `json:"credited_amount"`
		DailyLimit      int64  `json:"daily_limit"`
		TotalLimit      int64  `json:"total_limit"`
		LimitUnit       string `json:"limit_unit"`
		LimitMode       string `json:"limit_mode"`
		LimitWindowSize int    `json:"limit_window_size"`
		UpdatedAt       string `json:"updated_at"`
	}{
		Kind: "currency", ID: product.id, Name: product.name, Price: formatLedgerAmount(product.price),
		PaymentType: string(product.paymentType), CreditedType: string(product.creditedType),
		CreditedAmount: formatLedgerAmount(product.creditedAmount), DailyLimit: product.dailyLimit, TotalLimit: product.totalLimit,
		LimitUnit: product.purchaseLimitUnit, LimitMode: product.purchaseLimitMode, LimitWindowSize: product.purchaseLimitWindowSize,
		UpdatedAt: product.updatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func mallSubscriptionPlanQuoteVersion(plan *mallSubscriptionPlan) string {
	if plan == nil {
		return ""
	}
	return mallQuoteDigest(struct {
		Kind            string `json:"kind"`
		ID              int64  `json:"id"`
		GroupID         int64  `json:"group_id"`
		Name            string `json:"name"`
		Price           string `json:"price"`
		Currency        string `json:"currency"`
		PaymentType     string `json:"payment_type"`
		BenefitType     string `json:"benefit_type"`
		DailyAmount     string `json:"daily_amount"`
		ValidityDays    int    `json:"validity_days"`
		DailyLimit      int    `json:"daily_limit"`
		TotalLimit      int    `json:"total_limit"`
		LimitUnit       string `json:"limit_unit"`
		LimitMode       string `json:"limit_mode"`
		LimitWindowSize int    `json:"limit_window_size"`
		UpdatedAt       string `json:"updated_at"`
	}{
		Kind: "subscription", ID: plan.id, GroupID: plan.groupID, Name: plan.name, Price: formatLedgerAmount(plan.price),
		Currency: plan.currency, PaymentType: string(plan.paymentType), BenefitType: string(plan.benefitType),
		DailyAmount: formatLedgerAmount(plan.dailyAmount), ValidityDays: plan.validityDays, DailyLimit: plan.dailyLimit,
		TotalLimit: plan.totalLimit, LimitUnit: plan.purchaseLimitUnit, LimitMode: plan.purchaseLimitMode,
		LimitWindowSize: plan.purchaseLimitWindowSize, UpdatedAt: plan.updatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func mallQuoteDigest(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "sha256:unavailable"
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
