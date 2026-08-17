//go:build integration

package service

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestRefundNonForceAtomicHoldRejectsAfterConcurrentSpendPostgres(t *testing.T) {
	client, db, ctx := newPaymentLifecyclePostgresClient(t)
	user, err := client.User.Create().
		SetEmail("refund-concurrent-spend@example.com").
		SetPasswordHash("hash").
		SetUsername("refund-concurrent-spend").
		SetBalance(100).
		Save(ctx)
	require.NoError(t, err)
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeStripe).
		SetName("refund-concurrent-provider").
		SetConfig("{}").
		SetSupportedTypes(payment.TypeStripe).
		SetEnabled(true).
		SetRefundEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(100).
		SetPayAmount(100).
		SetFeeRate(0).
		SetRechargeCode("REFUND-CONCURRENT-SPEND").
		SetOutTradeNo("sub2_refund_concurrent_spend").
		SetPaymentType(payment.TypeStripe).
		SetPaymentTradeNo("pi_refund_concurrent_spend").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).
		Save(ctx)
	require.NoError(t, err)

	provider := &refundIntegrationProvider{}
	originalFactory := createPaymentProviderFromInstance
	createPaymentProviderFromInstance = func(string, string, map[string]string) (payment.Provider, error) { return provider, nil }
	t.Cleanup(func() { createPaymentProviderFromInstance = originalFactory })
	svc := &PaymentService{entClient: client, loadBalancer: refundIntegrationLoadBalancer{}}
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 100, "concurrent spend", false, true)
	require.NoError(t, err)
	require.Nil(t, early)

	spendTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = spendTx.Rollback() })
	_, err = spendTx.ExecContext(ctx, `UPDATE users SET balance = 20, updated_at = NOW() WHERE id = $1`, user.ID)
	require.NoError(t, err)

	type executeResult struct {
		result *RefundResult
		err    error
	}
	done := make(chan executeResult, 1)
	go func() {
		result, executeErr := svc.ExecuteRefund(ctx, plan)
		done <- executeResult{result: result, err: executeErr}
	}()
	waitForPostgresPaymentOrderLock(t, ctx, db, order.ID)
	require.NoError(t, spendTx.Commit())

	select {
	case outcome := <-done:
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		require.True(t, outcome.result.RequireForce)
	case <-ctx.Done():
		t.Fatalf("refund execution timed out: %v", ctx.Err())
	}
	require.Zero(t, provider.refundCalls.Load(), "provider must not be called when the atomic hold is short")
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	updatedUser, err := client.User.Get(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, 20.0, updatedUser.Balance)
}

type refundIntegrationLoadBalancer struct{}

func (refundIntegrationLoadBalancer) GetInstanceConfig(context.Context, int64) (map[string]string, error) {
	return map[string]string{}, nil
}
func (refundIntegrationLoadBalancer) SelectInstance(context.Context, string, payment.PaymentType, payment.Strategy, float64) (*payment.InstanceSelection, error) {
	return nil, fmt.Errorf("unexpected SelectInstance")
}

type refundIntegrationProvider struct {
	refundCalls atomic.Int32
}

func (*refundIntegrationProvider) Name() string        { return "refund-integration" }
func (*refundIntegrationProvider) ProviderKey() string { return payment.TypeStripe }
func (*refundIntegrationProvider) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeStripe}
}
func (*refundIntegrationProvider) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	return nil, fmt.Errorf("unexpected CreatePayment")
}
func (*refundIntegrationProvider) QueryOrder(context.Context, string) (*payment.QueryOrderResponse, error) {
	return nil, fmt.Errorf("unexpected QueryOrder")
}
func (*refundIntegrationProvider) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	return nil, fmt.Errorf("unexpected VerifyNotification")
}
func (p *refundIntegrationProvider) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	p.refundCalls.Add(1)
	return &payment.RefundResponse{RefundID: "rf_unexpected", Status: payment.ProviderStatusSuccess}, nil
}

var _ payment.LoadBalancer = refundIntegrationLoadBalancer{}
var _ payment.Provider = (*refundIntegrationProvider)(nil)
