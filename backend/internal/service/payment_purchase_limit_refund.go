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

	inst, err := s.getRefundOrderProviderInstance(ctx, order)
	if err != nil || inst == nil || !inst.RefundEnabled || strings.TrimSpace(tradeNo) == "" {
		detail := "automatic refund is unavailable"
		if err != nil {
			detail = err.Error()
		}
		return s.failRejectedProductRefund(ctx, order.ID, detail)
	}
	prov, err := s.createProviderFromInstance(ctx, inst)
	if err != nil {
		return s.failRejectedProductRefund(ctx, order.ID, err.Error())
	}
	resp, err := prov.Refund(ctx, payment.RefundRequest{
		TradeNo: tradeNo,
		OrderID: order.OutTradeNo,
		Amount:  formatGatewayRefundAmount(paid, order),
		Reason:  reason,
	})
	if err != nil {
		return s.failRejectedProductRefund(ctx, order.ID, err.Error())
	}
	if err := validateRefundProviderResponse(resp); err != nil {
		return s.failRejectedProductRefund(ctx, order.ID, err.Error())
	}

	// Persist the provider result before the local terminal transition. A
	// repeated callback can then finish the database state without issuing a
	// second refund if this process stops between the two writes.
	recordErr := s.recordPurchaseLimitRefundResult(ctx, order.ID, resp)
	applyErr := s.applyRejectedProductRefundResult(ctx, order, reason, purchaseLimitRefundResult{
		Status:   strings.TrimSpace(resp.Status),
		RefundID: refundResponseID(resp),
	})
	if applyErr != nil {
		if recordErr != nil {
			return fmt.Errorf("%v; %w", recordErr, applyErr)
		}
		return applyErr
	}
	if recordErr != nil {
		slog.Error("record rejected product refund result after local finalization", "orderID", order.ID, "error", recordErr)
	}
	return nil
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
	result, found, err := s.latestPurchaseLimitRefundResult(ctx, order.ID)
	if err != nil {
		return true, fmt.Errorf("load rejected product refund result: %w", err)
	}
	if found {
		return true, s.applyRejectedProductRefundResult(ctx, order, psStringValue(order.RefundReason), result)
	}
	if time.Now().Before(order.UpdatedAt.Add(paymentFulfillmentLeaseDuration)) {
		// The original callback may still be waiting for the provider. ACK a
		// duplicate callback and let that in-flight attempt finish.
		return true, nil
	}
	detail := "automatic refund interrupted before the provider result was recorded; verify the provider before retrying"
	return true, s.failRejectedProductRefund(ctx, order.ID, detail)
}

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
