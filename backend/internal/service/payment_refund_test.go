//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentrefundattempt"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type refundTestFixture struct {
	ctx      context.Context
	client   *dbent.Client
	user     *dbent.User
	order    *dbent.PaymentOrder
	service  *PaymentService
	provider *refundProviderRecorder
}

func newRefundTestFixture(t *testing.T, balance float64) *refundTestFixture {
	t.Helper()
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	sum := sha256.Sum256([]byte(t.Name()))
	suffix := fmt.Sprintf("%x", sum[:8])
	user, err := client.User.Create().
		SetEmail(suffix + "@example.com").
		SetPasswordHash("hash").
		SetUsername(suffix).
		SetBalance(balance).
		SetTotalRecharged(777).
		Save(ctx)
	require.NoError(t, err)
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeStripe).
		SetName(suffix + "-provider").
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
		SetRechargeCode("REFUND-" + suffix).
		SetOutTradeNo("sub2_" + suffix).
		SetPaymentType(payment.TypeStripe).
		SetPaymentTradeNo("pi_" + suffix).
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).
		Save(ctx)
	require.NoError(t, err)
	provider := &refundProviderRecorder{
		refundResponse: &payment.RefundResponse{RefundID: "rf_" + suffix, Status: payment.ProviderStatusSuccess},
		queryResponse:  &payment.RefundResponse{RefundID: "rf_" + suffix, Status: payment.ProviderStatusSuccess},
	}
	originalFactory := createPaymentProviderFromInstance
	createPaymentProviderFromInstance = func(string, string, map[string]string) (payment.Provider, error) {
		return provider, nil
	}
	t.Cleanup(func() { createPaymentProviderFromInstance = originalFactory })
	return &refundTestFixture{
		ctx:      ctx,
		client:   client,
		user:     user,
		order:    order,
		service:  &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, subscriptionSvc: &SubscriptionService{}},
		provider: provider,
	}
}

func (f *refundTestFixture) prepare(t *testing.T, amount float64, force, deduct bool) *RefundPlan {
	t.Helper()
	plan, result, err := f.service.PrepareRefund(f.ctx, f.order.ID, amount, "test refund", force, deduct)
	require.NoError(t, err)
	require.Nil(t, result)
	require.NotNil(t, plan)
	return plan
}

func latestRefundAttemptForTest(t *testing.T, ctx context.Context, client *dbent.Client, orderID int64) *dbent.PaymentRefundAttempt {
	t.Helper()
	attempt, err := client.PaymentRefundAttempt.Query().
		Where(paymentrefundattempt.OrderIDEQ(orderID)).
		Order(dbent.Desc(paymentrefundattempt.FieldCreatedAt), dbent.Desc(paymentrefundattempt.FieldID)).
		First(ctx)
	require.NoError(t, err)
	return attempt
}

func TestExecuteRefundAtomicHoldInsufficientSkipsProvider(t *testing.T) {
	f := newRefundTestFixture(t, 100)
	plan := f.prepare(t, 100, false, true)
	_, err := f.client.User.UpdateOneID(f.user.ID).SetBalance(20).Save(f.ctx)
	require.NoError(t, err)

	result, err := f.service.ExecuteRefund(f.ctx, plan)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.RequireForce)
	require.Zero(t, f.provider.refundCalls)

	order, err := f.client.PaymentOrder.Get(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, order.Status)
	count, err := f.client.PaymentRefundAttempt.Query().Where(paymentrefundattempt.OrderIDEQ(f.order.ID)).Count(f.ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	user, err := f.client.User.Get(f.ctx, f.user.ID)
	require.NoError(t, err)
	require.Equal(t, 20.0, user.Balance)
}

func TestExecuteRefundForceHoldsOnlyAvailablePermanentBalance(t *testing.T) {
	f := newRefundTestFixture(t, 35)
	plan := f.prepare(t, 100, true, true)

	result, err := f.service.ExecuteRefund(f.ctx, plan)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, 35.0, result.BalanceDeducted)
	require.Equal(t, 1, f.provider.refundCalls)

	user, err := f.client.User.Get(f.ctx, f.user.ID)
	require.NoError(t, err)
	require.Zero(t, user.Balance)
	require.Equal(t, 777.0, user.TotalRecharged)
	attempt := latestRefundAttemptForTest(t, f.ctx, f.client, f.order.ID)
	require.Equal(t, 35.0, attempt.HeldBalanceAmount)
	require.Equal(t, refundDeductionStateConsumed, attempt.DeductionState)
}

func TestPrepareRefundDeductBalanceFalseRequiresForceAndPersistsAuditChoice(t *testing.T) {
	f := newRefundTestFixture(t, 100)
	plan, result, err := f.service.PrepareRefund(f.ctx, f.order.ID, 100, "", false, false)
	require.Nil(t, plan)
	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, "DEDUCTION_REQUIRED", infraerrors.Reason(err))
	require.Zero(t, f.provider.refundCalls)

	plan = f.prepare(t, 100, true, false)
	result, err = f.service.ExecuteRefund(f.ctx, plan)
	require.NoError(t, err)
	require.True(t, result.Success)
	user, err := f.client.User.Get(f.ctx, f.user.ID)
	require.NoError(t, err)
	require.Equal(t, 100.0, user.Balance)
	attempt := latestRefundAttemptForTest(t, f.ctx, f.client, f.order.ID)
	require.True(t, attempt.Force)
	require.False(t, attempt.DeductBalance)
	require.Equal(t, refundDeductionStateNone, attempt.DeductionState)
	require.Equal(t, payment.DeductionTypeNone, attempt.DeductionType)
}

func TestRefundPendingKeepsHoldAndQuerySuccessConsumesExactlyOnce(t *testing.T) {
	f := newRefundTestFixture(t, 100)
	f.provider.refundResponse = &payment.RefundResponse{RefundID: "rf_pending", Status: payment.ProviderStatusPending}
	f.provider.queryResponse = &payment.RefundResponse{RefundID: "rf_pending", Status: payment.ProviderStatusSuccess}
	plan := f.prepare(t, 100, false, true)

	pending, err := f.service.ExecuteRefund(f.ctx, plan)
	require.NoError(t, err)
	require.False(t, pending.Success)
	user, err := f.client.User.Get(f.ctx, f.user.ID)
	require.NoError(t, err)
	require.Zero(t, user.Balance)
	attempt := latestRefundAttemptForTest(t, f.ctx, f.client, f.order.ID)
	require.Equal(t, refundDeductionStateHeld, attempt.DeductionState)
	require.Equal(t, refundProviderStatePending, attempt.ProviderState)
	require.Equal(t, attempt.AttemptID, f.provider.refundRequest.AttemptID)

	completed, err := f.service.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.True(t, completed.Success)
	require.Equal(t, attempt.AttemptID, f.provider.queryRequest.AttemptID)
	attempt = latestRefundAttemptForTest(t, f.ctx, f.client, f.order.ID)
	require.Equal(t, refundDeductionStateConsumed, attempt.DeductionState)
	require.Equal(t, refundProviderStateSucceeded, attempt.ProviderState)

	replayed, err := f.service.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.True(t, replayed.Success)
	require.Equal(t, 1, f.provider.refundCalls)
	require.Equal(t, 1, f.provider.queryCalls)
	user, err = f.client.User.Get(f.ctx, f.user.ID)
	require.NoError(t, err)
	require.Zero(t, user.Balance)
}

func TestRefundPendingFailureAndCancelReturnHoldExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name           string
		providerStatus string
		attemptState   string
	}{
		{name: "failed", providerStatus: payment.ProviderStatusFailed, attemptState: refundProviderStateFailed},
		{name: "canceled", providerStatus: payment.ProviderStatusCanceled, attemptState: refundProviderStateCanceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRefundTestFixture(t, 100)
			f.provider.refundResponse = &payment.RefundResponse{RefundID: "rf_terminal", Status: payment.ProviderStatusPending}
			f.provider.queryResponse = &payment.RefundResponse{RefundID: "rf_terminal", Status: tc.providerStatus}
			plan := f.prepare(t, 100, false, true)
			_, err := f.service.ExecuteRefund(f.ctx, plan)
			require.NoError(t, err)

			result, err := f.service.QueryAndFinalizeRefund(f.ctx, f.order.ID)
			require.NoError(t, err)
			require.False(t, result.Success)
			user, err := f.client.User.Get(f.ctx, f.user.ID)
			require.NoError(t, err)
			require.Equal(t, 100.0, user.Balance)
			require.Equal(t, 777.0, user.TotalRecharged, "returning a hold must not count as recharge")
			attempt := latestRefundAttemptForTest(t, f.ctx, f.client, f.order.ID)
			require.Equal(t, refundDeductionStateReturned, attempt.DeductionState)
			require.Equal(t, tc.attemptState, attempt.ProviderState)

			replayed, err := f.service.QueryAndFinalizeRefund(f.ctx, f.order.ID)
			require.NoError(t, err)
			require.False(t, replayed.Success)
			user, err = f.client.User.Get(f.ctx, f.user.ID)
			require.NoError(t, err)
			require.Equal(t, 100.0, user.Balance)
			require.Equal(t, 1, f.provider.queryCalls)
		})
	}
}

func TestRefundUnknownKeepsHoldAndBlocksRepeatedOrdinaryAPI(t *testing.T) {
	f := newRefundTestFixture(t, 100)
	f.provider.refundResponse = nil
	f.provider.refundErr = context.DeadlineExceeded
	plan := f.prepare(t, 100, false, true)

	result, err := f.service.ExecuteRefund(f.ctx, plan)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.Warning, "unknown")
	attempt := latestRefundAttemptForTest(t, f.ctx, f.client, f.order.ID)
	require.Equal(t, refundProviderStateUnknown, attempt.ProviderState)
	require.Equal(t, refundDeductionStateHeld, attempt.DeductionState)
	require.True(t, attempt.ManualReview)
	user, err := f.client.User.Get(f.ctx, f.user.ID)
	require.NoError(t, err)
	require.Zero(t, user.Balance)

	retryPlan, retryResult, err := f.service.PrepareRefund(f.ctx, f.order.ID, 100, "retry", false, true)
	require.NoError(t, err)
	require.Nil(t, retryPlan)
	require.NotNil(t, retryResult)
	require.Contains(t, retryResult.Warning, "no new provider request")
	require.Equal(t, 1, f.provider.refundCalls)
}

func TestRefundUnknownUnsupportedQueryRemainsManual(t *testing.T) {
	f := newRefundTestFixture(t, 100)
	provider := &refundProviderWithoutQuery{refundErr: context.DeadlineExceeded}
	originalFactory := createPaymentProviderFromInstance
	createPaymentProviderFromInstance = func(string, string, map[string]string) (payment.Provider, error) { return provider, nil }
	t.Cleanup(func() { createPaymentProviderFromInstance = originalFactory })
	plan := f.prepare(t, 100, false, true)
	_, err := f.service.ExecuteRefund(f.ctx, plan)
	require.NoError(t, err)

	result, err := f.service.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.Warning, "cannot query")
	attempt := latestRefundAttemptForTest(t, f.ctx, f.client, f.order.ID)
	require.True(t, attempt.ManualReview)
	require.Equal(t, refundDeductionStateHeld, attempt.DeductionState)
	require.Equal(t, 1, provider.refundCalls)
}

func TestSubscriptionRefundPendingHoldsThenFailureRestoresExactExpiry(t *testing.T) {
	f := newRefundTestFixture(t, 0)
	now := time.Now().UTC().Truncate(time.Second)
	group, err := f.client.Group.Create().SetName("refund-subscription-" + strings.ReplaceAll(t.Name(), "/", "-")).SetStatus(StatusActive).Save(f.ctx)
	require.NoError(t, err)
	originalExpiry := now.AddDate(0, 0, 20)
	sub, err := f.client.UserSubscription.Create().
		SetUserID(f.user.ID).
		SetGroupID(group.ID).
		SetStartsAt(now.Add(-time.Hour)).
		SetExpiresAt(originalExpiry).
		SetStatus(SubscriptionStatusActive).
		SetAssignedAt(now).
		SetNotes("").
		Save(f.ctx)
	require.NoError(t, err)
	f.order, err = f.client.PaymentOrder.UpdateOneID(f.order.ID).
		SetOrderType(payment.OrderTypeSubscription).
		SetSubscriptionGroupID(group.ID).
		SetSubscriptionDays(10).
		Save(f.ctx)
	require.NoError(t, err)
	f.provider.refundResponse = &payment.RefundResponse{RefundID: "rf_sub", Status: payment.ProviderStatusPending}
	f.provider.queryResponse = &payment.RefundResponse{RefundID: "rf_sub", Status: payment.ProviderStatusFailed}
	plan := f.prepare(t, 100, false, true)

	_, err = f.service.ExecuteRefund(f.ctx, plan)
	require.NoError(t, err)
	held, err := f.client.UserSubscription.Get(f.ctx, sub.ID)
	require.NoError(t, err)
	require.WithinDuration(t, originalExpiry.AddDate(0, 0, -10), held.ExpiresAt, time.Second)
	attempt := latestRefundAttemptForTest(t, f.ctx, f.client, f.order.ID)
	require.Equal(t, refundDeductionStateHeld, attempt.DeductionState)
	require.Equal(t, 10, attempt.SubscriptionDays)

	_, err = f.service.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	restored, err := f.client.UserSubscription.Get(f.ctx, sub.ID)
	require.NoError(t, err)
	require.WithinDuration(t, originalExpiry, restored.ExpiresAt, time.Second)
	attempt = latestRefundAttemptForTest(t, f.ctx, f.client, f.order.ID)
	require.Equal(t, refundDeductionStateReturned, attempt.DeductionState)
}

func TestSubscriptionRefundPendingSuccessDoesNotDeductTwice(t *testing.T) {
	f := newRefundTestFixture(t, 0)
	now := time.Now().UTC().Truncate(time.Second)
	group, err := f.client.Group.Create().SetName("refund-subscription-success-" + strings.ReplaceAll(t.Name(), "/", "-")).SetStatus(StatusActive).Save(f.ctx)
	require.NoError(t, err)
	originalExpiry := now.AddDate(0, 0, 20)
	sub, err := f.client.UserSubscription.Create().
		SetUserID(f.user.ID).
		SetGroupID(group.ID).
		SetStartsAt(now.Add(-time.Hour)).
		SetExpiresAt(originalExpiry).
		SetStatus(SubscriptionStatusActive).
		SetAssignedAt(now).
		SetNotes("").
		Save(f.ctx)
	require.NoError(t, err)
	f.order, err = f.client.PaymentOrder.UpdateOneID(f.order.ID).
		SetOrderType(payment.OrderTypeSubscription).
		SetSubscriptionGroupID(group.ID).
		SetSubscriptionDays(10).
		Save(f.ctx)
	require.NoError(t, err)
	f.provider.refundResponse = &payment.RefundResponse{RefundID: "rf_sub_success", Status: payment.ProviderStatusPending}
	f.provider.queryResponse = &payment.RefundResponse{RefundID: "rf_sub_success", Status: payment.ProviderStatusSuccess}
	plan := f.prepare(t, 100, false, true)
	_, err = f.service.ExecuteRefund(f.ctx, plan)
	require.NoError(t, err)
	_, err = f.service.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	current, err := f.client.UserSubscription.Get(f.ctx, sub.ID)
	require.NoError(t, err)
	require.WithinDuration(t, originalExpiry.AddDate(0, 0, -10), current.ExpiresAt, time.Second)
	attempt := latestRefundAttemptForTest(t, f.ctx, f.client, f.order.ID)
	require.Equal(t, refundDeductionStateConsumed, attempt.DeductionState)
}

func TestPartiallyRefundedOrderRejectsAnotherRefund(t *testing.T) {
	f := newRefundTestFixture(t, 100)
	plan := f.prepare(t, 50, false, true)
	result, err := f.service.ExecuteRefund(f.ctx, plan)
	require.NoError(t, err)
	require.True(t, result.Success)
	order, err := f.client.PaymentOrder.Get(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPartiallyRefunded, order.Status)

	plan, early, err := f.service.PrepareRefund(f.ctx, f.order.ID, 10, "again", true, true)
	require.Nil(t, plan)
	require.Nil(t, early)
	require.Error(t, err)
	require.Equal(t, "MULTIPLE_PARTIAL_REFUNDS_UNSUPPORTED", infraerrors.Reason(err))
	require.Equal(t, 1, f.provider.refundCalls)
}

func TestLegacyRefundStatesRequireManualReviewWithoutAssumingHold(t *testing.T) {
	for _, status := range []string{OrderStatusRefundPending, OrderStatusRefunding, OrderStatusRefundFailed} {
		t.Run(status, func(t *testing.T) {
			f := newRefundTestFixture(t, 80)
			_, err := f.client.PaymentOrder.UpdateOneID(f.order.ID).SetStatus(status).SetRefundAmount(100).Save(f.ctx)
			require.NoError(t, err)
			if status == OrderStatusRefundFailed {
				plan, result, prepErr := f.service.PrepareRefund(f.ctx, f.order.ID, 100, "", true, true)
				require.Nil(t, plan)
				require.Nil(t, result)
				require.Error(t, prepErr)
				require.Equal(t, "REFUND_MANUAL_REVIEW_REQUIRED", infraerrors.Reason(prepErr))
			} else {
				plan, result, prepErr := f.service.PrepareRefund(f.ctx, f.order.ID, 100, "", true, true)
				require.NoError(t, prepErr)
				require.Nil(t, plan)
				require.NotNil(t, result)
			}
			_, queryErr := f.service.QueryAndFinalizeRefund(f.ctx, f.order.ID)
			require.Error(t, queryErr)
			require.Equal(t, "REFUND_MANUAL_REVIEW_REQUIRED", infraerrors.Reason(queryErr))
			user, err := f.client.User.Get(f.ctx, f.user.ID)
			require.NoError(t, err)
			require.Equal(t, 80.0, user.Balance)
			require.Zero(t, f.provider.refundCalls)
		})
	}
}

func TestValidateRefundProviderResponseAcceptsTerminalAndPendingStates(t *testing.T) {
	for _, status := range []string{
		payment.ProviderStatusPending,
		payment.ProviderStatusSuccess,
		payment.ProviderStatusRefunded,
		payment.ProviderStatusFailed,
		payment.ProviderStatusCanceled,
	} {
		require.NoError(t, validateRefundProviderResponse(&payment.RefundResponse{Status: status}))
	}
	require.Error(t, validateRefundProviderResponse(nil))
	require.Error(t, validateRefundProviderResponse(&payment.RefundResponse{Status: "mystery"}))
}

func TestRefundProviderRequestUsesPersistedStableAttemptID(t *testing.T) {
	f := newRefundTestFixture(t, 100)
	f.provider.refundResponse = &payment.RefundResponse{RefundID: "rf_stable", Status: payment.ProviderStatusPending}
	f.provider.queryResponse = &payment.RefundResponse{RefundID: "rf_stable", Status: payment.ProviderStatusPending}
	plan := f.prepare(t, 100, false, true)
	_, err := f.service.ExecuteRefund(f.ctx, plan)
	require.NoError(t, err)
	attempt := latestRefundAttemptForTest(t, f.ctx, f.client, f.order.ID)
	require.NotEmpty(t, attempt.AttemptID)
	require.Equal(t, attempt.AttemptID, f.provider.refundRequest.AttemptID)
	_, err = f.service.QueryAndFinalizeRefund(f.ctx, f.order.ID)
	require.NoError(t, err)
	require.Equal(t, attempt.AttemptID, f.provider.queryRequest.AttemptID)
}

type refundProviderRecorder struct {
	refundResponse *payment.RefundResponse
	refundErr      error
	queryResponse  *payment.RefundResponse
	queryErr       error
	refundCalls    int
	queryCalls     int
	refundRequest  payment.RefundRequest
	queryRequest   payment.RefundQueryRequest
}

func (*refundProviderRecorder) Name() string        { return "refund-recorder" }
func (*refundProviderRecorder) ProviderKey() string { return payment.TypeStripe }
func (*refundProviderRecorder) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeStripe}
}
func (*refundProviderRecorder) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	return nil, errors.New("unexpected CreatePayment")
}
func (*refundProviderRecorder) QueryOrder(context.Context, string) (*payment.QueryOrderResponse, error) {
	return nil, errors.New("unexpected QueryOrder")
}
func (*refundProviderRecorder) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	return nil, errors.New("unexpected VerifyNotification")
}
func (p *refundProviderRecorder) Refund(_ context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	p.refundCalls++
	p.refundRequest = req
	return p.refundResponse, p.refundErr
}
func (p *refundProviderRecorder) QueryRefund(_ context.Context, req payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	p.queryCalls++
	p.queryRequest = req
	return p.queryResponse, p.queryErr
}

type refundProviderWithoutQuery struct {
	refundErr   error
	refundCalls int
}

func (*refundProviderWithoutQuery) Name() string        { return "refund-no-query" }
func (*refundProviderWithoutQuery) ProviderKey() string { return payment.TypeStripe }
func (*refundProviderWithoutQuery) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeStripe}
}
func (*refundProviderWithoutQuery) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	return nil, errors.New("unexpected CreatePayment")
}
func (*refundProviderWithoutQuery) QueryOrder(context.Context, string) (*payment.QueryOrderResponse, error) {
	return nil, errors.New("unexpected QueryOrder")
}
func (*refundProviderWithoutQuery) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	return nil, errors.New("unexpected VerifyNotification")
}
func (p *refundProviderWithoutQuery) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	p.refundCalls++
	return nil, p.refundErr
}

var _ payment.Provider = (*refundProviderRecorder)(nil)
var _ payment.RefundQueryProvider = (*refundProviderRecorder)(nil)
var _ payment.Provider = (*refundProviderWithoutQuery)(nil)
