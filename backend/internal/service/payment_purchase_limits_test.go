package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentpurchasecounter"
	"github.com/Wei-Shaw/sub2api/ent/paymentpurchasereservation"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestPurchaseDailyPeriodStartUsesBeijingNaturalDay(t *testing.T) {
	beforeMidnightUTC := time.Date(2026, time.July, 21, 15, 59, 59, 0, time.UTC)
	afterMidnightUTC := beforeMidnightUTC.Add(2 * time.Second)

	require.Equal(t, "2026-07-21", purchaseDailyPeriodStart(beforeMidnightUTC).Format("2006-01-02"))
	require.Equal(t, "2026-07-22", purchaseDailyPeriodStart(afterMidnightUTC).Format("2006-01-02"))
}

func TestPurchaseReservationRejectsDailyAndTotalLastSlot(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	svc := &PaymentService{entClient: client}

	first := createPurchaseLimitTestOrder(t, ctx, client, userID, "LIMIT-FIRST")
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.reservePurchaseTx(ctx, tx, first.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductCurrency,
		productID:   11,
		dailyLimit:  1,
		totalLimit:  2,
	}, time.Now()))
	require.NoError(t, tx.Commit())

	second := createPurchaseLimitTestOrder(t, ctx, client, userID, "LIMIT-SECOND")
	tx, err = client.Tx(ctx)
	require.NoError(t, err)
	err = svc.reservePurchaseTx(ctx, tx, second.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductCurrency,
		productID:   11,
		dailyLimit:  1,
		totalLimit:  2,
	}, time.Now())
	require.Equal(t, "DAILY_PURCHASE_LIMIT_EXCEEDED", infraerrors.Reason(err))
	require.NoError(t, tx.Rollback())

	third := createPurchaseLimitTestOrder(t, ctx, client, userID, "LIMIT-THIRD")
	tx, err = client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.reservePurchaseTx(ctx, tx, third.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductSubscription,
		productID:   22,
		dailyLimit:  0,
		totalLimit:  1,
	}, time.Now()))
	require.NoError(t, tx.Commit())

	fourth := createPurchaseLimitTestOrder(t, ctx, client, userID, "LIMIT-FOURTH")
	tx, err = client.Tx(ctx)
	require.NoError(t, err)
	err = svc.reservePurchaseTx(ctx, tx, fourth.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductSubscription,
		productID:   22,
		dailyLimit:  0,
		totalLimit:  1,
	}, time.Now())
	require.Equal(t, "TOTAL_PURCHASE_LIMIT_EXCEEDED", infraerrors.Reason(err))
	require.NoError(t, tx.Rollback())
}

func TestPurchaseReservationTransitionsAndCheckoutRemaining(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	svc := &PaymentService{entClient: client}
	order := createPurchaseLimitTestOrder(t, ctx, client, userID, "STATE-ORDER")

	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.reservePurchaseTx(ctx, tx, order.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductCurrency,
		productID:   33,
		dailyLimit:  2,
		totalLimit:  3,
	}, time.Now()))
	require.NoError(t, tx.Commit())

	usage, err := svc.GetPurchaseLimitUsage(ctx, userID)
	require.NoError(t, err)
	status := CurrencyProductPurchaseLimitStatus(usage, 33, 2, 3)
	require.Equal(t, ProductPurchaseLimitStatus{DailyLimit: 2, DailyRemaining: 1, TotalLimit: 3, TotalRemaining: 2}, status)

	tx, err = client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, consumePurchaseReservationTx(ctx, tx, order.ID))
	require.NoError(t, tx.Commit())
	reservation, err := client.PaymentPurchaseReservation.Query().
		Where(paymentpurchasereservation.OrderIDEQ(order.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, purchaseReservationConsumed, reservation.Status)
	for _, counter := range mustPurchaseCounters(t, ctx, client, userID, 33) {
		require.Zero(t, counter.ReservedCount)
		require.Equal(t, 1, counter.ConsumedCount)
	}

	tx, err = client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, releaseConsumedPurchaseTx(ctx, tx, order.ID))
	require.NoError(t, tx.Commit())
	reservation, err = client.PaymentPurchaseReservation.Query().
		Where(paymentpurchasereservation.OrderIDEQ(order.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, purchaseReservationReleased, reservation.Status)
	for _, counter := range mustPurchaseCounters(t, ctx, client, userID, 33) {
		require.Zero(t, counter.ReservedCount)
		require.Zero(t, counter.ConsumedCount)
	}
}

func TestProductPolicySnapshotDoesNotChangeWithShelfEdit(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	product, err := client.CurrencyProduct.Create().
		SetName("snapshot").
		SetDescription("").
		SetPaymentPrice(10).
		SetCreditedAmount(12).
		SetCreditedPermanentAmount(12).
		SetDailyPurchaseLimit(1).
		SetTotalPurchaseLimit(4).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	order, err := svc.createOrderInTxWithProduct(ctx, CreateOrderRequest{
		UserID: userID, PaymentType: payment.TypeAlipay, OrderType: payment.OrderTypeBalance, ProductID: product.ID,
	}, &User{ID: userID, Email: "snapshot@example.com", Username: "snapshot"}, nil, product,
		&PaymentConfig{MaxPendingOrders: 5, OrderTimeoutMin: 30}, 12, 10, 0, 10, nil)
	require.NoError(t, err)
	require.Equal(t, 1, order.DailyPurchaseLimitSnapshot)
	require.Equal(t, 4, order.TotalPurchaseLimitSnapshot)

	_, err = client.CurrencyProduct.UpdateOneID(product.ID).
		SetDailyPurchaseLimit(9).
		SetTotalPurchaseLimit(12).
		Save(ctx)
	require.NoError(t, err)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, 1, reloaded.DailyPurchaseLimitSnapshot)
	require.Equal(t, 4, reloaded.TotalPurchaseLimitSnapshot)
}

func TestSubscriptionPlanPurchaseLimitCRUDAndValidation(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	configService := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	plan, err := configService.CreatePlan(ctx, CreatePlanRequest{
		GroupID: 1, Name: "limited plan", Price: 10, ValidityDays: 30, ValidityUnit: "day",
		ForSale: true, DailyPurchaseLimit: 2, TotalPurchaseLimit: 6,
	})
	require.NoError(t, err)
	require.Equal(t, 2, plan.DailyPurchaseLimit)
	require.Equal(t, 6, plan.TotalPurchaseLimit)

	negative := -1
	_, err = configService.UpdatePlan(ctx, plan.ID, UpdatePlanRequest{TotalPurchaseLimit: &negative})
	require.Equal(t, "INVALID_PURCHASE_LIMIT", infraerrors.Reason(err))
	tooLarge := maxPurchaseLimit + 1
	_, err = configService.UpdatePlan(ctx, plan.ID, UpdatePlanRequest{DailyPurchaseLimit: &tooLarge})
	require.Equal(t, "INVALID_PURCHASE_LIMIT", infraerrors.Reason(err))
	require.Equal(t, 400, infraerrors.Code(err))

	daily, total := 3, 8
	plan, err = configService.UpdatePlan(ctx, plan.ID, UpdatePlanRequest{
		DailyPurchaseLimit: &daily, TotalPurchaseLimit: &total,
	})
	require.NoError(t, err)
	require.Equal(t, daily, plan.DailyPurchaseLimit)
	require.Equal(t, total, plan.TotalPurchaseLimit)
}

func TestCancelAndRefundReleaseOnlyEligiblePurchaseReservations(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	svc := &PaymentService{entClient: client}
	order := createPurchaseLimitTestOrder(t, ctx, client, userID, "CANCEL-RELEASE")

	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.reservePurchaseTx(ctx, tx, order.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductCurrency, productID: 44, dailyLimit: 1, totalLimit: 1,
	}, time.Now()))
	require.NoError(t, tx.Commit())
	order.PaymentType = ""
	outcome, err := svc.cancelCore(ctx, order, OrderStatusCancelled, "test", "cancel")
	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)
	reservation, err := client.PaymentPurchaseReservation.Query().
		Where(paymentpurchasereservation.OrderIDEQ(order.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, purchaseReservationReleased, reservation.Status)

	refundable := createPurchaseLimitTestOrder(t, ctx, client, userID, "REFUND-RELEASE")
	tx, err = client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.reservePurchaseTx(ctx, tx, refundable.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductCurrency, productID: 45, dailyLimit: 2, totalLimit: 2,
	}, time.Now()))
	require.NoError(t, consumePurchaseReservationTx(ctx, tx, refundable.ID))
	require.NoError(t, tx.Commit())

	_, err = svc.markRefundOk(ctx, &RefundPlan{OrderID: refundable.ID, Order: refundable, RefundAmount: 5, Reason: "partial"})
	require.NoError(t, err)
	reservation, err = client.PaymentPurchaseReservation.Query().
		Where(paymentpurchasereservation.OrderIDEQ(refundable.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, purchaseReservationConsumed, reservation.Status)

	_, err = svc.markRefundOk(ctx, &RefundPlan{OrderID: refundable.ID, Order: refundable, RefundAmount: refundable.Amount, Reason: "full", Force: true})
	require.NoError(t, err)
	reservation, err = client.PaymentPurchaseReservation.Query().
		Where(paymentpurchasereservation.OrderIDEQ(refundable.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, purchaseReservationReleased, reservation.Status)
}

func TestReleasedLateOrderCannotReclaimOccupiedSnapshotLimit(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	svc := &PaymentService{entClient: client}
	first := createPurchaseLimitTestOrder(t, ctx, client, userID, "LATE-FIRST")
	first.DailyPurchaseLimitSnapshot = 1
	first.TotalPurchaseLimitSnapshot = 0
	_, err := client.PaymentOrder.UpdateOneID(first.ID).
		SetDailyPurchaseLimitSnapshot(1).
		SetTotalPurchaseLimitSnapshot(0).
		Save(ctx)
	require.NoError(t, err)

	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.reservePurchaseTx(ctx, tx, first.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductCurrency, productID: 46, dailyLimit: 1, totalLimit: 0,
	}, time.Now()))
	require.NoError(t, releaseReservedPurchaseTx(ctx, tx, first.ID))
	require.NoError(t, tx.Commit())

	second := createPurchaseLimitTestOrder(t, ctx, client, userID, "LATE-SECOND")
	tx, err = client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.reservePurchaseTx(ctx, tx, second.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductCurrency, productID: 46, dailyLimit: 1, totalLimit: 0,
	}, time.Now()))
	require.NoError(t, tx.Commit())

	tx, err = client.Tx(ctx)
	require.NoError(t, err)
	reacquired, err := reacquireAndConsumePurchaseTx(ctx, tx, first, time.Now())
	require.False(t, reacquired)
	require.Equal(t, "DAILY_PURCHASE_LIMIT_EXCEEDED", infraerrors.Reason(err))
	require.NoError(t, tx.Rollback())
	reservation, err := client.PaymentPurchaseReservation.Query().
		Where(paymentpurchasereservation.OrderIDEQ(first.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, purchaseReservationReleased, reservation.Status)
}

func TestPaidTransitionReloadsCancelledOrderAndReacquiresReleasedSlot(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	svc := &PaymentService{entClient: client}
	stale := createPurchaseLimitTestOrder(t, ctx, client, userID, "STALE-CANCELLED")
	stale, err := client.PaymentOrder.UpdateOneID(stale.ID).
		SetCurrencyProductID(91).
		SetDailyPurchaseLimitSnapshot(1).
		SetTotalPurchaseLimitSnapshot(1).
		Save(ctx)
	require.NoError(t, err)

	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.reservePurchaseTx(ctx, tx, stale.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductCurrency, productID: 91, dailyLimit: 1, totalLimit: 1,
	}, time.Now()))
	require.NoError(t, tx.Commit())

	stale.PaymentType = ""
	outcome, err := svc.cancelCore(ctx, stale, OrderStatusCancelled, "test", "cancel")
	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)
	require.Equal(t, OrderStatusPending, stale.Status, "the callback input must remain the stale snapshot")

	updated, previousStatus, err := svc.transitionOrderToPaidWithPurchase(
		ctx, stale, "trade-stale-cancelled", stale.PayAmount, time.Now(), time.Now().Add(-paymentGraceMinutes*time.Minute),
	)
	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, OrderStatusCancelled, previousStatus)

	reloaded, err := client.PaymentOrder.Get(ctx, stale.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPaid, reloaded.Status)
	require.NotNil(t, reloaded.PaidAt)
	reservation, err := client.PaymentPurchaseReservation.Query().
		Where(paymentpurchasereservation.OrderIDEQ(stale.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, purchaseReservationConsumed, reservation.Status)
	for _, counter := range mustPurchaseCounters(t, ctx, client, userID, 91) {
		require.Zero(t, counter.ReservedCount)
		require.Equal(t, 1, counter.ConsumedCount)
	}
}

func TestPaidTransitionRecoversProviderInitFailedOrderOnlyAfterReacquire(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	svc := &PaymentService{entClient: client}
	stale := createPurchaseLimitTestOrder(t, ctx, client, userID, "STALE-FAILED")
	stale, err := client.PaymentOrder.UpdateOneID(stale.ID).
		SetCurrencyProductID(92).
		SetDailyPurchaseLimitSnapshot(1).
		SetTotalPurchaseLimitSnapshot(1).
		Save(ctx)
	require.NoError(t, err)

	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.reservePurchaseTx(ctx, tx, stale.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductCurrency, productID: 92, dailyLimit: 1, totalLimit: 1,
	}, time.Now()))
	require.NoError(t, tx.Commit())
	require.NoError(t, svc.failPendingOrderAndReleasePurchase(ctx, stale.ID))

	updated, previousStatus, err := svc.transitionOrderToPaidWithPurchase(
		ctx, stale, "trade-stale-failed", stale.PayAmount, time.Now(), time.Now().Add(-paymentGraceMinutes*time.Minute),
	)
	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, OrderStatusFailed, previousStatus)

	reloaded, err := client.PaymentOrder.Get(ctx, stale.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPaid, reloaded.Status)
	require.NotNil(t, reloaded.PaidAt)
	reservation, err := client.PaymentPurchaseReservation.Query().
		Where(paymentpurchasereservation.OrderIDEQ(stale.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, purchaseReservationConsumed, reservation.Status)
}

func TestPaidTransitionRejectsExpiredBalanceOrderAfterGrace(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	order := createPurchaseLimitTestOrder(t, ctx, client, userID, "EXPIRED-BALANCE-GRACE")
	oldUpdatedAt := time.Now().Add(-(paymentGraceMinutes*time.Minute + time.Second))
	order, err := client.PaymentOrder.UpdateOneID(order.ID).
		SetStatus(OrderStatusExpired).
		SetUpdatedAt(oldUpdatedAt).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	updated, previousStatus, err := svc.transitionOrderToPaidWithPurchase(
		ctx, order, "expired-balance-trade", order.PayAmount, time.Now(), time.Now().Add(-paymentGraceMinutes*time.Minute),
	)
	require.ErrorIs(t, err, errPaymentAfterExpiryGrace)
	require.False(t, updated)
	require.Equal(t, OrderStatusExpired, previousStatus)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusExpired, reloaded.Status)
	require.Nil(t, reloaded.PaidAt)
}

func TestLateExpiredBalanceCallbackAcknowledgesAfterGrace(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	order := createPurchaseLimitTestOrder(t, ctx, client, userID, "EXPIRED-BALANCE-ACK")
	order, err := client.PaymentOrder.UpdateOneID(order.ID).
		SetStatus(OrderStatusExpired).
		SetUpdatedAt(time.Now().Add(-(paymentGraceMinutes*time.Minute + time.Second))).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	require.NoError(t, svc.toPaid(ctx, order, "expired-balance-ack-trade", order.PayAmount, payment.TypeAlipay))

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusExpired, reloaded.Status)
	count, err := client.PaymentAuditLog.Query().
		Where(
			paymentauditlog.OrderIDEQ(fmt.Sprintf("%d", order.ID)),
			paymentauditlog.ActionEQ("PAYMENT_AFTER_EXPIRY"),
		).
		Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestRejectedProductRefundRecoveryUsesRecordedResultWithoutSecondCallbackRefund(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	order := createPurchaseLimitTestOrder(t, ctx, client, userID, "REFUND-RECOVER-SUCCESS")
	order, err := client.PaymentOrder.UpdateOneID(order.ID).
		SetCurrencyProductID(901).
		SetStatus(OrderStatusRefunding).
		SetPaidAt(time.Now()).
		SetPaymentTradeNo("refund-recover-trade").
		SetRefundAmount(order.Amount).
		SetRefundReason("late purchase callback").
		Save(ctx)
	require.NoError(t, err)
	createPurchaseLimitRefundAudit(t, ctx, client, order.ID, purchaseLimitRejectedPaymentAudit, `{"reason":"late"}`)
	createPurchaseLimitRefundAudit(t, ctx, client, order.ID, purchaseLimitRefundResultAudit, `{"status":"success","refundID":"provider-rf-1"}`)

	// No provider is configured: if recovery attempted another gateway call,
	// this callback would fail. The recorded result must finalize locally.
	svc := &PaymentService{entClient: client}
	require.NoError(t, svc.toPaid(ctx, order, "refund-recover-trade", order.PayAmount, payment.TypeAlipay))

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefunded, reloaded.Status)
	require.NotNil(t, reloaded.RefundAt)
}

func TestRejectedProductRefundRecoveryMarksStaleUnknownAttemptFailed(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	order := createPurchaseLimitTestOrder(t, ctx, client, userID, "REFUND-RECOVER-STALE")
	order, err := client.PaymentOrder.UpdateOneID(order.ID).
		SetCurrencyProductID(902).
		SetStatus(OrderStatusRefunding).
		SetUpdatedAt(time.Now().Add(-(paymentFulfillmentLeaseDuration + time.Second))).
		SetPaidAt(time.Now()).
		SetRefundAmount(order.Amount).
		SetRefundReason("late purchase callback").
		Save(ctx)
	require.NoError(t, err)
	createPurchaseLimitRefundAudit(t, ctx, client, order.ID, purchaseLimitRejectedPaymentAudit, `{"reason":"late"}`)

	svc := &PaymentService{entClient: client}
	handled, err := svc.recoverRejectedProductRefund(ctx, order)
	require.True(t, handled)
	require.NoError(t, err)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundFailed, reloaded.Status)
	require.NotNil(t, reloaded.FailedReason)
	require.Contains(t, *reloaded.FailedReason, "verify the provider")
}

func TestUnpaidFailedOrderCannotEnterFulfillment(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	order := createPurchaseLimitTestOrder(t, ctx, client, userID, "UNPAID-FAILED")
	_, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusFailed).Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	err = svc.ExecuteBalanceFulfillment(ctx, order.ID)
	require.Equal(t, "INVALID_STATUS", infraerrors.Reason(err))
	require.Equal(t, 400, infraerrors.Code(err))
	require.NoError(t, svc.alreadyProcessed(ctx, order))
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusFailed, reloaded.Status)
	require.Nil(t, reloaded.PaidAt)
}

func TestProviderInitFailedLatePaymentWithoutSlotEntersAutomaticRefund(t *testing.T) {
	ctx := context.Background()
	client := newPurchaseLimitTestClient(t)
	userID := createPurchaseLimitTestUser(t, ctx, client)
	svc := &PaymentService{entClient: client}
	late := createPurchaseLimitTestOrder(t, ctx, client, userID, "FAILED-NO-SLOT")
	late, err := client.PaymentOrder.UpdateOneID(late.ID).
		SetCurrencyProductID(93).
		SetDailyPurchaseLimitSnapshot(1).
		SetTotalPurchaseLimitSnapshot(1).
		Save(ctx)
	require.NoError(t, err)
	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.reservePurchaseTx(ctx, tx, late.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductCurrency, productID: 93, dailyLimit: 1, totalLimit: 1,
	}, time.Now()))
	require.NoError(t, tx.Commit())
	require.NoError(t, svc.failPendingOrderAndReleasePurchase(ctx, late.ID))

	occupying := createPurchaseLimitTestOrder(t, ctx, client, userID, "FAILED-SLOT-OCCUPIED")
	tx, err = client.Tx(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.reservePurchaseTx(ctx, tx, occupying.ID, userID, &purchaseLimitSpec{
		productType: purchaseProductCurrency, productID: 93, dailyLimit: 1, totalLimit: 1,
	}, time.Now()))
	require.NoError(t, tx.Commit())

	require.NoError(t, svc.toPaid(ctx, late, "late-trade-no-slot", late.PayAmount, payment.TypeAlipay))
	reloaded, err := client.PaymentOrder.Get(ctx, late.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundFailed, reloaded.Status, "missing refund provider must fail closed without fulfillment")
	require.NotNil(t, reloaded.PaidAt)
	reservation, err := client.PaymentPurchaseReservation.Query().
		Where(paymentpurchasereservation.OrderIDEQ(late.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, purchaseReservationReleased, reservation.Status)
	for _, counter := range mustPurchaseCounters(t, ctx, client, userID, 93) {
		require.Equal(t, 1, counter.ReservedCount)
		require.Zero(t, counter.ConsumedCount)
	}
}

func newPurchaseLimitTestClient(t *testing.T) *dbent.Client {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	driver := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func createPurchaseLimitTestUser(t *testing.T, ctx context.Context, client *dbent.Client) int64 {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_")) + "@example.com").
		SetPasswordHash("hash").
		SetUsername("purchase-limit-user").
		Save(ctx)
	require.NoError(t, err)
	return user.ID
}

func createPurchaseLimitTestOrder(t *testing.T, ctx context.Context, client *dbent.Client, userID int64, code string) *dbent.PaymentOrder {
	t.Helper()
	order, err := client.PaymentOrder.Create().
		SetUserID(userID).
		SetUserEmail("purchase-limit@example.com").
		SetUserName("purchase-limit-user").
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode(code).
		SetOutTradeNo("sub2_" + strings.ToLower(strings.ReplaceAll(code, "-", "_"))).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		Save(ctx)
	require.NoError(t, err)
	return order
}

func mustPurchaseCounters(t *testing.T, ctx context.Context, client *dbent.Client, userID, productID int64) []*dbent.PaymentPurchaseCounter {
	t.Helper()
	counters, err := client.PaymentPurchaseCounter.Query().
		Where(
			paymentpurchasecounter.UserIDEQ(userID),
			paymentpurchasecounter.ProductTypeEQ(purchaseProductCurrency),
			paymentpurchasecounter.ProductIDEQ(productID),
		).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, counters, 2)
	return counters
}

func createPurchaseLimitRefundAudit(t *testing.T, ctx context.Context, client *dbent.Client, orderID int64, action, detail string) {
	t.Helper()
	_, err := client.PaymentAuditLog.Create().
		SetOrderID(fmt.Sprintf("%d", orderID)).
		SetAction(action).
		SetDetail(detail).
		SetOperator("system").
		Save(ctx)
	require.NoError(t, err)
}
