//go:build unit

package service

import (
	"context"
	"math"
	"sync"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type paymentCompensationMockLedger struct {
	mu      sync.Mutex
	states  map[int64]string
	effects map[int64]int
}

func newPaymentCompensationMockLedger(orders ...*dbent.PaymentOrder) *paymentCompensationMockLedger {
	ledger := &paymentCompensationMockLedger{
		states:  make(map[int64]string, len(orders)),
		effects: make(map[int64]int, len(orders)),
	}
	for _, order := range orders {
		if order != nil {
			ledger.states[order.ID] = OrderStatusPending
		}
	}
	return ledger
}

func (l *paymentCompensationMockLedger) applyPaid(_ context.Context, order *dbent.PaymentOrder) string {
	if order == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.states[order.ID] != OrderStatusPending {
		return ""
	}
	l.states[order.ID] = OrderStatusCompleted
	l.effects[order.ID]++
	return checkPaidResultAlreadyPaid
}

func (l *paymentCompensationMockLedger) effectCount(orderID int64) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.effects[orderID]
}

func TestReconcilePendingPaymentBatchCountsPaidAndSkipsNil(t *testing.T) {
	orders := []*dbent.PaymentOrder{{ID: 1}, nil, {ID: 2}, {ID: 3}}
	var calls []int64
	recovered := reconcilePendingPaymentBatch(context.Background(), orders, func(_ context.Context, order *dbent.PaymentOrder) string {
		calls = append(calls, order.ID)
		if order.ID == 1 || order.ID == 3 {
			return checkPaidResultAlreadyPaid
		}
		return ""
	})
	require.Equal(t, 2, recovered)
	require.Equal(t, []int64{1, 2, 3}, calls)
}

func TestReconcilePendingPaymentBatchStopsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	orders := []*dbent.PaymentOrder{{ID: 1}, {ID: 2}, {ID: 3}}
	var calls []int64
	recovered := reconcilePendingPaymentBatch(ctx, orders, func(_ context.Context, order *dbent.PaymentOrder) string {
		calls = append(calls, order.ID)
		cancel()
		return checkPaidResultAlreadyPaid
	})
	require.Equal(t, 1, recovered)
	require.Equal(t, []int64{1}, calls)
}

func TestReconcilePendingPaymentBatchPendingThenPaidAcrossSweeps(t *testing.T) {
	order := &dbent.PaymentOrder{ID: 21}
	ledger := newPaymentCompensationMockLedger(order)
	providerStatus := payment.ProviderStatusPending
	reconcile := func(ctx context.Context, order *dbent.PaymentOrder) string {
		if providerStatus != payment.ProviderStatusPaid {
			return ""
		}
		return ledger.applyPaid(ctx, order)
	}

	require.Zero(t, reconcilePendingPaymentBatch(context.Background(), []*dbent.PaymentOrder{order}, reconcile))
	require.Zero(t, ledger.effectCount(order.ID))

	providerStatus = payment.ProviderStatusPaid
	require.Equal(t, 1, reconcilePendingPaymentBatch(context.Background(), []*dbent.PaymentOrder{order}, reconcile))
	require.Equal(t, 1, ledger.effectCount(order.ID))

	require.Zero(t, reconcilePendingPaymentBatch(context.Background(), []*dbent.PaymentOrder{order}, reconcile))
	require.Equal(t, 1, ledger.effectCount(order.ID))
}

func TestReconcilePendingPaymentBatchConcurrentCompensationAndCallbackApplyOnce(t *testing.T) {
	order := &dbent.PaymentOrder{ID: 31}
	orders := []*dbent.PaymentOrder{order}
	ledger := newPaymentCompensationMockLedger(order)
	start := make(chan struct{})
	results := make(chan int, 2)
	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- reconcilePendingPaymentBatch(context.Background(), orders, ledger.applyPaid)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_ = ledger.applyPaid(context.Background(), order) // mock late provider callback
	}()

	close(start)
	wg.Wait()
	close(results)

	recovered := 0
	for result := range results {
		recovered += result
	}
	require.LessOrEqual(t, recovered, 1)
	require.Equal(t, 1, ledger.effectCount(order.ID))
}

func TestIsPaidProviderResponseRejectsPendingUnknownNilAndInvalidAmounts(t *testing.T) {
	tests := []struct {
		name string
		resp *payment.QueryOrderResponse
		want bool
	}{
		{name: "nil", resp: nil},
		{name: "pending", resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPending, Amount: 99}},
		{name: "unknown", resp: &payment.QueryOrderResponse{Status: "unknown", Amount: 99}},
		{name: "zero", resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPaid, Amount: 0}},
		{name: "negative", resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPaid, Amount: -1}},
		{name: "nan", resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPaid, Amount: math.NaN()}},
		{name: "positive infinity", resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPaid, Amount: math.Inf(1)}},
		{name: "paid", resp: &payment.QueryOrderResponse{Status: payment.ProviderStatusPaid, Amount: 99}, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isPaidProviderResponse(tc.resp))
		})
	}
}

func TestRequeryPaidOrderOnceRejectsPendingUnknownAndNilThenAcceptsPaid(t *testing.T) {
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeAlipay,
		responses: []*payment.QueryOrderResponse{
			{Status: payment.ProviderStatusPending, Amount: 99},
			{Status: "unknown", Amount: 99},
			nil,
			{Status: payment.ProviderStatusPaid, Amount: 99},
		},
	}
	for i := 0; i < 3; i++ {
		resp, ok := requeryPaidOrderOnce(context.Background(), provider, "sub2_query_ref")
		require.False(t, ok)
		require.Nil(t, resp)
	}
	resp, ok := requeryPaidOrderOnce(context.Background(), provider, "sub2_query_ref")
	require.True(t, ok)
	require.Equal(t, payment.ProviderStatusPaid, resp.Status)
	require.Equal(t, 4, provider.queryCalls)
	require.Equal(t, "sub2_query_ref", provider.lastQueryTradeNo)
}

func TestPaymentCompensationRejectsPinnedProviderFallbackAndWrongMerchantMetadata(t *testing.T) {
	instanceID := "42"
	order := &dbent.PaymentOrder{
		ID:                 99,
		PaymentType:        payment.TypeAlipay,
		ProviderInstanceID: &instanceID,
		ProviderSnapshot: map[string]any{
			"schema_version":       2,
			"provider_instance_id": instanceID,
			"provider_key":         payment.TypeAlipay,
			"merchant_app_id":      "expected-app-id",
		},
	}

	require.False(t, paymentOrderAllowsRegistryFallback(order))
	require.Equal(t, payment.TypeAlipay, expectedNotificationProviderKeyForOrder(nil, order, ""))
	err := validateProviderNotificationMetadata(order, payment.TypeAlipay, map[string]string{"app_id": "wrong-app-id"})
	require.ErrorContains(t, err, "app_id mismatch")
}

func TestReconcilePendingPaymentBatchNilReconcilerDoesNothing(t *testing.T) {
	require.Zero(t, reconcilePendingPaymentBatch(context.Background(), []*dbent.PaymentOrder{{ID: 1}}, nil))
}
