package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/paymentproviderinstance"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

// --- Refund Flow ---

var createPaymentProviderFromInstance = provider.CreateProvider

// getOrderProviderInstance looks up the provider instance that processed this order.
// For legacy orders without provider_instance_id, it resolves only when the
// historical instance is uniquely identifiable from the stored order fields.
func (s *PaymentService) getOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return s.resolveUniqueLegacyOrderProviderInstance(ctx, o)
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, nil
	}
	return s.entClient.PaymentProviderInstance.Get(ctx, instID)
}

// getRefundOrderProviderInstance resolves the provider instance for refund paths.
// Refunds must be pinned to an explicit historical binding, so legacy
// "best-effort" provider guessing is intentionally not allowed here.
func (s *PaymentService) getRefundOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	if s == nil || s.entClient == nil || o == nil {
		return nil, nil
	}

	if snapshot := psOrderProviderSnapshot(o); snapshot != nil {
		return s.resolveSnapshotOrderProviderInstance(ctx, o, snapshot)
	}

	instIDStr := strings.TrimSpace(psStringValue(o.ProviderInstanceID))
	if instIDStr == "" {
		return nil, nil
	}

	instID, err := strconv.ParseInt(instIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("order %d refund provider instance id is invalid: %s", o.ID, instIDStr)
	}
	inst, err := s.entClient.PaymentProviderInstance.Get(ctx, instID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, fmt.Errorf("order %d refund provider instance %s is missing", o.ID, instIDStr)
		}
		return nil, err
	}
	return inst, nil
}

func (s *PaymentService) resolveUniqueLegacyOrderProviderInstance(ctx context.Context, o *dbent.PaymentOrder) (*dbent.PaymentProviderInstance, error) {
	paymentType := payment.GetBasePaymentType(strings.TrimSpace(o.PaymentType))
	providerKey := strings.TrimSpace(psStringValue(o.ProviderKey))
	if providerKey != "" {
		instances, err := s.entClient.PaymentProviderInstance.Query().
			Where(paymentproviderinstance.ProviderKeyEQ(providerKey)).
			All(ctx)
		if err != nil {
			return nil, err
		}
		matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
		if len(matched) == 1 {
			return matched[0], nil
		}
		return nil, nil
	}

	if paymentType == "" {
		return nil, nil
	}

	instances, err := s.entClient.PaymentProviderInstance.Query().
		All(ctx)
	if err != nil {
		return nil, err
	}

	matched := psFilterLegacyOrderProviderInstances(paymentType, instances)
	if len(matched) == 1 {
		return matched[0], nil
	}
	return nil, nil
}

func psFilterLegacyOrderProviderInstances(orderPaymentType string, instances []*dbent.PaymentProviderInstance) []*dbent.PaymentProviderInstance {
	if len(instances) == 0 {
		return nil
	}
	if strings.TrimSpace(orderPaymentType) == "" {
		return instances
	}
	var matched []*dbent.PaymentProviderInstance
	for _, inst := range instances {
		if psLegacyOrderMatchesInstance(orderPaymentType, inst) {
			matched = append(matched, inst)
		}
	}
	return matched
}

func psLegacyOrderMatchesInstance(orderPaymentType string, inst *dbent.PaymentProviderInstance) bool {
	if inst == nil {
		return false
	}

	baseType := payment.GetBasePaymentType(strings.TrimSpace(orderPaymentType))
	instanceProviderKey := strings.TrimSpace(inst.ProviderKey)
	if baseType == "" {
		return false
	}

	if baseType == payment.TypeStripe {
		return instanceProviderKey == payment.TypeStripe
	}
	if instanceProviderKey == payment.TypeStripe {
		return false
	}
	if instanceProviderKey == baseType {
		return true
	}
	return payment.InstanceSupportsType(inst.SupportedTypes, baseType)
}

func (s *PaymentService) RequestRefund(ctx context.Context, oid, uid int64, reason string) error {
	o, err := s.validateRefundRequest(ctx, oid, uid)
	if err != nil {
		return err
	}
	u, err := s.userRepo.GetByID(ctx, o.UserID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u.Balance < o.Amount {
		return infraerrors.BadRequest("BALANCE_NOT_ENOUGH", "refund amount exceeds balance")
	}
	nr := strings.TrimSpace(reason)
	now := time.Now()
	by := fmt.Sprintf("%d", uid)
	c, err := s.entClient.PaymentOrder.Update().Where(paymentorder.IDEQ(oid), paymentorder.UserIDEQ(uid), paymentorder.StatusEQ(OrderStatusCompleted), paymentorder.OrderTypeEQ(payment.OrderTypeBalance)).SetStatus(OrderStatusRefundRequested).SetRefundRequestedAt(now).SetRefundRequestReason(nr).SetRefundRequestedBy(by).SetRefundAmount(o.Amount).Save(ctx)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if c == 0 {
		return infraerrors.Conflict("CONFLICT", "order status changed")
	}
	s.writeAuditLog(ctx, oid, "REFUND_REQUESTED", fmt.Sprintf("user:%d", uid), map[string]any{"amount": o.Amount, "reason": nr})
	return nil
}

func (s *PaymentService) validateRefundRequest(ctx context.Context, oid, uid int64) (*dbent.PaymentOrder, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if o.UserID != uid {
		return nil, infraerrors.Forbidden("FORBIDDEN", "no permission")
	}
	if o.OrderType != payment.OrderTypeBalance {
		return nil, infraerrors.BadRequest("INVALID_ORDER_TYPE", "only balance orders can request refund")
	}
	if o.Status != OrderStatusCompleted {
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only completed orders can request refund")
	}
	// Check provider instance allows user refund
	inst, err := s.getRefundOrderProviderInstance(ctx, o)
	if err != nil || inst == nil {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.AllowUserRefund {
		return nil, infraerrors.Forbidden("USER_REFUND_DISABLED", "user refund is not enabled for this provider")
	}
	return o, nil
}

func (s *PaymentService) PrepareRefund(ctx context.Context, oid int64, amt float64, reason string, force, deduct bool) (*RefundPlan, *RefundResult, error) {
	o, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	rejectedPurchaseRefund := s.hasAuditLog(ctx, oid, purchaseLimitRejectedPaymentAudit)
	if o.Status == OrderStatusRefunding && rejectedPurchaseRefund {
		handled, recoverErr := s.recoverRejectedProductRefund(ctx, o)
		if recoverErr != nil {
			return nil, nil, recoverErr
		}
		if handled {
			o, err = s.entClient.PaymentOrder.Get(ctx, oid)
			if err != nil {
				return nil, nil, fmt.Errorf("reload recovered refund order: %w", err)
			}
		}
	}

	switch o.Status {
	case OrderStatusRefunded:
		return nil, &RefundResult{Success: true}, nil
	case OrderStatusRefundPending, OrderStatusRefunding:
		return nil, &RefundResult{Success: false, Warning: "an existing refund attempt must be queried or reviewed; no new provider request was sent"}, nil
	case OrderStatusPartiallyRefunded:
		return nil, nil, infraerrors.BadRequest("MULTIPLE_PARTIAL_REFUNDS_UNSUPPORTED", "additional refunds after a partial refund are not supported")
	case OrderStatusCompleted, OrderStatusRefundRequested:
		// Allowed. The order is locked and revalidated again when the hold is created.
	case OrderStatusRefundFailed:
		if !rejectedPurchaseRefund {
			attempt, loadErr := s.latestRefundAttempt(ctx, s.entClient, oid)
			if loadErr != nil {
				return nil, nil, fmt.Errorf("load previous refund attempt: %w", loadErr)
			}
			if !refundAttemptAllowsRetry(attempt) {
				return nil, nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "failed refund has no safely returned persisted hold and must be reviewed manually")
			}
		}
	default:
		return nil, nil, infraerrors.BadRequest("INVALID_STATUS", "order status does not allow refund")
	}

	inst, instErr := s.getRefundOrderProviderInstance(ctx, o)
	if instErr != nil {
		slog.Warn("refund: provider instance lookup failed", "orderID", oid, "error", instErr)
		return nil, nil, infraerrors.InternalServer("PROVIDER_LOOKUP_FAILED", "failed to look up payment provider for this order")
	}
	if inst == nil {
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not available for this order")
	}
	if !inst.RefundEnabled {
		return nil, nil, infraerrors.Forbidden("REFUND_DISABLED", "refund is not enabled for this provider")
	}
	if math.IsNaN(amt) || math.IsInf(amt, 0) {
		return nil, nil, infraerrors.BadRequest("INVALID_AMOUNT", "invalid refund amount")
	}
	if amt < 0 {
		return nil, nil, infraerrors.BadRequest("INVALID_AMOUNT", "invalid refund amount")
	}
	if amt == 0 {
		amt = o.Amount
	}
	orderCurrency := PaymentOrderCurrency(o)
	if amt-o.Amount > paymentAmountToleranceForCurrency(orderCurrency) {
		return nil, nil, infraerrors.BadRequest("REFUND_AMOUNT_EXCEEDED", "refund amount exceeds recharge")
	}
	if !force && !deduct && !rejectedPurchaseRefund {
		return nil, nil, infraerrors.BadRequest("DEDUCTION_REQUIRED", "non-forced refunds must deduct the refunded entitlement")
	}
	gatewayAmount := calculateGatewayRefundAmount(o.Amount, o.PayAmount, amt, orderCurrency)
	refundReason := strings.TrimSpace(reason)
	if refundReason == "" && o.RefundRequestReason != nil {
		refundReason = *o.RefundRequestReason
	}
	if refundReason == "" {
		refundReason = fmt.Sprintf("refund order:%d", o.ID)
	}
	plan := &RefundPlan{
		OrderID:       oid,
		Order:         o,
		RefundAmount:  amt,
		GatewayAmount: gatewayAmount,
		Reason:        refundReason,
		Force:         force,
		DeductBalance: deduct,
		DeductionType: payment.DeductionTypeNone,
	}
	if rejectedPurchaseRefund {
		plan.Force = true
		plan.DeductBalance = false
	}
	return plan, nil, nil
}

func (s *PaymentService) ExecuteRefund(ctx context.Context, plan *RefundPlan) (*RefundResult, error) {
	if plan == nil || plan.Order == nil {
		return nil, infraerrors.BadRequest("INVALID_REFUND_PLAN", "invalid refund plan")
	}
	var prov payment.Provider
	var err error
	if strings.TrimSpace(plan.Order.PaymentTradeNo) != "" {
		prov, err = s.prepareRefundProvider(ctx, plan.Order)
		if err != nil {
			return nil, err
		}
	}
	_, early, err := s.beginRefundAttempt(ctx, plan, refundAttemptSourceAdmin)
	if err != nil || early != nil {
		return early, err
	}
	if strings.TrimSpace(plan.Order.PaymentTradeNo) == "" {
		s.writeAuditLog(ctx, plan.OrderID, "REFUND_NO_TRADE_NO", "admin", map[string]any{"attemptID": plan.AttemptID, "detail": "skipped"})
		return s.finishRefund(ctx, plan, &payment.RefundResponse{Status: payment.ProviderStatusSuccess})
	}

	resp, callErr := s.callRefundProvider(ctx, plan, prov)
	if callErr != nil {
		if resp != nil && recognizedRefundProviderStatus(resp.Status) {
			return s.finishRefund(ctx, plan, resp)
		}
		return s.markRefundUnknown(ctx, plan, resp, callErr)
	}
	return s.finishRefund(ctx, plan, resp)
}

func (s *PaymentService) getRefundProvider(ctx context.Context, order *dbent.PaymentOrder) (payment.Provider, error) {
	inst, err := s.getRefundOrderProviderInstance(ctx, order)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, fmt.Errorf("refund provider instance is unavailable for order %d", order.ID)
	}
	return s.createProviderFromInstance(ctx, inst)
}

func (s *PaymentService) prepareRefundProvider(ctx context.Context, order *dbent.PaymentOrder) (payment.Provider, error) {
	prov, err := s.getRefundProvider(ctx, order)
	if err != nil {
		return nil, fmt.Errorf("get refund provider: %w", err)
	}
	if err := validateProviderSnapshotMetadata(order, prov.ProviderKey(), providerMerchantIdentityMetadata(prov)); err != nil {
		s.writeAuditLog(ctx, order.ID, "REFUND_PROVIDER_METADATA_MISMATCH", "admin", map[string]any{"detail": err.Error()})
		return nil, err
	}
	return prov, nil
}

//nolint:unused // 保留给旧支付网关兼容路径。
func (s *PaymentService) gwRefund(ctx context.Context, plan *RefundPlan) (*payment.RefundResponse, error) {
	if plan == nil || plan.Order == nil {
		return nil, infraerrors.BadRequest("INVALID_REFUND_PLAN", "invalid refund plan")
	}
	if strings.TrimSpace(plan.Order.PaymentTradeNo) == "" {
		return &payment.RefundResponse{Status: payment.ProviderStatusSuccess}, nil
	}
	prov, err := s.prepareRefundProvider(ctx, plan.Order)
	if err != nil {
		return nil, err
	}
	return s.callRefundProvider(ctx, plan, prov)
}

func (s *PaymentService) callRefundProvider(ctx context.Context, plan *RefundPlan, prov payment.Provider) (*payment.RefundResponse, error) {
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, err := prov.Refund(ctx, payment.RefundRequest{
		AttemptID: plan.AttemptID,
		TradeNo:   plan.Order.PaymentTradeNo,
		OrderID:   plan.Order.OutTradeNo,
		Amount:    formatGatewayRefundAmount(plan.GatewayAmount, plan.Order),
		Reason:    plan.Reason,
	})
	finishProviderCall()
	return resp, err
}

func formatGatewayRefundAmount(amount float64, order *dbent.PaymentOrder) string {
	return payment.FormatAmountForCurrency(amount, PaymentOrderCurrency(order))
}

func recognizedRefundProviderStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded, payment.ProviderStatusPending, payment.ProviderStatusFailed, payment.ProviderStatusCanceled:
		return true
	default:
		return false
	}
}

//nolint:unused // 由 unit 测试覆盖的响应校验辅助函数。
func validateRefundProviderResponse(resp *payment.RefundResponse) error {
	if resp == nil {
		return fmt.Errorf("payment refund response missing")
	}
	if !recognizedRefundProviderStatus(resp.Status) {
		return fmt.Errorf("payment refund returned unknown status: %s", strings.TrimSpace(resp.Status))
	}
	return nil
}

func (s *PaymentService) finishRefund(ctx context.Context, plan *RefundPlan, resp *payment.RefundResponse) (*RefundResult, error) {
	if resp == nil {
		return s.markRefundUnknown(ctx, plan, nil, fmt.Errorf("payment refund response missing"))
	}
	switch strings.TrimSpace(resp.Status) {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded:
		return s.finalizeRefundSuccess(ctx, plan, resp)
	case payment.ProviderStatusPending:
		return s.markRefundPending(ctx, plan, resp)
	case payment.ProviderStatusFailed:
		return s.finalizeRefundReturned(ctx, plan, resp, refundProviderStateFailed, "gateway refund failed")
	case payment.ProviderStatusCanceled:
		return s.finalizeRefundReturned(ctx, plan, resp, refundProviderStateCanceled, "gateway refund was canceled")
	default:
		return s.markRefundUnknown(ctx, plan, resp, fmt.Errorf("payment refund returned unknown status: %s", strings.TrimSpace(resp.Status)))
	}
}

func (s *PaymentService) QueryAndFinalizeRefund(ctx context.Context, oid int64) (*RefundResult, error) {
	order, err := s.entClient.PaymentOrder.Get(ctx, oid)
	if err != nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if s.hasAuditLog(ctx, oid, purchaseLimitRejectedPaymentAudit) {
		return s.queryAndFinalizeRejectedProductRefund(ctx, order)
	}
	switch order.Status {
	case OrderStatusRefunded:
		return &RefundResult{Success: true}, nil
	case OrderStatusPartiallyRefunded:
		return nil, infraerrors.BadRequest("MULTIPLE_PARTIAL_REFUNDS_UNSUPPORTED", "additional refunds after a partial refund are not supported")
	case OrderStatusRefundPending, OrderStatusRefunding:
		// Query the persisted attempt below.
	case OrderStatusRefundFailed:
		attempt, loadErr := s.latestRefundAttempt(ctx, s.entClient, oid)
		if loadErr != nil {
			return nil, fmt.Errorf("load refund attempt: %w", loadErr)
		}
		if attempt == nil {
			return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "legacy failed refund has no persisted attempt state")
		}
		return &RefundResult{Success: false, Warning: attempt.ProviderResult}, nil
	default:
		return nil, infraerrors.BadRequest("INVALID_STATUS", "only pending or in-progress refund attempts can be finalized")
	}

	attempt, err := s.latestRefundAttempt(ctx, s.entClient, oid)
	if err != nil {
		return nil, fmt.Errorf("load refund attempt: %w", err)
	}
	if attempt == nil {
		s.writeAuditLog(ctx, oid, "REFUND_LEGACY_MANUAL_REVIEW", "admin", map[string]any{"status": order.Status})
		return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "legacy pending refund has no persisted hold state and must be reviewed manually")
	}
	plan := s.refundPlanFromAttempt(order, attempt)
	if attempt.ProviderState == refundProviderStateFailed || attempt.ProviderState == refundProviderStateCanceled {
		return &RefundResult{Success: false, Warning: attempt.ProviderResult}, nil
	}
	if attempt.ProviderState == refundProviderStateSucceeded {
		return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "refund attempt and order terminal states are inconsistent")
	}

	prov, err := s.prepareRefundProvider(ctx, order)
	if err != nil {
		return nil, err
	}
	queryProvider, ok := prov.(payment.RefundQueryProvider)
	if !ok {
		return s.markRefundManualReview(ctx, plan, "this payment provider cannot query refund status; verify the persisted attempt manually")
	}
	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	resp, queryErr := queryProvider.QueryRefund(ctx, payment.RefundQueryRequest{
		AttemptID: attempt.AttemptID,
		TradeNo:   order.PaymentTradeNo,
		OrderID:   order.OutTradeNo,
		RefundID:  psStringValue(attempt.ProviderRefundID),
		Amount:    formatGatewayRefundAmount(attempt.GatewayAmount, order),
	})
	finishProviderCall()
	if queryErr != nil {
		return s.markRefundUnknown(ctx, plan, resp, fmt.Errorf("query refund: %w", queryErr))
	}
	return s.finishRefund(ctx, plan, resp)
}

func refundResponseID(resp *payment.RefundResponse) string {
	if resp == nil {
		return ""
	}
	return strings.TrimSpace(resp.RefundID)
}
