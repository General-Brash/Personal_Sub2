//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type userServiceAvailableCreditRepoStub struct {
	UserRepository
	snapshot AvailableCreditSnapshot
	err      error
	calls    int
}

func (s *userServiceAvailableCreditRepoStub) GetAvailableCreditSnapshot(context.Context, int64) (AvailableCreditSnapshot, error) {
	s.calls++
	return s.snapshot, s.err
}

func TestUserServiceGetTemporaryCreditAvailableUsesExistingSnapshotReader(t *testing.T) {
	repo := &userServiceAvailableCreditRepoStub{snapshot: AvailableCreditSnapshot{TemporaryCredit: 4.5}}
	svc := NewUserService(repo, nil, nil, nil)

	available, err := svc.GetTemporaryCreditAvailable(context.Background(), 42)

	require.NoError(t, err)
	require.InDelta(t, 4.5, available, 1e-12)
	require.Equal(t, 1, repo.calls)
}

func TestUserServiceGetTemporaryCreditAvailablePropagatesSnapshotError(t *testing.T) {
	repo := &userServiceAvailableCreditRepoStub{err: errors.New("snapshot unavailable")}
	svc := NewUserService(repo, nil, nil, nil)

	_, err := svc.GetTemporaryCreditAvailable(context.Background(), 42)

	require.ErrorContains(t, err, "get available credit snapshot: snapshot unavailable")
}
