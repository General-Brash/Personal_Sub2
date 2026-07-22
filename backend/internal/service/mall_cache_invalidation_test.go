package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type mallBalanceInvalidatorStub struct {
	balanceUserIDs   []int64
	availableUserIDs []int64
}

func (s *mallBalanceInvalidatorStub) InvalidateUserBalance(_ context.Context, userID int64) error {
	s.balanceUserIDs = append(s.balanceUserIDs, userID)
	return nil
}

func (s *mallBalanceInvalidatorStub) InvalidateAvailableCredit(_ context.Context, userID int64) error {
	s.availableUserIDs = append(s.availableUserIDs, userID)
	return nil
}

type mallAuthCacheInvalidatorStub struct {
	userIDs []int64
}

func (s *mallAuthCacheInvalidatorStub) InvalidateAuthCacheByKey(context.Context, string) {}

func (s *mallAuthCacheInvalidatorStub) InvalidateAuthCacheByUserID(_ context.Context, userID int64) {
	s.userIDs = append(s.userIDs, userID)
}

func (s *mallAuthCacheInvalidatorStub) InvalidateAuthCacheByGroupID(context.Context, int64) {}

func TestMallServiceInvalidateBalances(t *testing.T) {
	balanceInvalidator := &mallBalanceInvalidatorStub{}
	authInvalidator := &mallAuthCacheInvalidatorStub{}
	svc := NewMallService(nil, NewTemporaryCreditServiceWithAvailableCreditInvalidator(nil, balanceInvalidator), nil)
	svc.SetAuthCacheInvalidator(authInvalidator)

	svc.invalidateMallBalances(context.Background(), 42, true, false)
	require.Equal(t, []int64{42}, balanceInvalidator.balanceUserIDs)
	require.Empty(t, balanceInvalidator.availableUserIDs)
	require.Equal(t, []int64{42}, authInvalidator.userIDs)

	svc.invalidateMallBalances(context.Background(), 43, false, true)
	require.Equal(t, []int64{42}, balanceInvalidator.balanceUserIDs)
	require.Equal(t, []int64{43}, balanceInvalidator.availableUserIDs)
	require.Equal(t, []int64{42}, authInvalidator.userIDs)
}
