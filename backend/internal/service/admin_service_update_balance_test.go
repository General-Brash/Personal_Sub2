//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"net/http"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type balanceUserRepoStub struct {
	*userRepoStub
	updateErr   error
	updated     []*User
	auditDeltas []float64
	applyCalls  int
}

func (s *balanceUserRepoStub) Update(ctx context.Context, user *User) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	if user == nil {
		return nil
	}
	clone := *user
	s.updated = append(s.updated, &clone)
	if s.userRepoStub != nil {
		s.userRepoStub.user = &clone
	}
	return nil
}

func (s *balanceUserRepoStub) ApplyAdminBalanceAdjustment(_ context.Context, userID int64, adjustment AdminBalanceAdjustment) (*AdminBalanceAdjustmentResult, error) {
	s.applyCalls++
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	user, err := s.GetByID(context.Background(), userID)
	if err != nil {
		return nil, err
	}
	next, delta, err := adjustment.ApplyTo(user.Balance)
	if err != nil {
		return nil, err
	}
	user.Balance = next
	if err := s.Update(context.Background(), user); err != nil {
		return nil, err
	}
	if delta != 0 {
		s.auditDeltas = append(s.auditDeltas, delta)
	}
	return &AdminBalanceAdjustmentResult{User: user, BalanceDelta: delta, Response: user}, nil
}

func (s *balanceUserRepoStub) ApplyAdminBalanceAdjustmentAtomic(ctx context.Context, userID int64, adjustment AdminBalanceAdjustment, _ *IdempotencyAtomicClaim, responseFactory AdminBalanceAdjustmentResponseFactory) (*AdminBalanceAdjustmentResult, error) {
	result, err := s.ApplyAdminBalanceAdjustment(ctx, userID, adjustment)
	if err != nil {
		return nil, err
	}
	result.Response = responseFactory(result.User)
	return result, nil
}

type balanceRedeemRepoStub struct {
	*redeemRepoStub
	created []*RedeemCode
}

func (s *balanceRedeemRepoStub) Create(ctx context.Context, code *RedeemCode) error {
	if code == nil {
		return nil
	}
	clone := *code
	s.created = append(s.created, &clone)
	return nil
}

type authCacheInvalidatorStub struct {
	userIDs  []int64
	groupIDs []int64
	keys     []string
	ctxErrs  []error
	onUser   func()
}

type adminRechargeAffiliateAccruerStub struct {
	calls  []adminRechargeAffiliateAccrual
	rebate float64
	err    error
	onCall func()
}

type adminRechargeAffiliateAccrual struct {
	userID int64
	amount float64
}

func (s *adminRechargeAffiliateAccruerStub) AccrueInviteRebate(_ context.Context, userID int64, amount float64) (float64, error) {
	s.calls = append(s.calls, adminRechargeAffiliateAccrual{userID: userID, amount: amount})
	if s.onCall != nil {
		s.onCall()
	}
	return s.rebate, s.err
}

func adminRechargeSettingService(enabled bool) *SettingService {
	values := map[string]string{}
	if enabled {
		values[SettingKeyAffiliateAdminRechargeEnabled] = "true"
	}
	return NewSettingService(&settingRepoStub{values: values}, nil)
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByKey(ctx context.Context, key string) {
	s.keys = append(s.keys, key)
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	s.userIDs = append(s.userIDs, userID)
	s.ctxErrs = append(s.ctxErrs, ctx.Err())
	if s.onUser != nil {
		s.onUser()
	}
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByGroupID(ctx context.Context, groupID int64) {
	s.groupIDs = append(s.groupIDs, groupID)
}

type adminBalanceBillingCacheStub struct {
	*billingCacheStub
	balanceInvalidations   []int64
	availableInvalidations []int64
	balanceCtxErrs         []error
	balanceDeadlines       []time.Time
	balanceStarted         chan<- struct{}
	balanceRelease         <-chan struct{}
	balanceErr             error
	availableErr           error
}

func newAdminBalanceBillingCacheStub() *adminBalanceBillingCacheStub {
	return &adminBalanceBillingCacheStub{billingCacheStub: newBillingCacheStub(0)}
}

func (s *adminBalanceBillingCacheStub) InvalidateUserBalance(ctx context.Context, userID int64) error {
	s.balanceInvalidations = append(s.balanceInvalidations, userID)
	s.balanceCtxErrs = append(s.balanceCtxErrs, ctx.Err())
	if deadline, ok := ctx.Deadline(); ok {
		s.balanceDeadlines = append(s.balanceDeadlines, deadline)
	}
	if s.balanceStarted != nil {
		s.balanceStarted <- struct{}{}
	}
	if s.balanceRelease != nil {
		select {
		case <-s.balanceRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.balanceErr
}

func (s *adminBalanceBillingCacheStub) GetAvailableCredit(context.Context, int64) (float64, error) {
	panic("unexpected GetAvailableCredit call")
}

func (s *adminBalanceBillingCacheStub) SetAvailableCredit(context.Context, int64, float64, time.Duration) error {
	panic("unexpected SetAvailableCredit call")
}

func (s *adminBalanceBillingCacheStub) InvalidateAvailableCredit(_ context.Context, userID int64) error {
	s.availableInvalidations = append(s.availableInvalidations, userID)
	return s.availableErr
}

func TestAdminService_UpdateUserBalance_InvalidatesAuthCache(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{ID: 7, Balance: 10}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       redeemRepo,
		authCacheInvalidator: invalidator,
	}

	_, err := svc.UpdateUserBalance(context.Background(), 7, 5, "add", "")
	require.NoError(t, err)
	require.Equal(t, []int64{7}, invalidator.userIDs)
	require.Len(t, repo.auditDeltas, 1)
}

func TestAdminService_UpdateUserBalanceAtomicInvalidatesCachesBeforeSuccessWithoutPostCommitAffiliate(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{ID: 7, Balance: 10}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	cache := newAdminBalanceBillingCacheStub()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	cache.balanceStarted = started
	cache.balanceRelease = release
	invalidator := &authCacheInvalidatorStub{}
	affiliate := &adminRechargeAffiliateAccruerStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		billingCacheService:  &BillingCacheService{cache: cache},
		authCacheInvalidator: invalidator,
		settingService:       adminRechargeSettingService(true),
		affiliateService:     affiliate,
	}

	type updateResult struct {
		data any
		err  error
	}
	done := make(chan updateResult, 1)
	go func() {
		data, err := svc.UpdateUserBalanceAtomic(
			context.Background(),
			7,
			5,
			"add",
			"sync-cache",
			nil,
			func(user *User) any { return user },
		)
		done <- updateResult{data: data, err: err}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("balance cache invalidation did not start")
	}
	select {
	case result := <-done:
		t.Fatalf("balance update returned before cache invalidation completed: %v", result.err)
	default:
	}
	close(release)

	var result updateResult
	select {
	case result = <-done:
	case <-time.After(time.Second):
		t.Fatal("balance update did not finish after cache invalidation")
	}
	require.NoError(t, result.err)
	require.Equal(t, []int64{7}, cache.balanceInvalidations)
	require.Equal(t, []int64{7}, cache.availableInvalidations)
	require.Equal(t, []int64{7}, invalidator.userIDs)
	require.Empty(t, affiliate.calls)
	require.Equal(t, 1, repo.applyCalls)
	require.Len(t, cache.balanceDeadlines, 1)
	remaining := time.Until(cache.balanceDeadlines[0])
	require.Positive(t, remaining)
	require.LessOrEqual(t, remaining, adminBalanceCacheInvalidationTimeout)
}

func TestAdminService_UpdateUserBalanceAtomicCacheFailureKeepsCommittedSuccess(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{ID: 7, Balance: 10}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	cache := newAdminBalanceBillingCacheStub()
	cache.balanceErr = errors.New("redis balance unavailable")
	cache.availableErr = errors.New("redis available credit unavailable")
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		billingCacheService:  &BillingCacheService{cache: cache},
		authCacheInvalidator: invalidator,
	}
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()

	data, err := svc.UpdateUserBalanceAtomic(
		requestCtx,
		7,
		5,
		"add",
		"cache-failure",
		nil,
		func(user *User) any { return user },
	)

	require.NoError(t, err)
	updated, ok := data.(*User)
	require.True(t, ok)
	require.Equal(t, 15.0, updated.Balance)
	require.Equal(t, 1, repo.applyCalls, "cache failure must not trigger a second balance adjustment")
	require.Equal(t, []int64{7}, cache.balanceInvalidations)
	require.Equal(t, []int64{7}, cache.availableInvalidations)
	require.Equal(t, []error{nil}, cache.balanceCtxErrs)
	require.Equal(t, []int64{7}, invalidator.userIDs)
	require.Equal(t, []error{nil}, invalidator.ctxErrs)
}

func TestAdminService_UpdateUserBalance_NoChangeNoInvalidate(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{ID: 7, Balance: 10}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       redeemRepo,
		authCacheInvalidator: invalidator,
	}

	_, err := svc.UpdateUserBalance(context.Background(), 7, 10, "set", "")
	require.NoError(t, err)
	require.Empty(t, invalidator.userIDs)
	require.Empty(t, repo.auditDeltas)
}

func TestAdminService_UpdateUserBalance_AdminRechargeAffiliateRebate(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		operation string
		amount    float64
	}{
		{
			name:      "disabled by default",
			operation: "add",
			amount:    5,
		},
		{
			name:      "enabled add",
			enabled:   true,
			operation: "add",
			amount:    0.1,
		},
		{
			name:      "enabled set increase",
			enabled:   true,
			operation: "set",
			amount:    15,
		},
		{
			name:      "enabled subtract",
			enabled:   true,
			operation: "subtract",
			amount:    5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseRepo := &userRepoStub{user: &User{ID: 7, Balance: 10}}
			repo := &balanceUserRepoStub{userRepoStub: baseRepo}
			redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
			affiliate := &adminRechargeAffiliateAccruerStub{}
			svc := &adminServiceImpl{
				userRepo:         repo,
				redeemCodeRepo:   redeemRepo,
				settingService:   adminRechargeSettingService(tt.enabled),
				affiliateService: affiliate,
			}

			_, err := svc.UpdateUserBalance(context.Background(), 7, tt.amount, tt.operation, "")
			require.NoError(t, err)
			require.Empty(t, affiliate.calls, "affiliate delivery must be owned by the transactional outbox")
		})
	}
}

func TestAdminService_UpdateUserBalance_DoesNotUsePostCommitAffiliateDelivery(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{ID: 7, Balance: 10}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
	affiliate := &adminRechargeAffiliateAccruerStub{err: errors.New("affiliate unavailable")}
	svc := &adminServiceImpl{
		userRepo:         repo,
		redeemCodeRepo:   redeemRepo,
		settingService:   adminRechargeSettingService(true),
		affiliateService: affiliate,
	}

	user, err := svc.UpdateUserBalance(context.Background(), 7, 5, "add", "")
	require.NoError(t, err)
	require.Equal(t, 15.0, user.Balance)
	require.Empty(t, affiliate.calls)
	require.Len(t, repo.auditDeltas, 1)
}

func TestAdminService_UpdateUserBalance_PreservesLegacyFloatContract(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{
		ID:      7,
		Balance: 0.1,
	}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
	svc := &adminServiceImpl{
		userRepo:       repo,
		redeemCodeRepo: redeemRepo,
	}

	updated, err := svc.UpdateUserBalance(
		context.Background(),
		7,
		0.2,
		"add",
		"",
	)
	require.NoError(t, err)
	require.InDelta(t, 0.3, updated.Balance, 1e-12)
	require.Len(t, repo.auditDeltas, 1)
	require.InDelta(t, 0.2, repo.auditDeltas[0], 1e-12)
}

func TestAdminBalanceAdjustmentApplyToNormalizesEightDecimals(t *testing.T) {
	adjustment, err := newAdminBalanceAdjustment(0.200000004, "add", "precision")
	require.NoError(t, err)

	next, delta, err := adjustment.ApplyTo(0.100000004)
	require.NoError(t, err)
	require.Equal(t, 0.3, next)
	require.Equal(t, 0.2, delta)
}

func TestAdminBalanceAdjustmentApplyToSubtractBoundary(t *testing.T) {
	adjustment, err := newAdminBalanceAdjustment(0.8, "subtract", "boundary")
	require.NoError(t, err)

	next, delta, err := adjustment.ApplyTo(0.8)
	require.NoError(t, err)
	require.Zero(t, next)
	require.Equal(t, -0.8, delta)

	_, _, err = adjustment.ApplyTo(0.79999999)
	require.ErrorIs(t, err, ErrAdminBalanceInsufficient)
	require.Equal(t, http.StatusConflict, infraerrors.Code(err))
	require.Equal(t, "INSUFFICIENT_BALANCE", infraerrors.Reason(err))
	require.Equal(t, "insufficient permanent balance", infraerrors.Message(err))
	require.Equal(t, map[string]string{
		"current_balance":   "0.79999999",
		"requested_amount":  "0.80000000",
		"resulting_balance": "-0.00000001",
		"operation":         "subtract",
	}, infraerrors.FromError(err).Metadata)
}

func TestAdminBalanceAdjustmentApplyToSetAndRejectsInvalidAmount(t *testing.T) {
	adjustment, err := newAdminBalanceAdjustment(1.234567895, "set", "set")
	require.NoError(t, err)
	next, delta, err := adjustment.ApplyTo(2)
	require.NoError(t, err)
	require.Equal(t, 1.2345679, next)
	require.Equal(t, -0.7654321, delta)

	for _, amount := range []float64{0.000000001, maxLedgerAmount, math.Inf(1)} {
		_, err = newAdminBalanceAdjustment(amount, "add", "invalid")
		require.ErrorIs(t, err, ErrInvalidAdminBalanceAdjustment)
		require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
		require.Equal(t, "INVALID_BALANCE_ADJUSTMENT", infraerrors.Reason(err))
		require.Equal(t, "invalid balance adjustment", infraerrors.Message(err))
	}
}

func TestAdminBalanceAdjustmentApplyToMapsResultOverflowToBadRequest(t *testing.T) {
	adjustment, err := newAdminBalanceAdjustment(1, "add", "overflow")
	require.NoError(t, err)

	_, _, err = adjustment.ApplyTo(maxLedgerAmount - 0.5)
	require.ErrorIs(t, err, ErrInvalidAdminBalanceAdjustment)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Equal(t, "INVALID_BALANCE_ADJUSTMENT", infraerrors.Reason(err))
	require.Equal(t, "resulting_balance", infraerrors.FromError(err).Metadata["field"])
}
