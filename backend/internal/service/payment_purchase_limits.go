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
	purchasePeriodTotal         = "total"
	purchaseReservationReserved = "reserved"
	purchaseReservationConsumed = "consumed"
	purchaseReservationReleased = "released"
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
}

type purchaseReservationRecord struct {
	userID           int64
	productType      string
	productID        int64
	dailyPeriodStart time.Time
}

type purchaseCounterKey struct {
	productType string
	productID   int64
	periodType  string
}

// ProductPurchaseLimitStatus is returned beside checkout products for one user.
type ProductPurchaseLimitStatus struct {
	DailyLimit     int `json:"daily_purchase_limit"`
	DailyRemaining int `json:"daily_purchase_remaining"`
	TotalLimit     int `json:"total_purchase_limit"`
	TotalRemaining int `json:"total_purchase_remaining"`
}

type ProductPurchaseLimitUsage struct {
	DailyUsed int
	TotalUsed int
}

type paymentPurchaseSQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func invalidPurchaseLimitError(field string) error {
	return infraerrors.BadRequest("INVALID_PURCHASE_LIMIT", "purchase limits must be non-negative 32-bit integers").
		WithMetadata(map[string]string{"field": field})
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
		return &purchaseLimitSpec{
			productType: purchaseProductCurrency,
			productID:   product.ID,
			dailyLimit:  product.DailyPurchaseLimit,
			totalLimit:  product.TotalPurchaseLimit,
		}
	}
	if plan != nil {
		return &purchaseLimitSpec{
			productType: purchaseProductSubscription,
			productID:   plan.ID,
			dailyLimit:  plan.DailyPurchaseLimit,
			totalLimit:  plan.TotalPurchaseLimit,
		}
	}
	return nil
}

func purchaseDailyPeriodStart(now time.Time) time.Time {
	local := now.In(beijingLocation)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *PaymentService) reservePurchaseTx(ctx context.Context, tx *dbent.Tx, orderID, userID int64, spec *purchaseLimitSpec, now time.Time) error {
	if spec == nil {
		return nil
	}
	periodStart := purchaseDailyPeriodStart(now)
	if err := reservePurchaseCounter(ctx, tx, userID, spec.productType, spec.productID, purchasePeriodDaily, periodStart, spec.dailyLimit); err != nil {
		return err
	}
	if err := reservePurchaseCounter(ctx, tx, userID, spec.productType, spec.productID, purchasePeriodTotal, totalPurchasePeriodStart, spec.totalLimit); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO payment_purchase_reservations
    (order_id, user_id, product_type, product_id, daily_period_start, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, 'reserved', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		orderID, userID, spec.productType, spec.productID, periodStart); err != nil {
		return fmt.Errorf("create purchase reservation: %w", err)
	}
	return nil
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
	code := "TOTAL_PURCHASE_LIMIT_EXCEEDED"
	if periodType == purchasePeriodDaily {
		code = "DAILY_PURCHASE_LIMIT_EXCEEDED"
	}
	return infraerrors.TooManyRequests(code, "purchase limit exceeded").WithMetadata(map[string]string{
		"product_type": productType,
		"product_id":   strconv.FormatInt(productID, 10),
		"limit":        strconv.Itoa(limit),
		"remaining":    "0",
	})
}

// consumeImmediatePurchaseCounters increments the same counters used by
// external pending-order reservations, but directly as consumed because an
// internal mall purchase settles in one transaction.
func consumeImmediatePurchaseCounters(ctx context.Context, db paymentPurchaseSQL, userID int64, spec *purchaseLimitSpec, now time.Time) error {
	if spec == nil {
		return nil
	}
	dailyStart := purchaseDailyPeriodStart(now)
	if err := consumeImmediatePurchaseCounter(ctx, db, userID, spec.productType, spec.productID, purchasePeriodDaily, dailyStart, spec.dailyLimit); err != nil {
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
	code := "TOTAL_PURCHASE_LIMIT_EXCEEDED"
	if periodType == purchasePeriodDaily {
		code = "DAILY_PURCHASE_LIMIT_EXCEEDED"
	}
	return infraerrors.TooManyRequests(code, "purchase limit exceeded").WithMetadata(map[string]string{
		"product_type": productType,
		"product_id":   strconv.FormatInt(productID, 10),
		"limit":        strconv.Itoa(limit),
		"remaining":    "0",
	})
}

func mutatePurchaseReservation(ctx context.Context, db paymentPurchaseSQL, orderID int64, fromStatus, toStatus string, dailyPeriodStart *time.Time) (*purchaseReservationRecord, error) {
	query := `
UPDATE payment_purchase_reservations
SET status = $2, updated_at = CURRENT_TIMESTAMP
WHERE order_id = $1 AND status = $3
RETURNING user_id, product_type, product_id, daily_period_start`
	args := []any{orderID, toStatus, fromStatus}
	if dailyPeriodStart != nil {
		query = `
UPDATE payment_purchase_reservations
SET status = $2, daily_period_start = $4, updated_at = CURRENT_TIMESTAMP
WHERE order_id = $1 AND status = $3
RETURNING user_id, product_type, product_id, daily_period_start`
		args = append(args, *dailyPeriodStart)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("transition purchase reservation: %w", err)
	}
	closeRows := func() error {
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close purchase reservation transition rows: %w", err)
		}
		return nil
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("transition purchase reservation: %w", err)
		}
		if err := closeRows(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	record := &purchaseReservationRecord{}
	if err := rows.Scan(&record.userID, &record.productType, &record.productID, &record.dailyPeriodStart); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("scan purchase reservation transition: %w", err)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("transition purchase reservation: %w", err)
	}
	if err := closeRows(); err != nil {
		return nil, err
	}
	return record, nil
}

func updatePurchaseCounter(ctx context.Context, db paymentPurchaseSQL, record *purchaseReservationRecord, periodType string, reservedDelta, consumedDelta int) error {
	periodStart := totalPurchasePeriodStart
	if periodType == purchasePeriodDaily {
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

func transitionPurchaseCounters(ctx context.Context, db paymentPurchaseSQL, record *purchaseReservationRecord, reservedDelta, consumedDelta int) error {
	if record == nil {
		return nil
	}
	// Every path locks daily before total to avoid cross-request deadlocks.
	if err := updatePurchaseCounter(ctx, db, record, purchasePeriodDaily, reservedDelta, consumedDelta); err != nil {
		return err
	}
	return updatePurchaseCounter(ctx, db, record, purchasePeriodTotal, reservedDelta, consumedDelta)
}

func consumePurchaseReservationTx(ctx context.Context, tx *dbent.Tx, orderID int64) error {
	record, err := mutatePurchaseReservation(ctx, tx, orderID, purchaseReservationReserved, purchaseReservationConsumed, nil)
	if err != nil || record == nil {
		return err
	}
	return transitionPurchaseCounters(ctx, tx, record, -1, 1)
}

// consumePurchaseReservationRequiredTx is used by a paid transition. A
// missing reservation is an invariant violation, not an idempotent no-op:
// fulfilling without consuming a reservation would bypass purchase limits.
func consumePurchaseReservationRequiredTx(ctx context.Context, tx *dbent.Tx, orderID int64) error {
	record, err := mutatePurchaseReservation(ctx, tx, orderID, purchaseReservationReserved, purchaseReservationConsumed, nil)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("%w for order %d", errPurchaseReservationUnavailable, orderID)
	}
	return transitionPurchaseCounters(ctx, tx, record, -1, 1)
}

func releaseReservedPurchaseTx(ctx context.Context, tx *dbent.Tx, orderID int64) error {
	record, err := mutatePurchaseReservation(ctx, tx, orderID, purchaseReservationReserved, purchaseReservationReleased, nil)
	if err != nil || record == nil {
		return err
	}
	return transitionPurchaseCounters(ctx, tx, record, -1, 0)
}

func releaseConsumedPurchaseTx(ctx context.Context, tx *dbent.Tx, orderID int64) error {
	record, err := mutatePurchaseReservation(ctx, tx, orderID, purchaseReservationConsumed, purchaseReservationReleased, nil)
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
	periodStart := purchaseDailyPeriodStart(now)
	record, err := mutatePurchaseReservation(ctx, tx, order.ID, purchaseReservationReleased, purchaseReservationReserved, &periodStart)
	if err != nil || record == nil {
		return false, err
	}
	if err := reservePurchaseCounter(ctx, tx, record.userID, record.productType, record.productID, purchasePeriodDaily, periodStart, order.DailyPurchaseLimitSnapshot); err != nil {
		return false, err
	}
	if err := reservePurchaseCounter(ctx, tx, record.userID, record.productType, record.productID, purchasePeriodTotal, totalPurchasePeriodStart, order.TotalPurchaseLimitSnapshot); err != nil {
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
	return reason == "DAILY_PURCHASE_LIMIT_EXCEEDED" || reason == "TOTAL_PURCHASE_LIMIT_EXCEEDED"
}

// GetPurchaseLimitUsage returns reserved plus consumed counts for today's Beijing period and all time.
func (s *PaymentService) GetPurchaseLimitUsage(ctx context.Context, userID int64) (map[string]ProductPurchaseLimitUsage, error) {
	periodStart := purchaseDailyPeriodStart(time.Now())
	rows, err := s.entClient.QueryContext(ctx, `
SELECT product_type, product_id, period_type, reserved_count + consumed_count
FROM payment_purchase_counters
WHERE user_id = $1
  AND ((period_type = 'daily' AND period_start = $2)
       OR (period_type = 'total' AND period_start = $3))`, userID, periodStart, totalPurchasePeriodStart)
	if err != nil {
		return nil, fmt.Errorf("query purchase limit usage: %w", err)
	}
	used := make(map[purchaseCounterKey]int)
	for rows.Next() {
		var key purchaseCounterKey
		var count int
		if err := rows.Scan(&key.productType, &key.productID, &key.periodType, &count); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan purchase limit usage: %w", err)
		}
		used[key] = count
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("query purchase limit usage: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close purchase limit usage rows: %w", err)
	}
	result := make(map[string]ProductPurchaseLimitUsage)
	for key, count := range used {
		mapKey := purchaseLimitUsageKey(key.productType, key.productID)
		status := result[mapKey]
		if key.periodType == purchasePeriodDaily {
			status.DailyUsed = count
		} else {
			status.TotalUsed = count
		}
		result[mapKey] = status
	}
	return result, nil
}

func purchaseLimitUsageKey(productType string, productID int64) string {
	return productType + ":" + strconv.FormatInt(productID, 10)
}

func productPurchaseLimitStatus(usage map[string]ProductPurchaseLimitUsage, productType string, productID int64, dailyLimit, totalLimit int) ProductPurchaseLimitStatus {
	used := usage[purchaseLimitUsageKey(productType, productID)]
	return ProductPurchaseLimitStatus{
		DailyLimit:     dailyLimit,
		DailyRemaining: purchaseLimitRemaining(dailyLimit, used.DailyUsed),
		TotalLimit:     totalLimit,
		TotalRemaining: purchaseLimitRemaining(totalLimit, used.TotalUsed),
	}
}

func CurrencyProductPurchaseLimitStatus(usage map[string]ProductPurchaseLimitUsage, productID int64, dailyLimit, totalLimit int) ProductPurchaseLimitStatus {
	return productPurchaseLimitStatus(usage, purchaseProductCurrency, productID, dailyLimit, totalLimit)
}

func SubscriptionPlanPurchaseLimitStatus(usage map[string]ProductPurchaseLimitUsage, planID int64, dailyLimit, totalLimit int) ProductPurchaseLimitStatus {
	return productPurchaseLimitStatus(usage, purchaseProductSubscription, planID, dailyLimit, totalLimit)
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
