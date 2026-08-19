package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	purchaseLimitRejectedPaymentAudit = "PURCHASE_LIMIT_PAYMENT_REJECTED"
	purchaseLimitRefundResultAudit    = "PURCHASE_LIMIT_REFUND_RESULT"
)

// purchaseLimitRefundResult is persisted before the local order transition.
// It lets a later callback recover a successful/pending gateway refund without
// issuing a second external refund request when the process dies after the
// provider call but before the order update commits.
type purchaseLimitRefundResult struct {
	Status   string `json:"status"`
	RefundID string `json:"refundID,omitempty"`
}

func (s *PaymentService) recordPurchaseLimitRefundResult(ctx context.Context, orderID int64, resp *payment.RefundResponse) error {
	if resp == nil {
		return fmt.Errorf("missing purchase-limit refund response")
	}
	detail, err := json.Marshal(purchaseLimitRefundResult{
		Status:   strings.TrimSpace(resp.Status),
		RefundID: strings.TrimSpace(resp.RefundID),
	})
	if err != nil {
		return fmt.Errorf("encode purchase-limit refund result: %w", err)
	}
	_, err = s.entClient.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(orderID, 10)).
		SetAction(purchaseLimitRefundResultAudit).
		SetDetail(string(detail)).
		SetOperator("system").
		Save(ctx)
	if err != nil {
		return fmt.Errorf("record purchase-limit refund result: %w", err)
	}
	return nil
}

func (s *PaymentService) latestPurchaseLimitRefundResult(ctx context.Context, orderID int64) (purchaseLimitRefundResult, bool, error) {
	entry, err := s.entClient.PaymentAuditLog.Query().
		Where(
			paymentauditlog.OrderIDEQ(strconv.FormatInt(orderID, 10)),
			paymentauditlog.ActionEQ(purchaseLimitRefundResultAudit),
		).
		Order(paymentauditlog.ByCreatedAt(sql.OrderDesc())).
		First(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return purchaseLimitRefundResult{}, false, nil
		}
		return purchaseLimitRefundResult{}, false, err
	}
	var result purchaseLimitRefundResult
	if err := json.Unmarshal([]byte(entry.Detail), &result); err != nil {
		return purchaseLimitRefundResult{}, false, fmt.Errorf("decode purchase-limit refund result: %w", err)
	}
	result.Status = strings.TrimSpace(result.Status)
	result.RefundID = strings.TrimSpace(result.RefundID)
	return result, result.Status != "", nil
}

// rejectLateProductPayment refunds a captured product payment that can no longer
// reclaim its released purchase slot. It never fulfills the product.
func (s *PaymentService) rejectLateProductPayment(ctx context.Context, order *dbent.PaymentOrder, tradeNo string, paid float64, providerKey, reason string) error {
	if order == nil {
		return nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "purchase limit no longer permits this order"
	}
	current, err := s.entClient.PaymentOrder.Get(ctx, order.ID)
	if err != nil {
		return fmt.Errorf("reload rejected product payment: %w", err)
	}
	if handled, recoverErr := s.recoverRejectedProductRefund(ctx, current); handled {
		return recoverErr
	}

	now := time.Now()
	claimed, err := s.entClient.PaymentOrder.Update().Where(
		paymentorder.IDEQ(order.ID),
		paymentorder.Or(
			paymentorder.StatusIn(OrderStatusCancelled, OrderStatusExpired),
			paymentorder.And(
				paymentorder.StatusEQ(OrderStatusFailed),
				paymentorder.PaidAtIsNil(),
			),
		),
	).
		SetStatus(OrderStatusRefunding).
		SetPayAmount(paid).
		SetPaymentTradeNo(tradeNo).
		SetPaidAt(now).
		SetRefundAmount(order.Amount).
		SetRefundReason(reason).
		SetForceRefund(true).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("claim rejected product payment refund: %w", err)
	}
	if claimed == 0 {
		return s.alreadyProcessed(ctx, order)
	}
	s.writeAuditLog(ctx, order.ID, purchaseLimitRejectedPaymentAudit, providerKey, map[string]any{
		"tradeNo":    tradeNo,
		"paidAmount": paid,
		"reason":     reason,
	})

	claimedOrder, err := s.entClient.PaymentOrder.Get(ctx, order.ID)
	if err != nil {
		return fmt.Errorf("reload claimed rejected product refund: %w", err)
	}
	_, plan, created, err := s.createPurchaseLimitRejectedAttempt(ctx, claimedOrder, current.Status, reason)
	if err != nil {
		return err
	}
	if !created {
		// A persisted attempt already owns this order. Never issue Refund again.
		return nil
	}
	if strings.TrimSpace(tradeNo) == "" {
		_, finishErr := s.finalizeRefundReturned(ctx, plan, nil, refundProviderStateFailed, "automatic refund is unavailable: missing provider trade number")
		return finishErr
	}
	prov, err := s.prepareRefundProvider(ctx, claimedOrder)
	if err != nil {
		_, finishErr := s.finalizeRefundReturned(ctx, plan, nil, refundProviderStateFailed, "automatic refund is unavailable: "+err.Error())
		return finishErr
	}
	resp, callErr := s.callRefundProvider(ctx, plan, prov)
	if callErr != nil && (resp == nil || !recognizedRefundProviderStatus(resp.Status)) {
		_, stateErr := s.markRefundUnknown(ctx, plan, resp, callErr)
		return stateErr
	}
	if resp == nil {
		_, stateErr := s.markRefundUnknown(ctx, plan, nil, fmt.Errorf("automatic refund response missing"))
		return stateErr
	}
	if recordErr := s.recordPurchaseLimitRefundResult(ctx, order.ID, resp); recordErr != nil {
		slog.Error("record rejected product refund result", "orderID", order.ID, "error", recordErr)
	}
	_, finishErr := s.finishRefund(ctx, plan, resp)
	return finishErr
}

func (s *PaymentService) applyRejectedProductRefundResult(ctx context.Context, order *dbent.PaymentOrder, reason string, result purchaseLimitRefundResult) error {
	if order == nil {
		return fmt.Errorf("nil rejected product refund order")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = strings.TrimSpace(psStringValue(order.RefundReason))
	}
	if reason == "" {
		reason = "purchase limit no longer permits this order"
	}

	switch strings.TrimSpace(result.Status) {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded:
		updated, err := s.entClient.PaymentOrder.Update().
			Where(paymentorder.IDEQ(order.ID), paymentorder.StatusEQ(OrderStatusRefunding)).
			SetStatus(OrderStatusRefunded).
			SetRefundAmount(order.Amount).
			SetRefundReason(reason).
			SetRefundAt(time.Now()).
			SetForceRefund(true).
			ClearFailedAt().
			ClearFailedReason().
			Save(ctx)
		if err != nil {
			return fmt.Errorf("complete rejected product payment refund: %w", err)
		}
		if updated == 0 {
			return s.verifyRejectedProductRefundStatus(ctx, order.ID, OrderStatusRefunded)
		}
		s.writeAuditLog(ctx, order.ID, "REFUND_SUCCESS", "system", map[string]any{
			"refundID":     result.RefundID,
			"refundAmount": order.Amount,
			"reason":       reason,
			"force":        true,
			"unfulfilled":  true,
		})
		return nil
	case payment.ProviderStatusPending:
		updated, err := s.entClient.PaymentOrder.Update().
			Where(paymentorder.IDEQ(order.ID), paymentorder.StatusEQ(OrderStatusRefunding)).
			SetStatus(OrderStatusRefundPending).
			SetRefundAmount(order.Amount).
			SetRefundReason(reason).
			SetForceRefund(true).
			ClearRefundAt().
			ClearFailedAt().
			ClearFailedReason().
			Save(ctx)
		if err != nil {
			return fmt.Errorf("mark rejected product payment refund pending: %w", err)
		}
		if updated == 0 {
			return s.verifyRejectedProductRefundStatus(ctx, order.ID, OrderStatusRefundPending)
		}
		s.writeAuditLog(ctx, order.ID, "REFUND_PENDING", "system", map[string]any{
			"refundID":            result.RefundID,
			"refundAmount":        order.Amount,
			"reason":              reason,
			"force":               true,
			"deductionRollbackOK": true,
			"unfulfilled":         true,
		})
		return nil
	case payment.ProviderStatusFailed:
		return s.markRejectedProductRefundFailed(ctx, order.ID, "automatic refund failed")
	default:
		return s.markRejectedProductRefundFailed(ctx, order.ID, "automatic refund returned unknown status: "+strings.TrimSpace(result.Status))
	}
}

func (s *PaymentService) verifyRejectedProductRefundStatus(ctx context.Context, orderID int64, expected string) error {
	current, err := s.entClient.PaymentOrder.Get(ctx, orderID)
	if err != nil {
		return fmt.Errorf("reload rejected product refund status: %w", err)
	}
	if current.Status == expected {
		return nil
	}
	return fmt.Errorf("rejected product refund status changed to %s while finalizing %s", current.Status, expected)
}

func (s *PaymentService) recoverRejectedProductRefund(ctx context.Context, order *dbent.PaymentOrder) (bool, error) {
	if order == nil || order.Status != OrderStatusRefunding || !s.hasAuditLog(ctx, order.ID, purchaseLimitRejectedPaymentAudit) {
		return false, nil
	}
	attempt, err := s.latestRefundAttempt(ctx, s.entClient, order.ID)
	if err != nil {
		return true, fmt.Errorf("load rejected product refund attempt: %w", err)
	}
	if attempt != nil {
		plan := s.refundPlanFromAttempt(order, attempt)
		switch attempt.ProviderState {
		case refundProviderStateCalling:
			if time.Now().Before(order.UpdatedAt.Add(paymentFulfillmentLeaseDuration)) {
				return true, nil
			}
			_, stateErr := s.markRefundUnknown(ctx, plan, nil, fmt.Errorf("automatic refund was interrupted after the persisted provider-call boundary"))
			return true, stateErr
		case refundProviderStatePending:
			_, stateErr := s.markRefundPending(ctx, plan, &payment.RefundResponse{RefundID: psStringValue(attempt.ProviderRefundID), Status: payment.ProviderStatusPending})
			return true, stateErr
		case refundProviderStateUnknown:
			_, stateErr := s.markRefundUnknown(ctx, plan, nil, fmt.Errorf("automatic refund result requires manual reconciliation"))
			return true, stateErr
		default:
			return true, nil
		}
	}

	result, found, err := s.latestPurchaseLimitRefundResult(ctx, order.ID)
	if err != nil {
		return true, fmt.Errorf("load rejected product refund result: %w", err)
	}
	if found {
		return true, s.applyRejectedProductRefundResult(ctx, order, psStringValue(order.RefundReason), result)
	}
	if time.Now().Before(order.UpdatedAt.Add(paymentFulfillmentLeaseDuration)) {
		return true, nil
	}
	detail := "legacy automatic refund was interrupted without persisted attempt state; verify the provider manually"
	updated, updateErr := s.entClient.PaymentOrder.Update().
		Where(paymentorder.IDEQ(order.ID), paymentorder.StatusEQ(OrderStatusRefunding)).
		SetStatus(OrderStatusRefundPending).
		SetFailedReason(detail).
		Save(ctx)
	if updateErr != nil {
		return true, fmt.Errorf("mark legacy rejected product refund for review: %w", updateErr)
	}
	if updated > 0 {
		s.writeAuditLog(ctx, order.ID, "REFUND_LEGACY_MANUAL_REVIEW", "system", map[string]any{"detail": detail, "unfulfilled": true})
	}
	return true, nil
}

//nolint:unused // 保留给旧拒付退款兼容路径。
func (s *PaymentService) failRejectedProductRefund(ctx context.Context, orderID int64, detail string) error {
	if err := s.recordPurchaseLimitRefundResult(ctx, orderID, &payment.RefundResponse{Status: payment.ProviderStatusFailed}); err != nil {
		slog.Error("record rejected product refund failure", "orderID", orderID, "error", err)
	}
	return s.markRejectedProductRefundFailed(ctx, orderID, detail)
}

func (s *PaymentService) markRejectedProductRefundFailed(ctx context.Context, orderID int64, detail string) error {
	now := time.Now()
	updated, err := s.entClient.PaymentOrder.Update().
		Where(paymentorder.IDEQ(orderID), paymentorder.StatusEQ(OrderStatusRefunding)).
		SetStatus(OrderStatusRefundFailed).
		SetFailedAt(now).
		SetFailedReason(detail).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("mark rejected product refund failed: %w", err)
	}
	if updated == 0 {
		if err := s.verifyRejectedProductRefundStatus(ctx, orderID, OrderStatusRefundFailed); err != nil {
			return err
		}
	}
	s.writeAuditLog(ctx, orderID, "REFUND_FAILED", "system", map[string]any{
		"detail":      detail,
		"unfulfilled": true,
	})
	return nil
}

func (s *PaymentService) queryAndFinalizeRejectedProductRefund(ctx context.Context, order *dbent.PaymentOrder) (*RefundResult, error) {
	if order == nil {
		return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if order.Status == OrderStatusRefunding {
		_, recoverErr := s.recoverRejectedProductRefund(ctx, order)
		if recoverErr != nil {
			return nil, recoverErr
		}
		var err error
		order, err = s.entClient.PaymentOrder.Get(ctx, order.ID)
		if err != nil {
			return nil, fmt.Errorf("reload rejected product refund: %w", err)
		}
	}
	if order.Status == OrderStatusRefunded {
		return &RefundResult{Success: true}, nil
	}

	attempt, err := s.latestRefundAttempt(ctx, s.entClient, order.ID)
	if err != nil {
		return nil, fmt.Errorf("load rejected product refund attempt: %w", err)
	}
	if attempt != nil {
		if order.Status == OrderStatusRefundFailed {
			return &RefundResult{Success: false, Warning: attempt.ProviderResult}, nil
		}
		if order.Status != OrderStatusRefundPending && order.Status != OrderStatusRefunding {
			return nil, infraerrors.BadRequest("INVALID_STATUS", "automatic refund is not pending reconciliation")
		}
		plan := s.refundPlanFromAttempt(order, attempt)
		prov, providerErr := s.prepareRefundProvider(ctx, order)
		if providerErr != nil {
			return nil, providerErr
		}
		queryProvider, ok := prov.(payment.RefundQueryProvider)
		if !ok {
			return s.markRefundManualReview(ctx, plan, "this payment provider cannot query the automatic refund; verify it manually")
		}
		resp, queryErr := queryProvider.QueryRefund(ctx, payment.RefundQueryRequest{
			AttemptID: attempt.AttemptID,
			TradeNo:   order.PaymentTradeNo,
			OrderID:   order.OutTradeNo,
			RefundID:  psStringValue(attempt.ProviderRefundID),
			Amount:    formatGatewayRefundAmount(attempt.GatewayAmount, order),
		})
		if queryErr != nil {
			return s.markRefundUnknown(ctx, plan, resp, fmt.Errorf("query automatic refund: %w", queryErr))
		}
		return s.finishRefund(ctx, plan, resp)
	}

	if order.Status != OrderStatusRefundPending {
		return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "legacy automatic refund has no persisted attempt state")
	}
	legacy, found, err := s.latestPurchaseLimitRefundResult(ctx, order.ID)
	if err != nil {
		return nil, fmt.Errorf("load legacy automatic refund result: %w", err)
	}
	if !found {
		return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "legacy automatic refund has no persisted provider result")
	}
	prov, err := s.getRefundProvider(ctx, order)
	if err != nil {
		return nil, fmt.Errorf("get legacy automatic refund provider: %w", err)
	}
	queryProvider, ok := prov.(payment.RefundQueryProvider)
	if !ok {
		return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "legacy automatic refund provider cannot be queried")
	}
	gatewayAmount := calculateGatewayRefundAmount(order.Amount, order.PayAmount, order.RefundAmount, PaymentOrderCurrency(order))
	resp, err := queryProvider.QueryRefund(ctx, payment.RefundQueryRequest{
		TradeNo:  order.PaymentTradeNo,
		OrderID:  order.OutTradeNo,
		RefundID: legacy.RefundID,
		Amount:   formatGatewayRefundAmount(gatewayAmount, order),
	})
	if err != nil {
		return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "legacy automatic refund query failed; verify it manually")
	}
	status := strings.TrimSpace(resp.Status)
	switch status {
	case payment.ProviderStatusSuccess, payment.ProviderStatusRefunded:
		if err := s.applyRejectedProductRefundResult(ctx, order, psStringValue(order.RefundReason), purchaseLimitRefundResult{Status: status, RefundID: refundResponseID(resp)}); err != nil {
			return nil, err
		}
		return &RefundResult{Success: true}, nil
	case payment.ProviderStatusPending:
		return &RefundResult{Success: false, Warning: "gateway refund is still pending confirmation"}, nil
	case payment.ProviderStatusFailed, payment.ProviderStatusCanceled:
		if err := s.markRejectedProductRefundFailed(ctx, order.ID, "automatic refund failed or was canceled"); err != nil {
			return nil, err
		}
		return &RefundResult{Success: false, Warning: "automatic refund failed or was canceled"}, nil
	default:
		return nil, infraerrors.Conflict("REFUND_MANUAL_REVIEW_REQUIRED", "legacy automatic refund returned an unknown status")
	}
}
