package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type temporaryCreditAvailableCreditInvalidatorStub struct {
	calls int
	err   error
}

func (s *temporaryCreditAvailableCreditInvalidatorStub) InvalidateAvailableCredit(context.Context, int64) error {
	s.calls++
	return s.err
}

type temporaryCreditCreateFailureRepo struct {
	TemporaryCreditRepository
	err error
}

func (r *temporaryCreditCreateFailureRepo) CreateGrant(context.Context, TemporaryCreditGrant) (*TemporaryCreditGrant, error) {
	return nil, r.err
}

func TestTemporaryCreditServiceCreateAdminGrantInvalidatesAvailableCreditOnlyAfterPersist(t *testing.T) {
	t.Run("persisted grant invalidates and ignores cache failure", func(t *testing.T) {
		invalidator := &temporaryCreditAvailableCreditInvalidatorStub{err: errors.New("redis unavailable")}
		svc := NewTemporaryCreditServiceWithAvailableCreditInvalidator(&temporaryCreditRepositoryRecorder{}, invalidator)
		svc.now = func() time.Time { return time.Date(2026, 7, 13, 16, 30, 0, 0, time.UTC) }

		grant, err := svc.CreateAdminGrant(context.Background(), 7, 13, 3.5, "manual campaign grant")

		require.NoError(t, err)
		require.NotNil(t, grant)
		require.Equal(t, 1, invalidator.calls)
	})

	t.Run("failed grant does not invalidate", func(t *testing.T) {
		invalidator := &temporaryCreditAvailableCreditInvalidatorStub{}
		svc := NewTemporaryCreditServiceWithAvailableCreditInvalidator(
			&temporaryCreditCreateFailureRepo{err: errors.New("insert failed")},
			invalidator,
		)

		_, err := svc.CreateAdminGrant(context.Background(), 7, 13, 3.5, "manual campaign grant")

		require.Error(t, err)
		require.Equal(t, 0, invalidator.calls)
	})
}
