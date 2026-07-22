package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrMallProductNotAvailable          = infraerrors.NotFound("MALL_PRODUCT_NOT_AVAILABLE", "mall product is not available")
	ErrMallPermanentCreditInsufficient  = infraerrors.Conflict("MALL_INSUFFICIENT_PERMANENT_CREDIT", "permanent credit is insufficient")
	ErrMallTemporaryCreditInsufficient  = infraerrors.Conflict("MALL_INSUFFICIENT_TEMPORARY_CREDIT", "temporary credit is insufficient")
	ErrMallSubscriptionGroupUnavailable = infraerrors.Conflict("MALL_SUBSCRIPTION_GROUP_UNAVAILABLE", "subscription group is unavailable")
)

type MallBalanceSummary struct {
	PermanentBalance         string `json:"permanent_balance"`
	TemporaryCreditAvailable string `json:"temporary_credit_available"`
}

type MallPurchaseRequest struct {
	ProductType MallProductType `json:"product_type"`
	ProductID   int64           `json:"product_id"`
}

type MallPurchaseResult struct {
	PurchaseID               int64                    `json:"purchase_id"`
	ProductType              MallProductType          `json:"product_type"`
	ProductID                int64                    `json:"product_id"`
	PaymentCreditType        MallCreditType           `json:"payment_credit_type"`
	Price                    string                   `json:"price"`
	CreditedType             *MallCreditType          `json:"credited_type,omitempty"`
	CreditedAmount           *string                  `json:"credited_amount,omitempty"`
	BenefitType              *SubscriptionBenefitType `json:"benefit_type,omitempty"`
	SubscriptionExpiresAt    *time.Time               `json:"subscription_expires_at,omitempty"`
	PermanentBalance         string                   `json:"permanent_balance"`
	TemporaryCreditAvailable string                   `json:"temporary_credit_available"`
}

type mallCurrencyProduct struct {
	id, dailyLimit, totalLimit int64
	price, creditedAmount      float64
	paymentType, creditedType  MallCreditType
}

type mallSubscriptionPlan struct {
	id, groupID            int64
	price, dailyAmount     float64
	validityDays           int
	dailyLimit, totalLimit int
	paymentType            MallCreditType
	benefitType            SubscriptionBenefitType
}

// MallService owns immediate internal-credit purchases. It intentionally uses
// database/sql so debit, delivery, limits, grants, and idempotency success are
// committed atomically.
type MallService struct {
	db                   *sql.DB
	temporaryCredit      *TemporaryCreditService
	subscription         *SubscriptionService
	authCacheInvalidator APIKeyAuthCacheInvalidator
}

func NewMallService(db *sql.DB, temporaryCredit *TemporaryCreditService, subscription *SubscriptionService) *MallService {
	return &MallService{db: db, temporaryCredit: temporaryCredit, subscription: subscription}
}

func (s *MallService) SetAuthCacheInvalidator(invalidator APIKeyAuthCacheInvalidator) {
	if s != nil {
		s.authCacheInvalidator = invalidator
	}
}

func (s *MallService) GetBalance(ctx context.Context, userID int64) (*MallBalanceSummary, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("mall service database is nil")
	}
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	return loadMallBalance(ctx, s.db, userID, nil)
}

func (s *MallService) PurchaseAtomic(ctx context.Context, userID int64, req MallPurchaseRequest, claim *IdempotencyAtomicClaim) (*MallPurchaseResult, error) {
	if s == nil || s.db == nil || s.temporaryCredit == nil {
		return nil, errors.New("mall service is not configured")
	}
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	if claim == nil {
		return nil, ErrIdempotencyStoreUnavail
	}
	if req.ProductID <= 0 || (req.ProductType != MallProductTypeCurrency && req.ProductType != MallProductTypeSubscription) {
		return nil, ErrMallProductNotAvailable
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin mall purchase transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now, balance, err := lockMallUser(ctx, tx, userID)
	if err != nil {
		return nil, err
	}

	result := &MallPurchaseResult{ProductType: req.ProductType, ProductID: req.ProductID}
	permanentChanged := false
	temporaryChanged := false
	var subscriptionGroupID int64

	switch req.ProductType {
	case MallProductTypeCurrency:
		product, err := loadMallCurrencyProduct(ctx, tx, req.ProductID)
		if err != nil {
			return nil, err
		}
		if err := consumeImmediatePurchaseCounters(ctx, tx, userID, &purchaseLimitSpec{
			productType: purchaseProductCurrency, productID: product.id,
			dailyLimit: int(product.dailyLimit), totalLimit: int(product.totalLimit),
		}, now); err != nil {
			return nil, err
		}
		purchaseID, err := insertMallCurrencyPurchase(ctx, tx, userID, claim.recordID, product)
		if err != nil {
			return nil, err
		}
		if err := s.debitMallCredit(ctx, tx, userID, product.paymentType, product.price, claim.recordID); err != nil {
			return nil, err
		}
		if product.paymentType == MallCreditTypePermanent {
			permanentChanged = true
		} else {
			temporaryChanged = true
		}
		if product.creditedType == MallCreditTypePermanent {
			if err := addMallPermanentCredit(ctx, tx, userID, product.creditedAmount); err != nil {
				return nil, err
			}
			permanentChanged = true
		} else {
			if _, err := s.temporaryCredit.CreateGrantTx(ctx, tx, CreateTemporaryCreditGrantInput{
				UserID: userID, Source: TemporaryCreditSourceMallProduct, Amount: product.creditedAmount,
				MallPurchaseID: &purchaseID, businessNow: &now,
			}); err != nil {
				return nil, fmt.Errorf("create mall product temporary credit: %w", err)
			}
			temporaryChanged = true
		}
		creditedType := product.creditedType
		creditedAmount := formatLedgerAmount(product.creditedAmount)
		result.PurchaseID = purchaseID
		result.PaymentCreditType = product.paymentType
		result.Price = formatLedgerAmount(product.price)
		result.CreditedType = &creditedType
		result.CreditedAmount = &creditedAmount

	case MallProductTypeSubscription:
		plan, err := loadMallSubscriptionPlan(ctx, tx, req.ProductID)
		if err != nil {
			return nil, err
		}
		if err := consumeImmediatePurchaseCounters(ctx, tx, userID, &purchaseLimitSpec{
			productType: purchaseProductSubscription, productID: plan.id,
			dailyLimit: plan.dailyLimit, totalLimit: plan.totalLimit,
		}, now); err != nil {
			return nil, err
		}
		purchaseID, err := insertMallSubscriptionPurchase(ctx, tx, userID, claim.recordID, plan)
		if err != nil {
			return nil, err
		}
		if err := s.debitMallCredit(ctx, tx, userID, plan.paymentType, plan.price, claim.recordID); err != nil {
			return nil, err
		}
		if plan.paymentType == MallCreditTypePermanent {
			permanentChanged = true
		} else {
			temporaryChanged = true
		}

		var expiresAt time.Time
		if plan.benefitType == SubscriptionBenefitSub2 {
			expiresAt, err = assignOrExtendMallSub2(ctx, tx, userID, plan.groupID, plan.validityDays, purchaseID, now)
			subscriptionGroupID = plan.groupID
		} else {
			expiresAt, err = s.assignOrExtendDailyTemporaryCredit(ctx, tx, userID, plan, purchaseID, now)
			temporaryChanged = true
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE mall_purchases SET subscription_expires_at = $1 WHERE id = $2`, expiresAt, purchaseID); err != nil {
			return nil, fmt.Errorf("update mall subscription purchase expiry: %w", err)
		}
		benefitType := plan.benefitType
		expiresUTC := expiresAt.UTC()
		result.PurchaseID = purchaseID
		result.PaymentCreditType = plan.paymentType
		result.Price = formatLedgerAmount(plan.price)
		result.BenefitType = &benefitType
		result.SubscriptionExpiresAt = &expiresUTC
	}

	summary, err := loadMallBalance(ctx, tx, userID, &now)
	if err != nil {
		return nil, err
	}
	result.PermanentBalance = summary.PermanentBalance
	result.TemporaryCreditAvailable = summary.TemporaryCreditAvailable
	if err := claim.PersistSuccess(ctx, tx, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit mall purchase transaction: %w", err)
	}
	s.invalidateMallBalances(ctx, userID, permanentChanged, temporaryChanged)
	if subscriptionGroupID > 0 && s.subscription != nil {
		_ = s.subscription.invalidateSubscriptionCaches(userID, subscriptionGroupID)
	}
	_ = balance
	return result, nil
}

func lockMallUser(ctx context.Context, tx *sql.Tx, userID int64) (time.Time, float64, error) {
	var now time.Time
	var balance float64
	if err := tx.QueryRowContext(ctx, `
SELECT clock_timestamp(), balance
FROM users
WHERE id = $1 AND deleted_at IS NULL
FOR UPDATE`, userID).Scan(&now, &balance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, 0, ErrUserNotFound
		}
		return time.Time{}, 0, fmt.Errorf("lock mall user: %w", err)
	}
	return now, balance, nil
}

func loadMallCurrencyProduct(ctx context.Context, tx *sql.Tx, productID int64) (*mallCurrencyProduct, error) {
	var product mallCurrencyProduct
	var priceRaw, creditedRaw, paymentTypeRaw, creditedTypeRaw string
	if err := tx.QueryRowContext(ctx, `
SELECT id, payment_price::text, payment_credit_type, credited_type, credited_amount::text,
       daily_purchase_limit, total_purchase_limit
FROM currency_products
WHERE id = $1 AND is_active = TRUE AND for_sale = TRUE
FOR SHARE`, productID).Scan(&product.id, &priceRaw, &paymentTypeRaw, &creditedTypeRaw, &creditedRaw, &product.dailyLimit, &product.totalLimit); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMallProductNotAvailable
		}
		return nil, fmt.Errorf("load mall currency product: %w", err)
	}
	var err error
	if product.price, err = parseMallLedgerAmount(priceRaw); err != nil {
		return nil, err
	}
	if product.creditedAmount, err = parseMallLedgerAmount(creditedRaw); err != nil {
		return nil, err
	}
	if product.paymentType, err = normalizeMallCreditType(paymentTypeRaw); err != nil {
		return nil, err
	}
	if product.creditedType, err = normalizeMallCreditType(creditedTypeRaw); err != nil {
		return nil, err
	}
	return &product, nil
}

func loadMallSubscriptionPlan(ctx context.Context, tx *sql.Tx, planID int64) (*mallSubscriptionPlan, error) {
	var plan mallSubscriptionPlan
	var priceRaw, dailyRaw, paymentTypeRaw, benefitTypeRaw, validityUnit string
	if err := tx.QueryRowContext(ctx, `
SELECT id, group_id, price::text, payment_credit_type, benefit_type,
       daily_temporary_credit_amount::text, validity_days, validity_unit,
       daily_purchase_limit, total_purchase_limit
FROM subscription_plans
WHERE id = $1 AND for_sale = TRUE
FOR SHARE`, planID).Scan(&plan.id, &plan.groupID, &priceRaw, &paymentTypeRaw, &benefitTypeRaw, &dailyRaw,
		&plan.validityDays, &validityUnit, &plan.dailyLimit, &plan.totalLimit); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMallProductNotAvailable
		}
		return nil, fmt.Errorf("load mall subscription plan: %w", err)
	}
	var err error
	if plan.price, err = parseMallLedgerAmount(priceRaw); err != nil {
		return nil, err
	}
	if plan.dailyAmount, err = parseMallNonNegativeLedgerAmount(dailyRaw); err != nil {
		return nil, err
	}
	if plan.paymentType, err = normalizeMallCreditType(paymentTypeRaw); err != nil {
		return nil, err
	}
	if plan.benefitType, err = normalizeSubscriptionBenefitType(benefitTypeRaw); err != nil {
		return nil, err
	}
	if plan.validityDays <= 0 || (plan.benefitType == SubscriptionBenefitDailyTemporaryCredit && (plan.groupID != 0 || validityUnit != "day" || plan.dailyAmount <= 0)) {
		return nil, ErrMallProductNotAvailable
	}
	if plan.benefitType == SubscriptionBenefitSub2 {
		plan.validityDays = normalizeAssignValidityDays(psComputeValidityDays(plan.validityDays, validityUnit))
	}
	return &plan, nil
}

func (s *MallService) debitMallCredit(ctx context.Context, tx *sql.Tx, userID int64, creditType MallCreditType, amount float64, claimID int64) error {
	switch creditType {
	case MallCreditTypePermanent:
		result, err := tx.ExecContext(ctx, `
UPDATE users
SET balance = balance - $1, updated_at = clock_timestamp()
WHERE id = $2 AND deleted_at IS NULL AND balance >= $1`, formatLedgerAmount(amount), userID)
		if err != nil {
			return fmt.Errorf("deduct mall permanent credit: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read mall permanent deduction: %w", err)
		}
		if affected != 1 {
			return ErrMallPermanentCreditInsufficient
		}
		return nil
	case MallCreditTypeTemporary:
		remaining, err := s.temporaryCredit.ConsumeFEFO(ctx, tx, userID, amount, TemporaryCreditConsumptionReference{
			RequestID: fmt.Sprintf("mall-purchase:%d", claimID),
		})
		if err != nil {
			return err
		}
		if remaining > ledgerAmountEpsilon {
			return ErrMallTemporaryCreditInsufficient
		}
		return nil
	default:
		return ErrMallProductNotAvailable
	}
}

func addMallPermanentCredit(ctx context.Context, tx *sql.Tx, userID int64, amount float64) error {
	result, err := tx.ExecContext(ctx, `
UPDATE users
SET balance = balance + $1, updated_at = clock_timestamp()
WHERE id = $2 AND deleted_at IS NULL`, formatLedgerAmount(amount), userID)
	if err != nil {
		return fmt.Errorf("add mall permanent credit: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("mall permanent credit user disappeared")
	}
	return nil
}

func insertMallCurrencyPurchase(ctx context.Context, tx *sql.Tx, userID, claimID int64, product *mallCurrencyProduct) (int64, error) {
	var purchaseID int64
	err := tx.QueryRowContext(ctx, `
INSERT INTO mall_purchases
    (user_id, product_type, product_id, idempotency_record_id, payment_credit_type, price,
     credited_type, credited_amount, status)
VALUES ($1, 'currency', $2, $3, $4, $5, $6, $7, 'completed')
RETURNING id`, userID, product.id, claimID, product.paymentType, formatLedgerAmount(product.price), product.creditedType, formatLedgerAmount(product.creditedAmount)).Scan(&purchaseID)
	if err != nil {
		return 0, fmt.Errorf("create mall currency purchase: %w", err)
	}
	return purchaseID, nil
}

func insertMallSubscriptionPurchase(ctx context.Context, tx *sql.Tx, userID, claimID int64, plan *mallSubscriptionPlan) (int64, error) {
	var purchaseID int64
	var dailyAmount any
	if plan.benefitType == SubscriptionBenefitDailyTemporaryCredit {
		dailyAmount = formatLedgerAmount(plan.dailyAmount)
	}
	err := tx.QueryRowContext(ctx, `
INSERT INTO mall_purchases
    (user_id, product_type, product_id, idempotency_record_id, payment_credit_type, price,
     benefit_type, benefit_days, daily_temporary_credit_amount, status)
VALUES ($1, 'subscription', $2, $3, $4, $5, $6, $7, $8, 'completed')
RETURNING id`, userID, plan.id, claimID, plan.paymentType, formatLedgerAmount(plan.price), plan.benefitType, plan.validityDays, dailyAmount).Scan(&purchaseID)
	if err != nil {
		return 0, fmt.Errorf("create mall subscription purchase: %w", err)
	}
	return purchaseID, nil
}

func assignOrExtendMallSub2(ctx context.Context, tx *sql.Tx, userID, groupID int64, days int, purchaseID int64, now time.Time) (time.Time, error) {
	var groupExists bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM groups
    WHERE id = $1 AND deleted_at IS NULL AND status = 'active' AND subscription_type = 'subscription'
)`, groupID).Scan(&groupExists); err != nil {
		return time.Time{}, fmt.Errorf("validate mall subscription group: %w", err)
	}
	if !groupExists {
		return time.Time{}, ErrMallSubscriptionGroupUnavailable
	}

	var subscriptionID int64
	var expiresAt time.Time
	var status string
	var notes sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT id, expires_at, status, notes
FROM user_subscriptions
WHERE user_id = $1 AND group_id = $2 AND deleted_at IS NULL
FOR UPDATE`, userID, groupID).Scan(&subscriptionID, &expiresAt, &status, &notes)
	orderNote := fmt.Sprintf("mall_purchase:%d", purchaseID)
	if errors.Is(err, sql.ErrNoRows) {
		expiresAt = now.AddDate(0, 0, days)
		if expiresAt.After(MaxExpiresAt) {
			expiresAt = MaxExpiresAt
		}
		if err := tx.QueryRowContext(ctx, `
INSERT INTO user_subscriptions
    (user_id, group_id, starts_at, expires_at, status, assigned_at, notes, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'active', $3, $5, $3, $3)
RETURNING id`, userID, groupID, now, expiresAt, orderNote).Scan(&subscriptionID); err != nil {
			return time.Time{}, fmt.Errorf("create mall sub2 subscription: %w", err)
		}
		return expiresAt, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("lock mall sub2 subscription: %w", err)
	}
	base := expiresAt
	expired := !expiresAt.After(now)
	if expired {
		base = now
	}
	expiresAt = base.AddDate(0, 0, days)
	if expiresAt.After(MaxExpiresAt) {
		expiresAt = MaxExpiresAt
	}
	updatedNotes := appendSubscriptionNotes(notes.String, orderNote)
	if expired {
		windowStart := startOfDay(now)
		_, err = tx.ExecContext(ctx, `
UPDATE user_subscriptions
SET starts_at = $1, expires_at = $2, status = 'active',
    daily_window_start = $3, weekly_window_start = $3, monthly_window_start = $3,
    daily_usage_usd = 0, weekly_usage_usd = 0, monthly_usage_usd = 0,
    notes = $4, updated_at = $1
WHERE id = $5`, now, expiresAt, windowStart, updatedNotes, subscriptionID)
	} else {
		_, err = tx.ExecContext(ctx, `
UPDATE user_subscriptions
SET expires_at = $1, status = 'active', notes = $2, updated_at = $3
WHERE id = $4`, expiresAt, updatedNotes, now, subscriptionID)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("extend mall sub2 subscription: %w", err)
	}
	return expiresAt, nil
}

func (s *MallService) assignOrExtendDailyTemporaryCredit(ctx context.Context, tx *sql.Tx, userID int64, plan *mallSubscriptionPlan, purchaseID int64, now time.Time) (time.Time, error) {
	location := beijingLocation
	localNow := now.In(location)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	startDate := today
	startsAt := now
	activeRenewal := false
	var subscriptionID int64
	var existingStartsAt, existingExpiresAt, lastDate time.Time
	err := tx.QueryRowContext(ctx, `
SELECT id, starts_at, last_grant_date, expires_at
FROM mall_daily_credit_subscriptions
WHERE user_id = $1 AND plan_id = $2
FOR UPDATE`, userID, plan.id).Scan(&subscriptionID, &existingStartsAt, &lastDate, &existingExpiresAt)
	if err == nil && existingExpiresAt.After(now) {
		activeRenewal = true
		startsAt = existingStartsAt
		lastLocal := time.Date(lastDate.Year(), lastDate.Month(), lastDate.Day(), 0, 0, 0, 0, location)
		startDate = lastLocal.AddDate(0, 0, 1)
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, fmt.Errorf("lock daily temporary credit subscription: %w", err)
	}
	lastGrantDate := startDate.AddDate(0, 0, plan.validityDays-1)
	expiresAt := lastGrantDate.AddDate(0, 0, 1)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.QueryRowContext(ctx, `
INSERT INTO mall_daily_credit_subscriptions
    (user_id, plan_id, starts_at, last_grant_date, expires_at, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, 'active', $3, $3)
RETURNING id`, userID, plan.id, startsAt, lastGrantDate.Format("2006-01-02"), expiresAt).Scan(&subscriptionID); err != nil {
			return time.Time{}, fmt.Errorf("create daily temporary credit subscription: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
UPDATE mall_daily_credit_subscriptions
SET starts_at = $1, last_grant_date = $2, expires_at = $3, status = 'active', updated_at = $4
WHERE id = $5`, startsAt, lastGrantDate.Format("2006-01-02"), expiresAt, now, subscriptionID); err != nil {
			return time.Time{}, fmt.Errorf("extend daily temporary credit subscription: %w", err)
		}
	}

	for day := 0; day < plan.validityDays; day++ {
		scheduledDate := startDate.AddDate(0, 0, day)
		availableAt := scheduledDate
		if !activeRenewal && day == 0 {
			availableAt = now
		}
		expires := scheduledDate.AddDate(0, 0, 1)
		if _, err := s.temporaryCredit.CreateGrantTx(ctx, tx, CreateTemporaryCreditGrantInput{
			UserID: userID, Source: TemporaryCreditSourceSubscription, Amount: plan.dailyAmount,
			MallPurchaseID: &purchaseID, DailySubscriptionID: &subscriptionID, ScheduledDate: &scheduledDate,
			businessNow: &now, availableAt: &availableAt, expiresAt: &expires,
		}); err != nil {
			return time.Time{}, fmt.Errorf("create scheduled temporary credit for %s: %w", scheduledDate.Format("2006-01-02"), err)
		}
	}
	return expiresAt, nil
}

type mallBalanceQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadMallBalance(ctx context.Context, q mallBalanceQueryer, userID int64, at *time.Time) (*MallBalanceSummary, error) {
	var permanentRaw, temporaryRaw string
	query := `
SELECT balance::text,
       COALESCE((
           SELECT SUM(remaining_amount)
           FROM temporary_credit_grants
           WHERE user_id = users.id AND remaining_amount > 0
             AND available_at <= clock_timestamp() AND expires_at > clock_timestamp()
       ), 0)::text
FROM users
WHERE id = $1 AND deleted_at IS NULL`
	args := []any{userID}
	if at != nil {
		query = `
SELECT balance::text,
       COALESCE((
           SELECT SUM(remaining_amount)
           FROM temporary_credit_grants
           WHERE user_id = users.id AND remaining_amount > 0
             AND available_at <= $2 AND expires_at > $2
       ), 0)::text
FROM users
WHERE id = $1 AND deleted_at IS NULL`
		args = append(args, *at)
	}
	if err := q.QueryRowContext(ctx, query, args...).Scan(&permanentRaw, &temporaryRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("load mall balance: %w", err)
	}
	permanent, err := parseMallStoredLedgerAmount(permanentRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid mall permanent balance: %w", err)
	}
	temporary, err := parseMallNonNegativeLedgerAmount(temporaryRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid mall temporary balance: %w", err)
	}
	return &MallBalanceSummary{
		PermanentBalance: formatLedgerAmount(permanent), TemporaryCreditAvailable: formatLedgerAmount(temporary),
	}, nil
}

func parseMallLedgerAmount(raw string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("parse mall amount: %w", err)
	}
	value, err = normalizeLedgerAmount(value)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("mall amount must be positive")
	}
	return value, nil
}

func parseMallNonNegativeLedgerAmount(raw string) (float64, error) {
	value, err := parseMallStoredLedgerAmount(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("mall amount must be non-negative")
	}
	return value, nil
}

func parseMallStoredLedgerAmount(raw string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, err
	}
	value, err = normalizeDerivedLedgerAmount(value)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func (s *MallService) invalidateMallBalances(ctx context.Context, userID int64, permanentChanged, temporaryChanged bool) {
	if s == nil {
		return
	}
	if permanentChanged && s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.temporaryCredit == nil {
		return
	}
	if permanentChanged {
		if invalidator, ok := s.temporaryCredit.availableCreditInvalidator.(interface {
			InvalidateUserBalance(context.Context, int64) error
		}); ok {
			_ = invalidator.InvalidateUserBalance(ctx, userID)
			return
		}
	}
	if temporaryChanged {
		s.temporaryCredit.invalidateAvailableCredit(ctx, userID)
	}
}
