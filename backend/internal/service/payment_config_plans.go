package service

import (
	"context"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// normalizePlanCurrency validates and normalizes the display-only currency label.
// Empty means "no label" and is kept as-is so existing plans stay unchanged.
func normalizePlanCurrency(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	currency, err := payment.NormalizePaymentCurrency(raw)
	if err != nil {
		return "", infraerrors.BadRequest("PLAN_CURRENCY_INVALID", "currency must be a 3-letter ISO currency code")
	}
	return currency, nil
}

// validatePlanRequired checks that all required fields for a plan are provided.
func validatePlanRequired(name string, groupID int64, price float64, validityDays int, validityUnit string, originalPrice *float64) error {
	if strings.TrimSpace(name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if groupID <= 0 {
		return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "group is required")
	}
	if price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if validityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if strings.TrimSpace(validityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if originalPrice != nil && *originalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	return nil
}

func validatePlanDefinition(name string, groupID int64, price float64, validityDays int, validityUnit string, originalPrice *float64, benefitType, paymentCreditType string, dailyAmount float64) (SubscriptionBenefitType, MallCreditType, float64, float64, error) {
	benefit, err := normalizeSubscriptionBenefitType(benefitType)
	if err != nil {
		return "", "", 0, 0, err
	}
	paymentType, err := normalizeMallCreditType(paymentCreditType)
	if err != nil {
		return "", "", 0, 0, err
	}
	if benefit == SubscriptionBenefitSub2 {
		if err := validatePlanRequired(name, groupID, price, validityDays, validityUnit, originalPrice); err != nil {
			return "", "", 0, 0, err
		}
		if dailyAmount != 0 {
			return "", "", 0, 0, infraerrors.BadRequest("PLAN_DAILY_CREDIT_INVALID", "sub2 plans cannot grant daily temporary credit")
		}
	} else {
		if strings.TrimSpace(name) == "" {
			return "", "", 0, 0, infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
		}
		if groupID != 0 {
			return "", "", 0, 0, infraerrors.BadRequest("PLAN_GROUP_INVALID", "daily temporary credit plans must not select a group")
		}
		if validityDays <= 0 || validityDays > 3650 || strings.TrimSpace(validityUnit) != "day" {
			return "", "", 0, 0, infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "daily temporary credit plans require 1-3650 days")
		}
		if _, err := validateCurrencyProductCreditedAmount(dailyAmount); err != nil {
			return "", "", 0, 0, infraerrors.BadRequest("PLAN_DAILY_CREDIT_INVALID", "daily temporary credit amount must be positive")
		}
		if originalPrice != nil && *originalPrice < 0 {
			return "", "", 0, 0, infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
		}
	}
	normalizedPrice, err := normalizeLedgerAmount(price)
	if err != nil || normalizedPrice <= 0 {
		return "", "", 0, 0, infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be a positive amount with at most eight decimals")
	}
	normalizedDaily := float64(0)
	if dailyAmount > 0 {
		normalizedDaily, _ = normalizeLedgerAmount(dailyAmount)
	}
	return benefit, paymentType, normalizedPrice, normalizedDaily, nil
}

// validatePlanPatch validates only the non-nil fields in a patch update.
func validatePlanPatch(req UpdatePlanRequest) error {
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	allowZeroGroup := req.BenefitType != nil && strings.TrimSpace(*req.BenefitType) == string(SubscriptionBenefitDailyTemporaryCredit)
	if req.GroupID != nil && (*req.GroupID < 0 || (*req.GroupID == 0 && !allowZeroGroup)) {
		return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "group is required")
	}
	if req.Price != nil && *req.Price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if req.ValidityDays != nil && *req.ValidityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if req.ValidityUnit != nil && strings.TrimSpace(*req.ValidityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if req.OriginalPrice != nil && *req.OriginalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	if req.BenefitType != nil {
		if _, err := normalizeSubscriptionBenefitType(*req.BenefitType); err != nil {
			return err
		}
	}
	if req.PaymentCreditType != nil {
		if _, err := normalizeMallCreditType(*req.PaymentCreditType); err != nil {
			return err
		}
	}
	if req.DailyTemporaryCreditAmount != nil && *req.DailyTemporaryCreditAmount < 0 {
		return infraerrors.BadRequest("PLAN_DAILY_CREDIT_INVALID", "daily temporary credit amount must be non-negative")
	}
	if err := validatePurchaseLimitPatch(req.DailyPurchaseLimit, req.TotalPurchaseLimit); err != nil {
		return err
	}
	return nil
}

// --- Plan CRUD ---

// PlanGroupInfo holds the group details needed for subscription plan display.
type PlanGroupInfo struct {
	Platform           string   `json:"platform"`
	Name               string   `json:"name"`
	RateMultiplier     float64  `json:"rate_multiplier"`
	PeakRateEnabled    bool     `json:"peak_rate_enabled"`
	PeakStart          string   `json:"peak_start"`
	PeakEnd            string   `json:"peak_end"`
	PeakRateMultiplier float64  `json:"peak_rate_multiplier"`
	DailyLimitUSD      *float64 `json:"daily_limit_usd"`
	WeeklyLimitUSD     *float64 `json:"weekly_limit_usd"`
	MonthlyLimitUSD    *float64 `json:"monthly_limit_usd"`
	ModelScopes        []string `json:"supported_model_scopes"`
}

// GetGroupInfoMap returns a map of group_id → PlanGroupInfo for the given plans.
func (s *PaymentConfigService) GetGroupInfoMap(ctx context.Context, plans []*dbent.SubscriptionPlan) map[int64]PlanGroupInfo {
	ids := make([]int64, 0, len(plans))
	seen := make(map[int64]bool)
	for _, p := range plans {
		if p.GroupID > 0 && !seen[p.GroupID] {
			seen[p.GroupID] = true
			ids = append(ids, p.GroupID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	groups, err := s.entClient.Group.Query().Where(group.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil
	}
	m := make(map[int64]PlanGroupInfo, len(groups))
	for _, g := range groups {
		m[int64(g.ID)] = PlanGroupInfo{
			Platform:           g.Platform,
			Name:               g.Name,
			RateMultiplier:     g.RateMultiplier,
			PeakRateEnabled:    g.PeakRateEnabled,
			PeakStart:          g.PeakStart,
			PeakEnd:            g.PeakEnd,
			PeakRateMultiplier: g.PeakRateMultiplier,
			DailyLimitUSD:      g.DailyLimitUsd,
			WeeklyLimitUSD:     g.WeeklyLimitUsd,
			MonthlyLimitUSD:    g.MonthlyLimitUsd,
			ModelScopes:        g.SupportedModelScopes,
		}
	}
	return m
}

func (s *PaymentConfigService) ListPlans(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) ListPlansForSale(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Where(subscriptionplan.ForSaleEQ(true)).Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) CreatePlan(ctx context.Context, req CreatePlanRequest) (*dbent.SubscriptionPlan, error) {
	benefit, paymentType, price, dailyAmount, err := validatePlanDefinition(req.Name, req.GroupID, req.Price, req.ValidityDays, req.ValidityUnit, req.OriginalPrice, req.BenefitType, req.PaymentCreditType, req.DailyTemporaryCreditAmount)
	if err != nil {
		return nil, err
	}
	currency, err := normalizePlanCurrency(req.Currency)
	if err != nil {
		return nil, err
	}
	if err := validatePurchaseLimits(req.DailyPurchaseLimit, req.TotalPurchaseLimit); err != nil {
		return nil, err
	}
	b := s.entClient.SubscriptionPlan.Create().
		SetGroupID(req.GroupID).SetName(req.Name).SetDescription(req.Description).
		SetPrice(price).SetCurrency(currency).SetValidityDays(req.ValidityDays).SetValidityUnit(req.ValidityUnit).
		SetBenefitType(string(benefit)).SetPaymentCreditType(string(paymentType)).SetDailyTemporaryCreditAmount(dailyAmount).
		SetFeatures(req.Features).SetProductName(req.ProductName).
		SetForSale(req.ForSale).SetSortOrder(req.SortOrder).
		SetDailyPurchaseLimit(req.DailyPurchaseLimit).SetTotalPurchaseLimit(req.TotalPurchaseLimit)
	if req.OriginalPrice != nil {
		b.SetOriginalPrice(*req.OriginalPrice)
	}
	return b.Save(ctx)
}

// UpdatePlan updates a subscription plan by ID (patch semantics).
// NOTE: This function exceeds 30 lines due to per-field nil-check patch update boilerplate
// plus a validation guard for non-nil fields.
func (s *PaymentConfigService) UpdatePlan(ctx context.Context, id int64, req UpdatePlanRequest) (*dbent.SubscriptionPlan, error) {
	if err := validatePlanPatch(req); err != nil {
		return nil, err
	}
	current, err := s.GetPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	name, groupID, price := current.Name, current.GroupID, current.Price
	validityDays, validityUnit := current.ValidityDays, current.ValidityUnit
	benefitType, paymentCreditType, dailyAmount := current.BenefitType, current.PaymentCreditType, current.DailyTemporaryCreditAmount
	originalPrice := current.OriginalPrice
	if req.Name != nil {
		name = *req.Name
	}
	if req.GroupID != nil {
		groupID = *req.GroupID
	}
	if req.Price != nil {
		price = *req.Price
	}
	if req.ValidityDays != nil {
		validityDays = *req.ValidityDays
	}
	if req.ValidityUnit != nil {
		validityUnit = *req.ValidityUnit
	}
	if req.BenefitType != nil {
		benefitType = *req.BenefitType
	}
	if req.PaymentCreditType != nil {
		paymentCreditType = *req.PaymentCreditType
	}
	if req.DailyTemporaryCreditAmount != nil {
		dailyAmount = *req.DailyTemporaryCreditAmount
	}
	if req.OriginalPrice != nil {
		originalPrice = req.OriginalPrice
	}
	benefit, paymentType, normalizedPrice, normalizedDaily, err := validatePlanDefinition(name, groupID, price, validityDays, validityUnit, originalPrice, benefitType, paymentCreditType, dailyAmount)
	if err != nil {
		return nil, err
	}
	u := s.entClient.SubscriptionPlan.UpdateOneID(id)
	if req.GroupID != nil {
		u.SetGroupID(*req.GroupID)
	}
	if req.Name != nil {
		u.SetName(*req.Name)
	}
	if req.Description != nil {
		u.SetDescription(*req.Description)
	}
	if req.Price != nil {
		u.SetPrice(normalizedPrice)
	}
	if req.OriginalPrice != nil {
		u.SetOriginalPrice(*req.OriginalPrice)
	}
	if req.Currency != nil {
		currency, err := normalizePlanCurrency(*req.Currency)
		if err != nil {
			return nil, err
		}
		u.SetCurrency(currency)
	}
	if req.ValidityDays != nil {
		u.SetValidityDays(*req.ValidityDays)
	}
	if req.ValidityUnit != nil {
		u.SetValidityUnit(*req.ValidityUnit)
	}
	if req.Features != nil {
		u.SetFeatures(*req.Features)
	}
	if req.ProductName != nil {
		u.SetProductName(*req.ProductName)
	}
	if req.ForSale != nil {
		u.SetForSale(*req.ForSale)
	}
	if req.SortOrder != nil {
		u.SetSortOrder(*req.SortOrder)
	}
	if req.DailyPurchaseLimit != nil {
		u.SetDailyPurchaseLimit(*req.DailyPurchaseLimit)
	}
	if req.TotalPurchaseLimit != nil {
		u.SetTotalPurchaseLimit(*req.TotalPurchaseLimit)
	}
	if req.BenefitType != nil {
		u.SetBenefitType(string(benefit))
	}
	if req.PaymentCreditType != nil {
		u.SetPaymentCreditType(string(paymentType))
	}
	if req.DailyTemporaryCreditAmount != nil {
		u.SetDailyTemporaryCreditAmount(normalizedDaily)
	}
	return u.Save(ctx)
}

func (s *PaymentConfigService) DeletePlan(ctx context.Context, id int64) error {
	count, err := s.countPendingOrdersByPlan(ctx, id)
	if err != nil {
		return fmt.Errorf("check pending orders: %w", err)
	}
	if count > 0 {
		return infraerrors.Conflict("PENDING_ORDERS",
			fmt.Sprintf("this plan has %d in-progress orders and cannot be deleted — wait for orders to complete first", count))
	}
	return s.entClient.SubscriptionPlan.DeleteOneID(id).Exec(ctx)
}

// GetPlan returns a subscription plan by ID.
func (s *PaymentConfigService) GetPlan(ctx context.Context, id int64) (*dbent.SubscriptionPlan, error) {
	plan, err := s.entClient.SubscriptionPlan.Get(ctx, id)
	if err != nil {
		return nil, infraerrors.NotFound("PLAN_NOT_FOUND", "subscription plan not found")
	}
	return plan, nil
}
