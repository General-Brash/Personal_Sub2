package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentrefundattempt"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

const (
	refundAttemptSourceAdmin                 = "admin"
	refundAttemptSourcePurchaseLimitRejected = "purchase_limit_rejected"

	refundDeductionStateNone     = "none"
	refundDeductionStateHeld     = "held"
	refundDeductionStateConsumed = "consumed"
	refundDeductionStateReturned = "returned"

	refundProviderStateCalling   = "calling"
	refundProviderStatePending   = "pending"
	refundProviderStateSucceeded = "succeeded"
	refundProviderStateFailed    = "failed"
	refundProviderStateCanceled  = "canceled"
	refundProviderStateUnknown   = "unknown"
)

func (s *PaymentService) latestRefundAttempt(ctx context.Context, client *dbent.Client, orderID int64) (*dbent.PaymentRefundAttempt, error) {
	attempt, err := client.PaymentRefundAttempt.Query().
		Where(paymentrefundattempt.OrderIDEQ(orderID)).
		Order(dbent.Desc(paymentrefundattempt.FieldCreatedAt), dbent.Desc(paymentrefundattempt.FieldID)).
		First(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return attempt, nil
}

func refundAttemptAllowsRetry(attempt *dbent.PaymentRefundAttempt) bool {
	if attempt == nil || attempt.ManualReview {
		return false
	}
	if attempt.ProviderState != refundProviderStateFailed && attempt.ProviderState != refundProviderStateCanceled {
		return false
	}
	return attempt.DeductionState == refundDeductionStateReturned || attempt.DeductionState == refundDeductionStateNone
}

//nolint:unused // 保留给 unit 测试与退款状态审查使用。
func refundAttemptIsActive(attempt *dbent.PaymentRefundAttempt) bool {
	if attempt == nil {
		return false
	}
	switch attempt.ProviderState {
	case refundProviderStateCalling, refundProviderStatePending, refundProviderStateUnknown:
		return true
	default:
		return false
	}
}

func (s *PaymentService) beginRefundAttempt(ctx context.Context, plan *RefundPlan, source string) (_ *dbent.PaymentRefundAttempt, _ *RefundResult, err error) {
	if plan == nil || plan.Order == nil {
		return nil, nil, infraerrors.BadRequest("INVALID_REFUND_PLAN", "invalid refund plan")
	}
	if source == "" {
		source = refundAttemptSourceAdmin
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin refund attempt: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	orderQuery := client.PaymentOrder.Query().Where(paymentorder.IDEQ(plan.OrderID))
	if paymentAuditDialect(client) == dialect.Postgres {
		orderQuery.ForUpdate()
	}
	order, err := orderQuery.Only(txCtx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil, infraerrors.NotFound("NOT_FOUND", "order not found")
		}
		return nil, nil, fmt.Errorf("lock refund order: %w", err)
	}
	if order.Status == OrderStatusPartiallyRefunded {
		return nil, nil, infraerrors.BadRequest("MULTIPLE_PARTIAL_REFUNDS_UNSUPPORTED", "additional refunds after a partial refund are not supported")
	}
	if order.Status == OrderStatusRefundPending || order.Status == OrderStatusRefunding {
		return nil, nil, infraerrors.Conflict("REFUND_IN_PROGRESS", "an existing refund attempt must be reconciled before another refund")
	}
	if order.Status == OrderStatusRefunded {
		return nil, &RefundResult{Success: true}, nil
	}
	if order.Status != OrderStatusCompleted && order.Status != OrderStatusRefundRequested && order.Status != OrderStatusRefundFailed {
		return nil, nil, infraerrors.BadRequest("INVALID_STATUS", "order status does not allow refund")
	}

	previous, err := s.latestRefundAttempt(txCtx, client, order.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("load previous refund attempt: %w", err)
	}
	if previous != nil && !refundAttemptAllowsRetry(previous) {
		return nil, nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "the existing refund attempt requires reconciliation before another refund")
	}
	if order.Status == OrderStatusRefundFailed && previous == nil && source != refundAttemptSourcePurchaseLimitRejected {
		return nil, nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "legacy failed refund has no persisted deduction state and must be reviewed manually")
	}
	if !plan.Force && !plan.DeductBalance {
		return nil, nil, infraerrors.BadRequest("DEDUCTION_REQUIRED", "non-forced refunds must deduct the refunded entitlement")
	}

	userQuery := client.User.Query().Where(user.IDEQ(order.UserID))
	if paymentAuditDialect(client) == dialect.Postgres {
		userQuery.ForUpdate()
	}
	lockedUser, err := userQuery.Only(txCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("lock refund user: %w", err)
	}

	attemptID := strings.TrimSpace(plan.AttemptID)
	if attemptID == "" {
		attemptID = uuid.NewString()
	}
	deductionType := payment.DeductionTypeNone
	deductionState := refundDeductionStateNone
	heldBalance := 0.0
	var subscriptionID *int64
	subscriptionDays := 0
	var subscriptionOriginalExpiresAt *time.Time
	var subscriptionOriginalStatus *string
	subscriptionRevoked := false
	var subscriptionGroupID int64

	if plan.DeductBalance && source != refundAttemptSourcePurchaseLimitRejected {
		if order.OrderType == payment.OrderTypeSubscription {
			deductionType = payment.DeductionTypeSubscription
			if order.SubscriptionGroupID == nil || order.SubscriptionDays == nil || *order.SubscriptionDays <= 0 {
				if !plan.Force {
					return nil, &RefundResult{Success: false, Warning: "cannot identify the subscription entitlement to hold; use force", RequireForce: true}, nil
				}
			} else {
				subscriptionGroupID = *order.SubscriptionGroupID
				subQuery := client.UserSubscription.Query().Where(
					usersubscription.UserIDEQ(order.UserID),
					usersubscription.GroupIDEQ(subscriptionGroupID),
					usersubscription.StatusEQ(SubscriptionStatusActive),
					usersubscription.ExpiresAtGT(time.Now()),
				)
				if paymentAuditDialect(client) == dialect.Postgres {
					subQuery.ForUpdate()
				}
				sub, subErr := subQuery.Only(txCtx)
				if subErr != nil {
					if !dbent.IsNotFound(subErr) {
						return nil, nil, fmt.Errorf("lock refund subscription: %w", subErr)
					}
					if !plan.Force {
						return nil, &RefundResult{Success: false, Warning: "cannot find an active subscription to hold; use force", RequireForce: true}, nil
					}
				} else {
					subscriptionID = &sub.ID
					subscriptionDays = *order.SubscriptionDays
					originalExpiresAt := sub.ExpiresAt
					originalStatus := sub.Status
					subscriptionOriginalExpiresAt = &originalExpiresAt
					subscriptionOriginalStatus = &originalStatus
					newExpiresAt := sub.ExpiresAt.AddDate(0, 0, -subscriptionDays)
					update := client.UserSubscription.UpdateOneID(sub.ID)
					if !newExpiresAt.After(time.Now()) {
						update.SetDeletedAt(time.Now())
						subscriptionRevoked = true
					} else {
						update.SetExpiresAt(newExpiresAt)
					}
					if _, err = update.Save(txCtx); err != nil {
						return nil, nil, fmt.Errorf("hold refund subscription: %w", err)
					}
					deductionState = refundDeductionStateHeld
				}
			}
		} else {
			deductionType = payment.DeductionTypeBalance
			available := math.Max(0, lockedUser.Balance)
			if !plan.Force && available < plan.RefundAmount {
				return nil, &RefundResult{Success: false, Warning: "user balance is insufficient for an atomic refund hold; use force", RequireForce: true}, nil
			}
			heldBalance = math.Min(plan.RefundAmount, available)
			if !plan.Force {
				heldBalance = plan.RefundAmount
			}
			if heldBalance > 0 {
				if _, err = client.User.UpdateOneID(order.UserID).AddBalance(-heldBalance).Save(txCtx); err != nil {
					return nil, nil, fmt.Errorf("hold refund balance: %w", err)
				}
				deductionState = refundDeductionStateHeld
			}
		}
	}

	attempt, err := client.PaymentRefundAttempt.Create().
		SetAttemptID(attemptID).
		SetOrderID(order.ID).
		SetRefundAmount(plan.RefundAmount).
		SetGatewayAmount(plan.GatewayAmount).
		SetReason(plan.Reason).
		SetOriginalOrderStatus(order.Status).
		SetSource(source).
		SetDeductBalance(plan.DeductBalance).
		SetForce(plan.Force).
		SetDeductionType(deductionType).
		SetHeldBalanceAmount(heldBalance).
		SetNillableSubscriptionID(subscriptionID).
		SetSubscriptionDays(subscriptionDays).
		SetNillableSubscriptionOriginalExpiresAt(subscriptionOriginalExpiresAt).
		SetNillableSubscriptionOriginalStatus(subscriptionOriginalStatus).
		SetSubscriptionRevoked(subscriptionRevoked).
		SetDeductionState(deductionState).
		SetProviderState(refundProviderStateCalling).
		SetProviderResult("").
		SetManualReview(false).
		Save(txCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("persist refund attempt: %w", err)
	}

	_, err = client.PaymentOrder.UpdateOneID(order.ID).
		SetStatus(OrderStatusRefunding).
		SetRefundAmount(plan.RefundAmount).
		SetRefundReason(plan.Reason).
		SetForceRefund(plan.Force).
		ClearRefundAt().
		ClearFailedAt().
		ClearFailedReason().
		Save(txCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("mark refund calling: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit refund attempt: %w", err)
	}

	plan.AttemptID = attempt.AttemptID
	plan.Order = order
	plan.DeductionType = attempt.DeductionType
	plan.BalanceToDeduct = attempt.HeldBalanceAmount
	plan.SubDaysToDeduct = attempt.SubscriptionDays
	if attempt.SubscriptionID != nil {
		plan.SubscriptionID = *attempt.SubscriptionID
	}
	if subscriptionID != nil {
		s.invalidateRefundSubscription(order.UserID, subscriptionGroupID)
	}
	s.writeAuditLog(ctx, order.ID, "REFUND_ATTEMPT_STARTED", "admin", map[string]any{
		"attemptID":        attempt.AttemptID,
		"source":           source,
		"refundAmount":     attempt.RefundAmount,
		"deductBalance":    attempt.DeductBalance,
		"force":            attempt.Force,
		"deductionType":    attempt.DeductionType,
		"heldBalance":      attempt.HeldBalanceAmount,
		"subscriptionDays": attempt.SubscriptionDays,
	})
	return attempt, nil, nil
}

func (s *PaymentService) createPurchaseLimitRejectedAttempt(ctx context.Context, order *dbent.PaymentOrder, originalStatus, reason string) (_ *dbent.PaymentRefundAttempt, _ *RefundPlan, _ bool, err error) {
	if order == nil {
		return nil, nil, false, fmt.Errorf("nil rejected product refund order")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, nil, false, fmt.Errorf("begin rejected product refund attempt: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	orderQuery := client.PaymentOrder.Query().Where(paymentorder.IDEQ(order.ID))
	if paymentAuditDialect(client) == dialect.Postgres {
		orderQuery.ForUpdate()
	}
	lockedOrder, err := orderQuery.Only(txCtx)
	if err != nil {
		return nil, nil, false, fmt.Errorf("lock rejected product refund order: %w", err)
	}
	if lockedOrder.Status != OrderStatusRefunding {
		return nil, nil, false, infraerrors.Conflict("CONFLICT", "rejected product refund order status changed")
	}
	previous, err := s.latestRefundAttempt(txCtx, client, order.ID)
	if err != nil {
		return nil, nil, false, fmt.Errorf("load rejected product refund attempt: %w", err)
	}
	if previous != nil {
		return previous, s.refundPlanFromAttempt(lockedOrder, previous), false, nil
	}
	userQuery := client.User.Query().Where(user.IDEQ(lockedOrder.UserID))
	if paymentAuditDialect(client) == dialect.Postgres {
		userQuery.ForUpdate()
	}
	if _, err = userQuery.Only(txCtx); err != nil {
		return nil, nil, false, fmt.Errorf("lock rejected product refund user: %w", err)
	}
	if strings.TrimSpace(originalStatus) == "" {
		originalStatus = OrderStatusFailed
	}
	gatewayAmount := calculateGatewayRefundAmount(lockedOrder.Amount, lockedOrder.PayAmount, lockedOrder.Amount, PaymentOrderCurrency(lockedOrder))
	attempt, err := client.PaymentRefundAttempt.Create().
		SetAttemptID(uuid.NewString()).
		SetOrderID(lockedOrder.ID).
		SetRefundAmount(lockedOrder.Amount).
		SetGatewayAmount(gatewayAmount).
		SetReason(reason).
		SetOriginalOrderStatus(originalStatus).
		SetSource(refundAttemptSourcePurchaseLimitRejected).
		SetDeductBalance(false).
		SetForce(true).
		SetDeductionType(payment.DeductionTypeNone).
		SetHeldBalanceAmount(0).
		SetSubscriptionDays(0).
		SetSubscriptionRevoked(false).
		SetDeductionState(refundDeductionStateNone).
		SetProviderState(refundProviderStateCalling).
		SetProviderResult("").
		SetManualReview(false).
		Save(txCtx)
	if err != nil {
		return nil, nil, false, fmt.Errorf("persist rejected product refund attempt: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, false, fmt.Errorf("commit rejected product refund attempt: %w", err)
	}
	plan := s.refundPlanFromAttempt(lockedOrder, attempt)
	s.writeAuditLog(ctx, lockedOrder.ID, "REFUND_ATTEMPT_STARTED", "system", map[string]any{
		"attemptID":     attempt.AttemptID,
		"source":        attempt.Source,
		"refundAmount":  attempt.RefundAmount,
		"deductBalance": false,
		"force":         true,
	})
	return attempt, plan, true, nil
}

func (s *PaymentService) refundPlanFromAttempt(order *dbent.PaymentOrder, attempt *dbent.PaymentRefundAttempt) *RefundPlan {
	plan := &RefundPlan{
		AttemptID:       attempt.AttemptID,
		OrderID:         order.ID,
		Order:           order,
		RefundAmount:    attempt.RefundAmount,
		GatewayAmount:   attempt.GatewayAmount,
		Reason:          attempt.Reason,
		Force:           attempt.Force,
		DeductBalance:   attempt.DeductBalance,
		DeductionType:   attempt.DeductionType,
		BalanceToDeduct: attempt.HeldBalanceAmount,
		SubDaysToDeduct: attempt.SubscriptionDays,
	}
	if attempt.SubscriptionID != nil {
		plan.SubscriptionID = *attempt.SubscriptionID
	}
	return plan
}

func (s *PaymentService) markRefundPending(ctx context.Context, plan *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	return s.updateRefundAttemptNonterminal(ctx, plan, refundProviderStatePending, false, resp, "gateway refund is pending confirmation")
}

func (s *PaymentService) markRefundUnknown(ctx context.Context, plan *RefundPlan, resp *payment.RefundResponse, cause error) (*RefundResult, error) {
	detail := psErrMsg(cause)
	if detail == "" {
		detail = "provider returned an unknown refund result"
	}
	return s.updateRefundAttemptNonterminal(ctx, plan, refundProviderStateUnknown, true, resp, "gateway refund result is unknown; manual verification is required: "+detail)
}

func (s *PaymentService) markRefundManualReview(ctx context.Context, plan *RefundPlan, detail string) (*RefundResult, error) {
	return s.updateRefundAttemptNonterminal(ctx, plan, "", true, nil, detail)
}

func (s *PaymentService) updateRefundAttemptNonterminal(ctx context.Context, plan *RefundPlan, providerState string, manualReview bool, resp *payment.RefundResponse, warning string) (_ *RefundResult, err error) {
	if plan == nil || strings.TrimSpace(plan.AttemptID) == "" {
		return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "refund attempt state is missing")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refund state update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	orderQuery := client.PaymentOrder.Query().Where(paymentorder.IDEQ(plan.OrderID))
	if paymentAuditDialect(client) == dialect.Postgres {
		orderQuery.ForUpdate()
	}
	order, err := orderQuery.Only(txCtx)
	if err != nil {
		return nil, fmt.Errorf("lock refund order: %w", err)
	}
	attemptQuery := client.PaymentRefundAttempt.Query().Where(
		paymentrefundattempt.AttemptIDEQ(plan.AttemptID),
		paymentrefundattempt.OrderIDEQ(plan.OrderID),
	)
	if paymentAuditDialect(client) == dialect.Postgres {
		attemptQuery.ForUpdate()
	}
	attempt, err := attemptQuery.Only(txCtx)
	if err != nil {
		return nil, fmt.Errorf("lock refund attempt: %w", err)
	}
	if attempt.ProviderState == refundProviderStateSucceeded {
		return &RefundResult{Success: true, BalanceDeducted: attempt.HeldBalanceAmount, SubDaysDeducted: attempt.SubscriptionDays}, nil
	}
	if attempt.ProviderState == refundProviderStateFailed || attempt.ProviderState == refundProviderStateCanceled {
		return &RefundResult{Success: false, Warning: "refund attempt is already terminal"}, nil
	}
	if order.Status != OrderStatusRefunding && order.Status != OrderStatusRefundPending {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed while updating refund attempt")
	}
	update := client.PaymentRefundAttempt.UpdateOneID(attempt.ID).
		SetManualReview(manualReview)
	if providerState != "" {
		update.SetProviderState(providerState)
	}
	if resp != nil {
		if refundID := refundResponseID(resp); refundID != "" {
			update.SetProviderRefundID(refundID)
		}
		update.SetProviderResult(strings.TrimSpace(resp.Status))
	} else if warning != "" {
		update.SetProviderResult(warning)
	}
	if _, err = update.Save(txCtx); err != nil {
		return nil, fmt.Errorf("update refund attempt: %w", err)
	}
	if _, err = client.PaymentOrder.UpdateOneID(order.ID).
		SetStatus(OrderStatusRefundPending).
		SetRefundAmount(attempt.RefundAmount).
		SetRefundReason(attempt.Reason).
		SetForceRefund(attempt.Force).
		ClearRefundAt().
		ClearFailedAt().
		ClearFailedReason().
		Save(txCtx); err != nil {
		return nil, fmt.Errorf("mark refund pending: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refund state update: %w", err)
	}
	action := "REFUND_PENDING"
	if manualReview {
		action = "REFUND_UNKNOWN"
	}
	s.writeAuditLog(ctx, order.ID, action, refundAttemptOperator(attempt), map[string]any{
		"attemptID":        attempt.AttemptID,
		"refundID":         refundResponseID(resp),
		"providerState":    providerState,
		"manualReview":     manualReview,
		"deductionState":   attempt.DeductionState,
		"heldBalance":      attempt.HeldBalanceAmount,
		"subscriptionDays": attempt.SubscriptionDays,
	})
	return &RefundResult{Success: false, Warning: warning}, nil
}

func (s *PaymentService) finalizeRefundSuccess(ctx context.Context, plan *RefundPlan, resp *payment.RefundResponse) (_ *RefundResult, err error) {
	if plan == nil || strings.TrimSpace(plan.AttemptID) == "" {
		return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "refund attempt state is missing")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refund success: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	orderQuery := client.PaymentOrder.Query().Where(paymentorder.IDEQ(plan.OrderID))
	if paymentAuditDialect(client) == dialect.Postgres {
		orderQuery.ForUpdate()
	}
	order, err := orderQuery.Only(txCtx)
	if err != nil {
		return nil, fmt.Errorf("lock refund order: %w", err)
	}
	attemptQuery := client.PaymentRefundAttempt.Query().Where(
		paymentrefundattempt.AttemptIDEQ(plan.AttemptID),
		paymentrefundattempt.OrderIDEQ(plan.OrderID),
	)
	if paymentAuditDialect(client) == dialect.Postgres {
		attemptQuery.ForUpdate()
	}
	attempt, err := attemptQuery.Only(txCtx)
	if err != nil {
		return nil, fmt.Errorf("lock refund attempt: %w", err)
	}
	if attempt.ProviderState == refundProviderStateSucceeded && (attempt.DeductionState == refundDeductionStateConsumed || attempt.DeductionState == refundDeductionStateNone) {
		return &RefundResult{Success: true, BalanceDeducted: attempt.HeldBalanceAmount, SubDaysDeducted: attempt.SubscriptionDays}, nil
	}
	if attempt.DeductionState == refundDeductionStateReturned {
		return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "provider success arrived after the local hold was returned")
	}
	if order.Status != OrderStatusRefunding && order.Status != OrderStatusRefundPending {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed while finalizing refund")
	}

	finalStatus := OrderStatusRefunded
	if attempt.RefundAmount < order.Amount-paymentAmountToleranceForCurrency(PaymentOrderCurrency(order)) {
		finalStatus = OrderStatusPartiallyRefunded
	}
	attemptUpdate := client.PaymentRefundAttempt.UpdateOneID(attempt.ID).
		SetProviderState(refundProviderStateSucceeded).
		SetProviderResult(strings.TrimSpace(resp.Status)).
		SetManualReview(false)
	if refundID := refundResponseID(resp); refundID != "" {
		attemptUpdate.SetProviderRefundID(refundID)
	}
	if attempt.DeductionState == refundDeductionStateHeld {
		attemptUpdate.SetDeductionState(refundDeductionStateConsumed)
	}
	if _, err = attemptUpdate.Save(txCtx); err != nil {
		return nil, fmt.Errorf("consume refund hold: %w", err)
	}
	if _, err = client.PaymentOrder.UpdateOneID(order.ID).
		SetStatus(finalStatus).
		SetRefundAmount(attempt.RefundAmount).
		SetRefundReason(attempt.Reason).
		SetRefundAt(time.Now()).
		SetForceRefund(attempt.Force).
		ClearFailedAt().
		ClearFailedReason().
		Save(txCtx); err != nil {
		return nil, fmt.Errorf("mark refund success: %w", err)
	}
	if finalStatus == OrderStatusRefunded && attempt.Source != refundAttemptSourcePurchaseLimitRejected {
		if err = releaseConsumedPurchaseTx(txCtx, tx, order.ID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refund success: %w", err)
	}
	s.writeAuditLog(ctx, order.ID, "REFUND_SUCCESS", refundAttemptOperator(attempt), map[string]any{
		"attemptID":       attempt.AttemptID,
		"refundID":        refundResponseID(resp),
		"refundAmount":    attempt.RefundAmount,
		"balanceDeducted": attempt.HeldBalanceAmount,
		"subDaysDeducted": attempt.SubscriptionDays,
		"force":           attempt.Force,
		"deductBalance":   attempt.DeductBalance,
	})
	return &RefundResult{Success: true, BalanceDeducted: attempt.HeldBalanceAmount, SubDaysDeducted: attempt.SubscriptionDays}, nil
}

func (s *PaymentService) finalizeRefundReturned(ctx context.Context, plan *RefundPlan, resp *payment.RefundResponse, providerState, detail string) (_ *RefundResult, err error) {
	if plan == nil || strings.TrimSpace(plan.AttemptID) == "" {
		return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "refund attempt state is missing")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refund return: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	orderQuery := client.PaymentOrder.Query().Where(paymentorder.IDEQ(plan.OrderID))
	if paymentAuditDialect(client) == dialect.Postgres {
		orderQuery.ForUpdate()
	}
	order, err := orderQuery.Only(txCtx)
	if err != nil {
		return nil, fmt.Errorf("lock refund order: %w", err)
	}
	attemptSnapshot, err := client.PaymentRefundAttempt.Query().Where(
		paymentrefundattempt.AttemptIDEQ(plan.AttemptID),
		paymentrefundattempt.OrderIDEQ(plan.OrderID),
	).Only(txCtx)
	if err != nil {
		return nil, fmt.Errorf("load refund attempt: %w", err)
	}
	userQuery := client.User.Query().Where(user.IDEQ(order.UserID))
	if paymentAuditDialect(client) == dialect.Postgres {
		userQuery.ForUpdate()
	}
	if _, err = userQuery.Only(txCtx); err != nil {
		return nil, fmt.Errorf("lock refund user: %w", err)
	}
	attemptQuery := client.PaymentRefundAttempt.Query().Where(paymentrefundattempt.IDEQ(attemptSnapshot.ID))
	if paymentAuditDialect(client) == dialect.Postgres {
		attemptQuery.ForUpdate()
	}
	attempt, err := attemptQuery.Only(txCtx)
	if err != nil {
		return nil, fmt.Errorf("lock refund attempt: %w", err)
	}
	if (attempt.ProviderState == refundProviderStateFailed || attempt.ProviderState == refundProviderStateCanceled) && (attempt.DeductionState == refundDeductionStateReturned || attempt.DeductionState == refundDeductionStateNone) {
		return &RefundResult{Success: false, Warning: detail}, nil
	}
	if attempt.DeductionState == refundDeductionStateConsumed {
		return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "cannot return a consumed refund hold")
	}
	if order.Status != OrderStatusRefunding && order.Status != OrderStatusRefundPending && order.Status != OrderStatusRefundFailed {
		return nil, infraerrors.Conflict("CONFLICT", "order status changed while returning refund hold")
	}

	if attempt.DeductionState == refundDeductionStateHeld {
		switch attempt.DeductionType {
		case payment.DeductionTypeBalance:
			if attempt.HeldBalanceAmount > 0 {
				if _, err = client.User.UpdateOneID(order.UserID).AddBalance(attempt.HeldBalanceAmount).Save(txCtx); err != nil {
					return nil, fmt.Errorf("return refund balance hold: %w", err)
				}
			}
		case payment.DeductionTypeSubscription:
			if attempt.SubscriptionID == nil || attempt.SubscriptionOriginalExpiresAt == nil || attempt.SubscriptionOriginalStatus == nil {
				return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "subscription refund hold is missing restoration metadata")
			}
			subCtx := mixins.SkipSoftDelete(txCtx)
			subQuery := client.UserSubscription.Query().Where(usersubscription.IDEQ(*attempt.SubscriptionID))
			if paymentAuditDialect(client) == dialect.Postgres {
				subQuery.ForUpdate()
			}
			if _, err = subQuery.Only(subCtx); err != nil {
				return nil, fmt.Errorf("lock refund subscription for restore: %w", err)
			}
			if _, err = client.UserSubscription.UpdateOneID(*attempt.SubscriptionID).
				SetExpiresAt(*attempt.SubscriptionOriginalExpiresAt).
				SetStatus(*attempt.SubscriptionOriginalStatus).
				ClearDeletedAt().
				Save(subCtx); err != nil {
				return nil, fmt.Errorf("restore refund subscription hold: %w", err)
			}
		}
	}
	attemptUpdate := client.PaymentRefundAttempt.UpdateOneID(attempt.ID).
		SetProviderState(providerState).
		SetProviderResult(detail).
		SetManualReview(false)
	if refundID := refundResponseID(resp); refundID != "" {
		attemptUpdate.SetProviderRefundID(refundID)
	}
	if attempt.DeductionState == refundDeductionStateHeld {
		attemptUpdate.SetDeductionState(refundDeductionStateReturned)
	}
	if _, err = attemptUpdate.Save(txCtx); err != nil {
		return nil, fmt.Errorf("mark refund hold returned: %w", err)
	}
	if _, err = client.PaymentOrder.UpdateOneID(order.ID).
		SetStatus(OrderStatusRefundFailed).
		SetFailedAt(time.Now()).
		SetFailedReason(detail).
		ClearRefundAt().
		Save(txCtx); err != nil {
		return nil, fmt.Errorf("mark refund failed: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refund return: %w", err)
	}
	if attempt.SubscriptionID != nil && order.SubscriptionGroupID != nil {
		s.invalidateRefundSubscription(order.UserID, *order.SubscriptionGroupID)
	}
	action := "REFUND_FAILED"
	if providerState == refundProviderStateCanceled {
		action = "REFUND_CANCELED"
	}
	finalDeductionState := attempt.DeductionState
	if finalDeductionState == refundDeductionStateHeld {
		finalDeductionState = refundDeductionStateReturned
	}
	s.writeAuditLog(ctx, order.ID, action, refundAttemptOperator(attempt), map[string]any{
		"attemptID":       attempt.AttemptID,
		"refundID":        refundResponseID(resp),
		"providerState":   providerState,
		"deductionState":  finalDeductionState,
		"balanceReturned": attempt.HeldBalanceAmount,
		"subDaysReturned": attempt.SubscriptionDays,
		"detail":          detail,
	})
	return &RefundResult{Success: false, Warning: detail}, nil
}

func refundAttemptOperator(attempt *dbent.PaymentRefundAttempt) string {
	if attempt != nil && attempt.Source == refundAttemptSourcePurchaseLimitRejected {
		return "system"
	}
	return "admin"
}

func (s *PaymentService) invalidateRefundSubscription(userID, groupID int64) {
	if s == nil || s.subscriptionSvc == nil || userID <= 0 || groupID <= 0 {
		return
	}
	if err := s.subscriptionSvc.invalidateSubscriptionCaches(userID, groupID); err != nil {
		slog.Warn("refund subscription cache invalidation failed", "userID", userID, "groupID", groupID, "error", err)
	}
}
