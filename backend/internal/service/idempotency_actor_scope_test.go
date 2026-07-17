package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdempotencyCoordinator_IsolatesSameKeyAcrossActors(t *testing.T) {
	repo := newInMemoryIdempotencyRepo()
	coordinator := NewIdempotencyCoordinator(repo, DefaultIdempotencyConfig())
	var executions atomic.Int32

	execute := func(context.Context) (any, error) {
		return executions.Add(1), nil
	}
	opts := IdempotencyExecuteOptions{
		Scope:          "user.checkin",
		ActorScope:     "user:101",
		Method:         "POST",
		Route:          "/api/v1/user/check-in",
		IdempotencyKey: "same-key",
		Payload:        map[string]any{},
	}

	first, err := coordinator.Execute(context.Background(), opts, execute)
	require.NoError(t, err)
	require.False(t, first.Replayed)

	opts.ActorScope = "user:202"
	second, err := coordinator.Execute(context.Background(), opts, execute)
	require.NoError(t, err)
	require.False(t, second.Replayed)
	require.Equal(t, int32(2), executions.Load())

	// The original actor must still resolve the original record rather than
	// creating another record for the same operation/key identity.
	opts.ActorScope = "user:101"
	third, err := coordinator.Execute(context.Background(), opts, execute)
	require.NoError(t, err)
	require.True(t, third.Replayed)
	require.Equal(t, int32(2), executions.Load())
}

func TestIdempotencyCoordinator_IsolatesSameKeyAcrossOperations(t *testing.T) {
	repo := newInMemoryIdempotencyRepo()
	coordinator := NewIdempotencyCoordinator(repo, DefaultIdempotencyConfig())
	var executions atomic.Int32

	execute := func(context.Context) (any, error) {
		return executions.Add(1), nil
	}
	opts := IdempotencyExecuteOptions{
		Scope:          "user.checkin",
		ActorScope:     "user:101",
		Method:         "POST",
		Route:          "/api/v1/user/check-in",
		IdempotencyKey: "same-key",
		Payload:        map[string]any{},
	}

	first, err := coordinator.Execute(context.Background(), opts, execute)
	require.NoError(t, err)
	require.False(t, first.Replayed)

	opts.Scope = "admin.temporary-credit.grant"
	opts.Route = "/api/v1/admin/users/101/temporary-credits"
	second, err := coordinator.Execute(context.Background(), opts, execute)
	require.NoError(t, err)
	require.False(t, second.Replayed)
	require.Equal(t, int32(2), executions.Load())
}
