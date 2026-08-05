package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	entdialect "entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	purchaseProductCurrency     = "currency"
	purchaseProductSubscription = "subscription"
	purchasePeriodDaily         = "daily"
	purchasePeriodWeekly        = "weekly"
	purchasePeriodMonthly       = "monthly"
	purchasePeriodRolling       = "rolling"
	purchasePeriodTotal         = "total"
	purchaseReservationReserved = "reserved"
	purchaseReservationConsumed = "consumed"
	purchaseReservationReleased = "released"
	purchaseLimitUnitDay        = "day"
	purchaseLimitUnitWeek       = "week"
	purchaseLimitUnitMonth      = "month"
	purchaseLimitModeCalendar   = "calendar"
	purchaseLimitModeRolling    = "rolling"
	purchaseEventSourceOrder    = "payment_order"
	purchaseEventSourceMall     = "mall_purchase"
	maxPurchaseLimit            = int(^uint32(0) >> 1)
)

var totalPurchasePeriodStart = time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC)

var errPurchaseReservationUnavailable = errors.New("purchase reservation unavailable")

// errPaymentAfterExpiryGrace distinguishes a product payment that arrived
// after the persisted expiry grace window. The caller must refund it without
// fulfilling the product.
var errPaymentAfterExpiryGrace = errors.New("payment arrived after expiry grace period")

type purchaseLimitSpec struct {
	productType string
	productID   int64
	dailyLimit  int
	totalLimit  int
	unit        string
	mode        string
	windowSize  int
	sourceType  string
	sourceID    int64
}

type purchaseReservationRecord struct {
	orderID          int64
	userID           int64
	productType      string
	productID        int64
	dailyPeriodStart time.Time
	periodType       string
}

type purchaseCounterKey struct {
	productType string
	productID   int64
	periodType  string
}

// ProductPurchaseLimitStatus is returned beside checkout products for one user.
type ProductPurchaseLimitStatus struct {
	DailyLimit              int    `json:"daily_purchase_limit"`
	DailyRemaining          int    `json:"daily_purchase_remaining"`
	TotalLimit              int    `json:"total_purchase_limit"`
	TotalRemaining          int    `json:"total_purchase_remaining"`
	PurchaseLimitUnit       string `json:"purchase_limit_unit"`
	PurchaseLimitMode       string `json:"purchase_limit_mode"`
	PurchaseLimitWindowSize int    `json:"purchase_limit_window_size"`
}

type ProductPurchaseLimitUsage struct {
	DailyUsed     int
	WeeklyUsed    int
	MonthlyUsed   int
	TotalUsed     int
	RollingEvents []time.Time
}

type paymentPurchaseSQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func invalidPurchaseLimitError(field string) error {
	return infraerrors.BadRequest("INVALID_PURCHASE_LIMIT", "purchase limits must be non-negative 32-bit integers").
		WithMetadata(map[string]string{"field": field})
}

func invalidPurchaseLimitPolicyError(field string) error {
	return infraerrors.BadRequest("INVALID_PURCHASE_LIMIT_POLICY", "purchase limit policy is invalid").
		WithMetadata(map[string]string{"field": field})
}

func normalizePurchaseLimitPolicy(unit, mode string, windowSize int) (string, string, int, error) {
	if unit == "" {
		unit = purchaseLimitUnitDay
	}
	if mode == "" {
		mode = purchaseLimitModeCalendar
	}
	if windowSize == 0 {
		windowSize = 1
	}
	if unit != purchaseLimitUnitDay && unit != purchaseLimitUnitWeek && unit != purchaseLimitUnitMonth {
		return "", "", 0, invalidPurchaseLimitPolicyError("purchase_limit_unit")
	}
	if mode != purchaseLimitModeCalendar && mode != purchaseLimitModeRolling {
		return "", "", 0, invalidPurchaseLimitPolicyError("purchase_limit_mode")
	}
	if windowSize <= 0 || windowSize > maxPurchaseLimit {
		return "", "", 0, invalidPurchaseLimitPolicyError("purchase_limit_window_size")
	}
	if mode == purchaseLimitModeCalendar && windowSize != 1 {
		return "", "", 0, invalidPurchaseLimitPolicyError("purchase_limit_window_size")
	}
	return unit, mode, windowSize, nil
}

func validatePurchaseLimitPolicy(unit, mode string, windowSize int) error {
	_, _, _, err := normalizePurchaseLimitPolicy(unit, mode, windowSize)
	return err
}

func validatePurchaseLimits(daily, total int) error {
	if daily < 0 || daily > maxPurchaseLimit {
		return invalidPurchaseLimitError("daily_purchase_limit")
	}
	if total < 0 || total > maxPurchaseLimit {
		return invalidPurchaseLimitError("total_purchase_limit")
	}
	return nil
}

func validatePurchaseLimitPatch(daily, total *int) error {
	d, t := 0, 0
	if daily != nil {
		d = *daily
	}
	if total != nil {
		t = *total
	}
	return validatePurchaseLimits(d, t)
}

func purchaseLimitSpecFor(plan *dbent.SubscriptionPlan, product *dbent.CurrencyProduct) *purchaseLimitSpec {
	if product != nil {
		unit, mode, size, err := normalizePurchaseLimitPolicy(product.PurchaseLimitUnit, product.PurchaseLimitMode, product.PurchaseLimitWindowSize)
		if err != nil {
			unit, mode, size = purchaseLimitUnitDay, purchaseLimitModeCalendar, 1
		}
		return &purchaseLimitSpec{
			productType: purchaseProductCurrency,
			productID:   product.ID,
			dailyLimit:  product.DailyPurchaseLimit,
			totalLimit:  product.TotalPurchaseLimit,
			unit:        unit,
			mode:        mode,
			windowSize:  size,
		}
	}
	if plan != nil {
		unit, mode, size, err := normalizePurchaseLimitPolicy(plan.PurchaseLimitUnit, plan.PurchaseLimitMode, plan.PurchaseLimitWindowSize)
		if err != nil {
			unit, mode, size = purchaseLimitUnitDay, purchaseLimitModeCalendar, 1
		}
		return &purchaseLimitSpec{
			productType: purchaseProductSubscription,
			productID:   plan.ID,
			dailyLimit:  plan.DailyPurchaseLimit,
			totalLimit:  plan.TotalPurchaseLimit,
			unit:        unit,
			mode:        mode,
			windowSize:  size,
		}
	}
	return nil
}

func normalizePurchaseLimitSpec(spec *purchaseLimitSpec) error {
	if spec == nil {
		return nil
	}
	unit, mode, size, err := normalizePurchaseLimitPolicy(spec.unit, spec.mode, spec.windowSize)
	if err != nil {
		return err
	}
	if err := validatePurchaseLimits(spec.dailyLimit, spec.totalLimit); err != nil {
		return err
	}
	spec.unit, spec.mode, spec.windowSize = unit, mode, size
	return nil
}

func purchaseDailyPeriodStart(now time.Time) time.Time {
	return purchaseCalendarPeriodStart(now, purchaseLimitUnitDay)
}

func purchaseCalendarPeriodStart(now time.Time, unit string) time.Time {
	local := now.In(beijingLocation)
	switch unit {
	case purchaseLimitUnitWeek:
		weekdayOffset := (int(local.Weekday()) + 6) % 7
		local = local.AddDate(0, 0, -weekdayOffset)
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	case purchaseLimitUnitMonth:
		return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	}
}

func purchaseCalendarPeriodType(unit string) string {
	switch unit {
	case purchaseLimitUnitWeek:
		return purchasePeriodWeekly
	case purchaseLimitUnitMonth:
		return purchasePeriodMonthly
	default:
		return purchasePeriodDaily
	}
}

func purchasePeriodTypeForSpec(spec *purchaseLimitSpec) string {
	if spec != nil && spec.mode == purchaseLimitModeRolling && spec.dailyLimit > 0 {
		return purchasePeriodRolling
	}
	if spec == nil {
		return purchasePeriodDaily
	}
	return purchaseCalendarPeriodType(spec.unit)
}

func rollingPurchaseWindowStart(now time.Time, unit string, size int) time.Time {
	switch unit {
	case purchaseLimitUnitWeek:
		return now.AddDate(0, 0, -7*size)
	case purchaseLimitUnitMonth:
		return now.AddDate(0, -size, 0)
	default:
		return now.AddDate(0, 0, -size)
	}
}

func (s *PaymentService) reservePurchaseTx(ctx context.Context, tx *dbent.Tx, orderID, userID int64, spec *purchaseLimitSpec, now time.Time) error {
	if spec == nil {
		return nil
	}
	if err := normalizePurchaseLimitSpec(spec); err != nil {
		return err
	}
	periodType := purchasePeriodTypeForSpec(spec)
	periodStart := purchaseCalendarPeriodStart(now, spec.unit)
	if periodType == purchasePeriodRolling {
		if err := reserveRollingPurchaseEvent(ctx, tx, userID, spec, orderID, now); err != nil {
			return err
		}
	} else if err := reservePurchaseCounter(ctx, tx, userID, spec.productType, spec.productID, periodType, periodStart, spec.dailyLimit); err != nil {
		return err
	}
	if err := reservePurchaseCounter(ctx, tx, userID, spec.productType, spec.productID, purchasePeriodTotal, totalPurchasePeriodStart, spec.totalLimit); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO payment_purchase_reservations
    (order_id, user_id, product_type, product_id, daily_period_start, period_type, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'reserved', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		orderID, userID, spec.productType, spec.productID, periodStart, periodType); err != nil {
		return fmt.Errorf("create purchase reservation: %w", err)
	}
	return nil
}

func reserveRollingPurchaseEvent(ctx context.Context, db paymentPurchaseSQL, userID int64, spec *purchaseLimitSpec, sourceID int64, now time.Time) error {
	if spec.dailyLimit <= 0 {
		return nil
	}
	if sourceID <= 0 {
		return fmt.Errorf("rolling purchase event source id is required")
	}
	if spec.sourceType == "" {
		spec.sourceType = purchaseEventSourceOrder
	}
	if err := lockRollingPurchaseScope(ctx, db, userID, spec.productType, spec.productID); err != nil {
		return err
	}
	windowStart := rollingPurchaseWindowStart(now, spec.unit, spec.windowSize)
	var used int
	rows, err := db.QueryContext(ctx, `
SELECT COUNT(*)
FROM payment_purchase_limit_events
WHERE user_id = $1 AND product_type = $2 AND product_id = $3
  AND status IN ('reserved', 'consumed')
  AND occurred_at >= $4 AND occurred_at < $5`, userID, spec.productType, spec.productID, windowStart, now)
	if err != nil {
		return fmt.Errorf("count rolling purchase events: %w", err)
	}
	if !rows.Next() {
		_ = rows.Close()
		return fmt.Errorf("count rolling purchase events returned no row")
	}
	if err := rows.Scan(&used); err != nil {
		_ = rows.Close()
		return fmt.Errorf("scan rolling purchase events: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close rolling purchase event count: %w", err)
	}
	if used >= spec.dailyLimit {
		return purchaseLimitExceededErrorForPeriod(spec.productType, spec.productID, spec.dailyLimit, purchasePeriodRolling)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO payment_purchase_limit_events
    (user_id, product_type, product_id, source_type, source_id, period_type, status, occurred_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, 'rolling', 'reserved', $6, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		userID, spec.productType, spec.productID, spec.sourceType, sourceID, now); err != nil {
		return fmt.Errorf("create rolling purchase event: %w", err)
	}
	return nil
}

func lockRollingPurchaseScope(ctx context.Context, db paymentPurchaseSQL, userID int64, productType string, productID int64) error {
	if _, err := db.ExecContext(ctx, `
INSERT INTO payment_purchase_counters
    (user_id, product_type, product_id, period_type, period_start, reserved_count, consumed_count, created_at, updated_at)
VALUES ($1, $2, $3, 'rolling', $4, 0, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (user_id, product_type, product_id, period_type, period_start) DO NOTHING`,
		userID, productType, productID, totalPurchasePeriodStart); err != nil {
		return fmt.Errorf("ensure rolling purchase scope: %w", err)
	}
	rows, err := db.QueryContext(ctx, `
UPDATE payment_purchase_counters
SET updated_at = CURRENT_TIMESTAMP
WHERE user_id = $1 AND product_type = $2 AND product_id = $3
  AND period_type = 'rolling' AND period_start = $4
RETURNING id`, userID, productType, productID, totalPurchasePeriodStart)
	if err != nil {
		return fmt.Errorf("lock rolling purchase scope: %w", err)
	}
	if !rows.Next() {
		_ = rows.Close()
		return fmt.Errorf("rolling purchase scope disappeared")
	}
	var id int64
	if err := rows.Scan(&id); err != nil {
		_ = rows.Close()
		return fmt.Errorf("scan rolling purchase scope: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close rolling purchase scope: %w", err)
	}
	return nil
}

func purchaseLimitExceededError(productType string, productID int64, limit int) error {
	return purchaseLimitExceededErrorForPeriod(productType, productID, limit, purchasePeriodDaily)
}

func purchaseLimitExceededErrorForPeriod(productType string, productID int64, limit int, periodType string) error {
	reason := "PERIODIC_PURCHASE_LIMIT_EXCEEDED"
	if periodType == purchasePeriodDaily {
		reason = "DAILY_PURCHASE_LIMIT_EXCEEDED"
	}
	return infraerrors.TooManyRequests(reason, "purchase limit exceeded").WithMetadata(map[string]string{
		"product_type": productType,
		"product_id":   strconv.FormatInt(productID, 10),
		"limit":        strconv.Itoa(limit),
		"remaining":    "0",
		"period_type":  periodType,
	})
}

func reservePurchaseCounter(ctx context.Context, db paymentPurchaseSQL, userID int64, productType string, productID int64, periodType string, periodStart time.Time, limit int) error {
	if _, err := db.ExecContext(ctx, `
INSERT INTO payment_purchase_counters
    (user_id, product_type, product_id, period_type, period_start, reserved_count, consumed_count, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, 0, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (user_id, product_type, product_id, period_type, period_start) DO NOTHING`,
		userID, productType, productID, periodType, periodStart); err != nil {
		return fmt.Errorf("ensure purchase counter: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
UPDATE payment_purchase_counters
SET reserved_count = reserved_count + 1, updated_at = CURRENT_TIMESTAMP
WHERE user_id = $1
  AND product_type = $2
  AND product_id = $3
  AND period_type = $4
  AND period_start = $5
  AND ($6 = 0 OR reserved_count + consumed_count < $6)
RETURNING reserved_count, consumed_count`,
		userID, productType, productID, periodType, periodStart, limit)
	if err != nil {
		return fmt.Errorf("reserve purchase counter: %w", err)
	}
	closeRows := func() error {
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close reserved purchase counter rows: %w", err)
		}
		return nil
	}
	if rows.Next() {
		var reserved, consumed int
		if err := rows.Scan(&reserved, &consumed); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan reserved purchase counter: %w", err)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("reserve purchase counter: %w", err)
		}
		return closeRows()
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("reserve purchase counter: %w", err)
	}
	if err := closeRows(); err != nil {
		return err
	}
	if periodType == purchasePeriodTotal {
		return infraerrors.TooManyRequests("TOTAL_PURCHASE_LIMIT_EXCEEDED", "purchase limit exceeded").WithMetadata(map[string]string{
			"product_type": productType,
			"product_id":   strconv.FormatInt(productID, 10),
			"limit":        strconv.Itoa(limit),
			"remaining":    "0",
		})
	}
	return purchaseLimitExceededErrorForPeriod(productType, productID, limit, periodType)
}

// consumeImmediatePurchaseCounters increments the same counters used by
// external pending-order reservations, but directly as consumed because an
// internal mall purchase settles in one transaction.
func consumeImmediatePurchaseCounters(ctx context.Context, db paymentPurchaseSQL, userID int64, spec *purchaseLimitSpec, now time.Time) error {
	if spec == nil {
		return nil
	}
	if err := normalizePurchaseLimitSpec(spec); err != nil {
		return err
	}
	periodType := purchasePeriodTypeForSpec(spec)
	periodStart := purchaseCalendarPeriodStart(now, spec.unit)
	if periodType == purchasePeriodRolling {
		if err := reserveRollingPurchaseEvent(ctx, db, userID, spec, spec.sourceID, now); err != nil {
			return err
		}
		result, err := db.ExecContext(ctx, `UPDATE payment_purchase_limit_events SET status = 'consumed', updated_at = CURRENT_TIMESTAMP WHERE source_type = $1 AND source_id = $2 AND status = 'reserved'`, spec.sourceType, spec.sourceID)
		if err != nil {
			return fmt.Errorf("consume rolling purchase event: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read consumed rolling purchase event rows: %w", err)
		}
		if affected != 1 {
			return fmt.Errorf("rolling purchase event invariant failed for %s:%d", spec.sourceType, spec.sourceID)
		}
	} else if err := consumeImmediatePurchaseCounter(ctx, db, userID, spec.productType, spec.productID, periodType, periodStart, spec.dailyLimit); err != nil {
		return err
	}
	return consumeImmediatePurchaseCounter(ctx, db, userID, spec.productType, spec.productID, purchasePeriodTotal, totalPurchasePeriodStart, spec.totalLimit)
}

func consumeImmediatePurchaseCounter(ctx context.Context, db paymentPurchaseSQL, userID int64, productType string, productID int64, periodType string, periodStart time.Time, limit int) error {
	if _, err := db.ExecContext(ctx, `
INSERT INTO payment_purchase_counters
    (user_id, product_type, product_id, period_type, period_start, reserved_count, consumed_count, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, 0, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (user_id, product_type, product_id, period_type, period_start) DO NOTHING`,
		userID, productType, productID, periodType, periodStart); err != nil {
		return fmt.Errorf("ensure immediate purchase counter: %w", err)
	}
	rows, err := db.QueryContext(ctx, `
UPDATE payment_purchase_counters
SET consumed_count = consumed_count + 1, updated_at = CURRENT_TIMESTAMP
WHERE user_id = $1
  AND product_type = $2
  AND product_id = $3
  AND period_type = $4
  AND period_start = $5
  AND ($6 = 0 OR reserved_count + consumed_count < $6)
RETURNING consumed_count`, userID, productType, productID, periodType, periodStart, limit)
	if err != nil {
		return fmt.Errorf("consume immediate purchase counter: %w", err)
	}
	closeRows := func() error {
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close immediate purchase counter rows: %w", err)
		}
		return nil
	}
	if rows.Next() {
		var consumed int
		if err := rows.Scan(&consumed); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan immediate purchase counter: %w", err)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("consume immediate purchase counter: %w", err)
		}
		return closeRows()
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("consume immediate purchase counter: %w", err)
	}
	if err := closeRows(); err != nil {
		return err
	}
	if periodType == purchasePeriodTotal {
		return infraerrors.TooManyRequests("TOTAL_PURCHASE_LIMIT_EXCEEDED", "purchase limit exceeded").WithMetadata(map[string]string{
			"product_type": productType,
			"product_id":   strconv.FormatInt(productID, 10),
			"limit":        strconv.Itoa(limit),
			"remaining":    "0",
		})
	}
	return purchaseLimitExceededErrorForPeriod(productType, productID, limit, periodType)
}

func mutatePurchaseReservation(ctx context.Context, db paymentPurchaseSQL, orderID int64, fromStatus, toStatus string, dailyPeriodStart *time.Time, periodType *string) (*purchaseReservationRecord, error) {
	rows, err := db.QueryContext(ctx, `
UPDATE payment_purchase_reservations
SET status = $2,
    daily_period_start = COALESCE($4, daily_period_start),
    period_type = COALESCE($5, period_type),
    updated_at = CURRENT_TIMESTAMP
WHERE order_id = $1 AND status = $3
RETURNING order_id, user_id, product_type, product_id, daily_period_start, period_type`,
		orderID, toStatus, fromStatus, dailyPeriodStart, periodType)
	if err != nil {
		return nil, fmt.Errorf("transition purchase reservation: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("transition purchase reservation: %w", err)
		}
		return nil, nil
	}
	record := &purchaseReservationRecord{}
	if err := rows.Scan(&record.orderID, &record.userID, &record.productType, &record.productID, &record.dailyPeriodStart, &record.periodType); err != nil {
		return nil, fmt.Errorf("scan purchase reservation transition: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("transition purchase reservation: %w", err)
	}
	return record, nil
}

func updatePurchaseCounter(ctx context.Context, db paymentPurchaseSQL, record *purchaseReservationRecord, periodType string, reservedDelta, consumedDelta int) error {
	periodStart := totalPurchasePeriodStart
	if periodType != purchasePeriodTotal {
		periodStart = record.dailyPeriodStart
	}
	result, err := db.ExecContext(ctx, `
UPDATE payment_purchase_counters
SET reserved_count = reserved_count + $6,
    consumed_count = consumed_count + $7,
    updated_at = CURRENT_TIMESTAMP
WHERE user_id = $1
  AND product_type = $2
  AND product_id = $3
  AND period_type = $4
  AND period_start = $5
  AND reserved_count + $6 >= 0
  AND consumed_count + $7 >= 0`,
		record.userID, record.productType, record.productID, periodType, periodStart, reservedDelta, consumedDelta)
	if err != nil {
		return fmt.Errorf("update purchase counter: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read purchase counter update: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("purchase counter invariant failed for order product %s:%d", record.productType, record.productID)
	}
	return nil
}

func transitionPurchaseEvent(ctx context.Context, db paymentPurchaseSQL, orderID int64, fromStatus, toStatus string) error {
	result, err := db.ExecContext(ctx, `
UPDATE payment_purchase_limit_events
SET status = $2, updated_at = CURRENT_TIMESTAMP
WHERE source_type = $3 AND source_id = $1 AND status = $4`, orderID, toStatus, purchaseEventSourceOrder, fromStatus)
	if err != nil {
		return fmt.Errorf("transition purchase limit event: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read purchase limit event transition: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("purchase limit event invariant failed for order %d", orderID)
	}
	return nil
}

func transitionPurchaseCounters(ctx context.Context, db paymentPurchaseSQL, record *purchaseReservationRecord, reservedDelta, consumedDelta int) error {
	if record == nil {
		return nil
	}
	if record.periodType == purchasePeriodRolling {
		if err := transitionPurchaseEvent(ctx, db, record.orderID, func() string {
			if reservedDelta == -1 && consumedDelta == 1 {
				return purchaseReservationReserved
			}
			if reservedDelta == -1 && consumedDelta == 0 {
				return purchaseReservationReserved
			}
			return purchaseReservationConsumed
		}(), func() string {
			if reservedDelta == -1 && consumedDelta == 1 {
				return purchaseReservationConsumed
			}
			return purchaseReservationReleased
		}()); err != nil {
			return err
		}
	} else if err := updatePurchaseCounter(ctx, db, record, record.periodType, reservedDelta, consumedDelta); err != nil {
		return err
	}
	return updatePurchaseCounter(ctx, db, record, purchasePeriodTotal, reservedDelta, consumedDelta)
}

func consumePurchaseReservationTx(ctx context.Context, tx *dbent.Tx, orderID int64) error {
	record, err := mutatePurchaseReservation(ctx, tx, orderID, purchaseReservationReserved, purchaseReservationConsumed, nil, nil)
	if err != nil || record == nil {
		return err
	}
	return transitionPurchaseCounters(ctx, tx, record, -1, 1)
}

// consumePurchaseReservationRequiredTx is used by a paid transition. A
// missing reservation is an invariant violation, not an idempotent no-op.
func consumePurchaseReservationRequiredTx(ctx context.Context, tx *dbent.Tx, orderID int64) error {
	record, err := mutatePurchaseReservation(ctx, tx, orderID, purchaseReservationReserved, purchaseReservationConsumed, nil, nil)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("%w for order %d", errPurchaseReservationUnavailable, orderID)
	}
	return transitionPurchaseCounters(ctx, tx, record, -1, 1)
}

func releaseReservedPurchaseTx(ctx context.Context, tx *dbent.Tx, orderID int64) error {
	record, err := mutatePurchaseReservation(ctx, tx, orderID, purchaseReservationReserved, purchaseReservationReleased, nil, nil)
	if err != nil || record == nil {
		return err
	}
	return transitionPurchaseCounters(ctx, tx, record, -1, 0)
}

func releaseConsumedPurchaseTx(ctx context.Context, tx *dbent.Tx, orderID int64) error {
	record, err := mutatePurchaseReservation(ctx, tx, orderID, purchaseReservationConsumed, purchaseReservationReleased, nil, nil)
	if err != nil || record == nil {
		return err
	}
	return transitionPurchaseCounters(ctx, tx, record, 0, -1)
}

func (s *PaymentService) failPendingOrderAndReleasePurchase(ctx context.Context, orderID int64) error {
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin failed-order purchase release: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	updated, err := tx.PaymentOrder.Update().
		Where(paymentorder.IDEQ(orderID), paymentorder.StatusEQ(OrderStatusPending)).
		SetStatus(OrderStatusFailed).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("mark provider order failed: %w", err)
	}
	if updated > 0 {
		if err := releaseReservedPurchaseTx(ctx, tx, orderID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed-order purchase release: %w", err)
	}
	return nil
}

func reacquireAndConsumePurchaseTx(ctx context.Context, tx *dbent.Tx, order *dbent.PaymentOrder, now time.Time) (bool, error) {
	if order == nil {
		return false, nil
	}
	spec := &purchaseLimitSpec{
		productType: func() string {
			if order.CurrencyProductID != nil {
				return purchaseProductCurrency
			}
			return purchaseProductSubscription
		}(),
		productID: func() int64 {
			if order.CurrencyProductID != nil {
				return *order.CurrencyProductID
			}
			if order.PlanID != nil {
				return *order.PlanID
			}
			return 0
		}(),
		dailyLimit: order.DailyPurchaseLimitSnapshot,
		totalLimit: order.TotalPurchaseLimitSnapshot,
		unit:       order.PurchaseLimitUnitSnapshot,
		mode:       order.PurchaseLimitModeSnapshot,
		windowSize: order.PurchaseLimitWindowSizeSnapshot,
		sourceType: purchaseEventSourceOrder,
		sourceID:   order.ID,
	}
	if spec.productID <= 0 {
		return false, nil
	}
	if err := normalizePurchaseLimitSpec(spec); err != nil {
		return false, err
	}
	periodStart := purchaseCalendarPeriodStart(now, spec.unit)
	periodType := purchasePeriodTypeForSpec(spec)
	record, err := mutatePurchaseReservation(ctx, tx, order.ID, purchaseReservationReleased, purchaseReservationReserved, &periodStart, &periodType)
	if err != nil || record == nil {
		return false, err
	}
	if periodType == purchasePeriodRolling {
		if err := lockRollingPurchaseScope(ctx, tx, record.userID, record.productType, record.productID); err != nil {
			return false, err
		}
		windowStart := rollingPurchaseWindowStart(now, spec.unit, spec.windowSize)
		var used int
		rows, err := tx.QueryContext(ctx, `SELECT COUNT(*) FROM payment_purchase_limit_events WHERE user_id = $1 AND product_type = $2 AND product_id = $3 AND status IN ('reserved', 'consumed') AND occurred_at >= $4 AND occurred_at < $5`, record.userID, record.productType, record.productID, windowStart, now)
		if err != nil {
			return false, fmt.Errorf("count rolling purchase events: %w", err)
		}
		if !rows.Next() {
			_ = rows.Close()
			return false, fmt.Errorf("count rolling purchase events returned no row")
		}
		if err := rows.Scan(&used); err != nil {
			_ = rows.Close()
			return false, fmt.Errorf("scan rolling purchase events: %w", err)
		}
		if err := rows.Close(); err != nil {
			return false, fmt.Errorf("close rolling purchase event count: %w", err)
		}
		if used >= spec.dailyLimit {
			return false, purchaseLimitExceededErrorForPeriod(record.productType, record.productID, spec.dailyLimit, purchasePeriodRolling)
		}
		result, err := tx.ExecContext(ctx, `UPDATE payment_purchase_limit_events SET status = 'reserved', occurred_at = $1, updated_at = CURRENT_TIMESTAMP WHERE source_type = $2 AND source_id = $3 AND status = 'released'`, now, purchaseEventSourceOrder, order.ID)
		if err != nil {
			return false, fmt.Errorf("reacquire rolling purchase event: %w", err)
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return false, fmt.Errorf("rolling purchase event unavailable for order %d", order.ID)
		}
	} else if err := reservePurchaseCounter(ctx, tx, record.userID, record.productType, record.productID, periodType, periodStart, spec.dailyLimit); err != nil {
		return false, err
	}
	if err := reservePurchaseCounter(ctx, tx, record.userID, record.productType, record.productID, purchasePeriodTotal, totalPurchasePeriodStart, spec.totalLimit); err != nil {
		return false, err
	}
	if err := consumePurchaseReservationTx(ctx, tx, order.ID); err != nil {
		return false, err
	}
	return true, nil
}

func orderHasProductPurchase(order *dbent.PaymentOrder) bool {
	if order == nil {
		return false
	}
	return order.CurrencyProductID != nil || (order.OrderType == payment.OrderTypeSubscription && order.PlanID != nil)
}

// transitionOrderToPaidWithPurchase locks and re-reads the order before
// touching its purchase reservation. All order lifecycle paths acquire the
// order row first, then reservation/counter rows, which prevents the
// cancellation and callback paths from deadlocking each other.
func (s *PaymentService) transitionOrderToPaidWithPurchase(ctx context.Context, order *dbent.PaymentOrder, tradeNo string, paid float64, now, grace time.Time) (bool, string, error) {
	if order == nil {
		return false, "", fmt.Errorf("nil payment order")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return false, "", fmt.Errorf("begin paid purchase transition: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The callback may have loaded a stale PENDING snapshot while cancellation
	// or provider setup was committing. Lock the current row so the decision and
	// reservation transition observe one coherent lifecycle state.
	current, err := tx.PaymentOrder.Query().
		Where(paymentorder.IDEQ(order.ID), predicate.PaymentOrder(func(selector *entsql.Selector) {
			// SQLite (used by unit tests) has no FOR UPDATE syntax. The
			// production PostgreSQL selector gets the row lock here, before any
			// reservation/counter rows are touched.
			if selector.Dialect() == entdialect.Postgres {
				selector.ForUpdate()
			}
		})).
		Only(ctx)
	if err != nil {
		return false, "", fmt.Errorf("lock payment order for paid transition: %w", err)
	}
	previousStatus := current.Status
	productOrder := orderHasProductPurchase(current)

	switch current.Status {
	case OrderStatusPending:
		if productOrder {
			if err := consumePurchaseReservationRequiredTx(ctx, tx, current.ID); err != nil {
				return false, previousStatus, err
			}
		}
	case OrderStatusCancelled:
		if productOrder {
			reacquired, err := reacquireAndConsumePurchaseTx(ctx, tx, current, now)
			if err != nil {
				return false, previousStatus, err
			}
			if !reacquired {
				return false, previousStatus, fmt.Errorf("%w for recovered order %d", errPurchaseReservationUnavailable, current.ID)
			}
		}
	case OrderStatusExpired:
		// Expiry grace applies to every order type. Product orders additionally
		// need to reacquire their released purchase slot below, but a late
		// balance payment must not bypass the same payment cutoff.
		if current.UpdatedAt.Before(grace) {
			return false, previousStatus, fmt.Errorf("%w for order %d", errPaymentAfterExpiryGrace, current.ID)
		}
		if productOrder {
			reacquired, err := reacquireAndConsumePurchaseTx(ctx, tx, current, now)
			if err != nil {
				return false, previousStatus, err
			}
			if !reacquired {
				return false, previousStatus, fmt.Errorf("%w for recovered order %d", errPurchaseReservationUnavailable, current.ID)
			}
		}
	case OrderStatusFailed:
		// FAILED with PaidAt is a fulfillment failure for an already captured
		// payment. It is retriable, but a duplicate provider callback must not
		// transition it again. FAILED without PaidAt is the provider-init
		// failure path and may be recovered only by this confirmed callback.
		if current.PaidAt != nil {
			return false, previousStatus, nil
		}
		if productOrder {
			reacquired, err := reacquireAndConsumePurchaseTx(ctx, tx, current, now)
			if err != nil {
				return false, previousStatus, err
			}
			if !reacquired {
				return false, previousStatus, fmt.Errorf("%w for recovered order %d", errPurchaseReservationUnavailable, current.ID)
			}
		}
	default:
		// PAID/RECHARGING/terminal states are handled idempotently by the
		// caller; they must never consume a reservation a second time.
		return false, previousStatus, nil
	}

	_, err = tx.PaymentOrder.UpdateOneID(current.ID).
		SetStatus(OrderStatusPaid).
		SetPayAmount(paid).
		SetPaymentTradeNo(tradeNo).
		SetPaidAt(now).
		ClearFailedAt().
		ClearFailedReason().
		Save(ctx)
	if err != nil {
		return false, previousStatus, fmt.Errorf("update to PAID: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, previousStatus, fmt.Errorf("commit paid purchase transition: %w", err)
	}
	return true, previousStatus, nil
}

func isPurchaseLimitExceeded(err error) bool {
	reason := infraerrors.Reason(err)
	return reason == "DAILY_PURCHASE_LIMIT_EXCEEDED" || reason == "TOTAL_PURCHASE_LIMIT_EXCEEDED" || reason == "PERIODIC_PURCHASE_LIMIT_EXCEEDED"
}

// GetPurchaseLimitUsage returns the current calendar/rolling usage for one user.
func (s *PaymentService) GetPurchaseLimitUsage(ctx context.Context, userID int64) (map[string]ProductPurchaseLimitUsage, error) {
	now := time.Now()
	dailyStart := purchaseCalendarPeriodStart(now, purchaseLimitUnitDay)
	weeklyStart := purchaseCalendarPeriodStart(now, purchaseLimitUnitWeek)
	monthlyStart := purchaseCalendarPeriodStart(now, purchaseLimitUnitMonth)
	rows, err := s.entClient.QueryContext(ctx, `
SELECT product_type, product_id, period_type, reserved_count + consumed_count
FROM payment_purchase_counters
WHERE user_id = $1
  AND ((period_type = 'daily' AND period_start = $2)
    OR (period_type = 'weekly' AND period_start = $3)
    OR (period_type = 'monthly' AND period_start = $4)
    OR (period_type = 'total' AND period_start = $5))`, userID, dailyStart, weeklyStart, monthlyStart, totalPurchasePeriodStart)
	if err != nil {
		return nil, fmt.Errorf("query purchase limit usage: %w", err)
	}
	defer rows.Close()
	result := make(map[string]ProductPurchaseLimitUsage)
	for rows.Next() {
		var productType, periodType string
		var productID int64
		var count int
		if err := rows.Scan(&productType, &productID, &periodType, &count); err != nil {
			return nil, fmt.Errorf("scan purchase limit usage: %w", err)
		}
		key := purchaseLimitUsageKey(productType, productID)
		usage := result[key]
		switch periodType {
		case purchasePeriodDaily:
			usage.DailyUsed = count
		case purchasePeriodWeekly:
			usage.WeeklyUsed = count
		case purchasePeriodMonthly:
			usage.MonthlyUsed = count
		case purchasePeriodTotal:
			usage.TotalUsed = count
		}
		result[key] = usage
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query purchase limit usage: %w", err)
	}
	eventRows, err := s.entClient.QueryContext(ctx, `
SELECT product_type, product_id, occurred_at
FROM payment_purchase_limit_events
WHERE user_id = $1 AND status IN ('reserved', 'consumed')`, userID)
	if err != nil {
		return nil, fmt.Errorf("query rolling purchase usage: %w", err)
	}
	defer eventRows.Close()
	for eventRows.Next() {
		var productType string
		var productID int64
		var occurredAt time.Time
		if err := eventRows.Scan(&productType, &productID, &occurredAt); err != nil {
			return nil, fmt.Errorf("scan rolling purchase usage: %w", err)
		}
		key := purchaseLimitUsageKey(productType, productID)
		usage := result[key]
		usage.RollingEvents = append(usage.RollingEvents, occurredAt)
		result[key] = usage
	}
	if err := eventRows.Err(); err != nil {
		return nil, fmt.Errorf("query rolling purchase usage: %w", err)
	}
	return result, nil
}

func purchaseLimitUsageKey(productType string, productID int64) string {
	return productType + ":" + strconv.FormatInt(productID, 10)
}

func productPurchaseLimitStatusForPolicy(usage map[string]ProductPurchaseLimitUsage, productType string, productID int64, dailyLimit, totalLimit int, unit, mode string, windowSize int) ProductPurchaseLimitStatus {
	unit, mode, windowSize, err := normalizePurchaseLimitPolicy(unit, mode, windowSize)
	if err != nil {
		unit, mode, windowSize = purchaseLimitUnitDay, purchaseLimitModeCalendar, 1
	}
	used := usage[purchaseLimitUsageKey(productType, productID)]
	periodicUsed := used.DailyUsed
	if mode == purchaseLimitModeRolling {
		periodicUsed = 0
		now := time.Now()
		windowStart := rollingPurchaseWindowStart(now, unit, windowSize)
		for _, occurredAt := range used.RollingEvents {
			if !occurredAt.Before(windowStart) && occurredAt.Before(now) {
				periodicUsed++
			}
		}
	} else {
		switch unit {
		case purchaseLimitUnitWeek:
			periodicUsed = used.WeeklyUsed
		case purchaseLimitUnitMonth:
			periodicUsed = used.MonthlyUsed
		}
	}
	return ProductPurchaseLimitStatus{
		DailyLimit: dailyLimit, DailyRemaining: purchaseLimitRemaining(dailyLimit, periodicUsed),
		TotalLimit: totalLimit, TotalRemaining: purchaseLimitRemaining(totalLimit, used.TotalUsed),
		PurchaseLimitUnit: unit, PurchaseLimitMode: mode, PurchaseLimitWindowSize: windowSize,
	}
}

func productPurchaseLimitStatus(usage map[string]ProductPurchaseLimitUsage, productType string, productID int64, dailyLimit, totalLimit int) ProductPurchaseLimitStatus {
	return productPurchaseLimitStatusForPolicy(usage, productType, productID, dailyLimit, totalLimit, purchaseLimitUnitDay, purchaseLimitModeCalendar, 1)
}

func CurrencyProductPurchaseLimitStatus(usage map[string]ProductPurchaseLimitUsage, productID int64, dailyLimit, totalLimit int) ProductPurchaseLimitStatus {
	return productPurchaseLimitStatus(usage, purchaseProductCurrency, productID, dailyLimit, totalLimit)
}

func CurrencyProductPurchaseLimitStatusWithPolicy(usage map[string]ProductPurchaseLimitUsage, productID int64, dailyLimit, totalLimit int, unit, mode string, windowSize int) ProductPurchaseLimitStatus {
	return productPurchaseLimitStatusForPolicy(usage, purchaseProductCurrency, productID, dailyLimit, totalLimit, unit, mode, windowSize)
}

func SubscriptionPlanPurchaseLimitStatus(usage map[string]ProductPurchaseLimitUsage, planID int64, dailyLimit, totalLimit int) ProductPurchaseLimitStatus {
	return productPurchaseLimitStatus(usage, purchaseProductSubscription, planID, dailyLimit, totalLimit)
}

func SubscriptionPlanPurchaseLimitStatusWithPolicy(usage map[string]ProductPurchaseLimitUsage, planID int64, dailyLimit, totalLimit int, unit, mode string, windowSize int) ProductPurchaseLimitStatus {
	return productPurchaseLimitStatusForPolicy(usage, purchaseProductSubscription, planID, dailyLimit, totalLimit, unit, mode, windowSize)
}

func purchaseLimitRemaining(limit, used int) int {
	if limit <= 0 {
		return 0
	}
	if used >= limit {
		return 0
	}
	return limit - used
}
