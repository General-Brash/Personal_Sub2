//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// hashedTestValue returns a unique SHA-256 hex string (64 chars) that fits VARCHAR(64) columns.
func hashedTestValue(t *testing.T, prefix string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(uniqueTestValue(t, prefix)))
	return hex.EncodeToString(sum[:])
}

func TestIdempotencyRepo_CreateProcessing_CompeteSameKey(t *testing.T) {
	tx := testTx(t)
	repo := &idempotencyRepository{sql: tx}
	ctx := context.Background()

	now := time.Now().UTC()
	record := &service.IdempotencyRecord{
		Scope:              uniqueTestValue(t, "idem-scope-create"),
		ActorScope:         "user:101",
		IdempotencyKeyHash: hashedTestValue(t, "idem-hash"),
		RequestFingerprint: hashedTestValue(t, "idem-fp"),
		Status:             service.IdempotencyStatusProcessing,
		LockedUntil:        ptrTime(now.Add(30 * time.Second)),
		ExpiresAt:          now.Add(24 * time.Hour),
	}
	owner, err := repo.CreateProcessing(ctx, record)
	require.NoError(t, err)
	require.True(t, owner)
	require.NotZero(t, record.ID)

	duplicate := &service.IdempotencyRecord{
		Scope:              record.Scope,
		ActorScope:         record.ActorScope,
		IdempotencyKeyHash: record.IdempotencyKeyHash,
		RequestFingerprint: hashedTestValue(t, "idem-fp-other"),
		Status:             service.IdempotencyStatusProcessing,
		LockedUntil:        ptrTime(now.Add(30 * time.Second)),
		ExpiresAt:          now.Add(24 * time.Hour),
	}
	owner, err = repo.CreateProcessing(ctx, duplicate)
	require.NoError(t, err)
	require.False(t, owner, "same scope+actor scope+key hash should be de-duplicated")
}

func TestIdempotencyRepo_TryReclaim_StatusAndLockWindow(t *testing.T) {
	tx := testTx(t)
	repo := &idempotencyRepository{sql: tx}
	ctx := context.Background()

	now := time.Now().UTC()
	record := &service.IdempotencyRecord{
		Scope:              uniqueTestValue(t, "idem-scope-reclaim"),
		ActorScope:         "user:102",
		IdempotencyKeyHash: hashedTestValue(t, "idem-hash-reclaim"),
		RequestFingerprint: hashedTestValue(t, "idem-fp-reclaim"),
		Status:             service.IdempotencyStatusProcessing,
		LockedUntil:        ptrTime(now.Add(10 * time.Second)),
		ExpiresAt:          now.Add(24 * time.Hour),
	}
	owner, err := repo.CreateProcessing(ctx, record)
	require.NoError(t, err)
	require.True(t, owner)

	require.NoError(t, repo.MarkFailedRetryable(
		ctx,
		record.ID,
		"RETRYABLE_FAILURE",
		now.Add(-2*time.Second),
		now.Add(24*time.Hour),
	))

	newLockedUntil := now.Add(20 * time.Second)
	reclaimed, err := repo.TryReclaim(
		ctx,
		record.ID,
		service.IdempotencyStatusFailedRetryable,
		now,
		newLockedUntil,
		now.Add(24*time.Hour),
	)
	require.NoError(t, err)
	require.True(t, reclaimed, "failed_retryable + expired lock should allow reclaim")

	got, err := repo.GetByScopeActorScopeAndKeyHash(ctx, record.Scope, record.ActorScope, record.IdempotencyKeyHash)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, service.IdempotencyStatusProcessing, got.Status)
	require.NotNil(t, got.LockedUntil)
	require.True(t, got.LockedUntil.After(now))

	require.NoError(t, repo.MarkFailedRetryable(
		ctx,
		record.ID,
		"RETRYABLE_FAILURE",
		now.Add(20*time.Second),
		now.Add(24*time.Hour),
	))

	reclaimed, err = repo.TryReclaim(
		ctx,
		record.ID,
		service.IdempotencyStatusFailedRetryable,
		now,
		now.Add(40*time.Second),
		now.Add(24*time.Hour),
	)
	require.NoError(t, err)
	require.False(t, reclaimed, "within lock window should not reclaim")
}

func TestIdempotencyRepo_StatusTransition_ToSucceeded(t *testing.T) {
	tx := testTx(t)
	repo := &idempotencyRepository{sql: tx}
	ctx := context.Background()

	now := time.Now().UTC()
	record := &service.IdempotencyRecord{
		Scope:              uniqueTestValue(t, "idem-scope-success"),
		ActorScope:         "user:103",
		IdempotencyKeyHash: hashedTestValue(t, "idem-hash-success"),
		RequestFingerprint: hashedTestValue(t, "idem-fp-success"),
		Status:             service.IdempotencyStatusProcessing,
		LockedUntil:        ptrTime(now.Add(10 * time.Second)),
		ExpiresAt:          now.Add(24 * time.Hour),
	}
	owner, err := repo.CreateProcessing(ctx, record)
	require.NoError(t, err)
	require.True(t, owner)

	require.NoError(t, repo.MarkSucceeded(ctx, record.ID, 200, `{"ok":true}`, now.Add(24*time.Hour)))

	got, err := repo.GetByScopeActorScopeAndKeyHash(ctx, record.Scope, record.ActorScope, record.IdempotencyKeyHash)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, service.IdempotencyStatusSucceeded, got.Status)
	require.NotNil(t, got.ResponseStatus)
	require.Equal(t, 200, *got.ResponseStatus)
	require.NotNil(t, got.ResponseBody)
	require.Equal(t, `{"ok":true}`, *got.ResponseBody)
	require.Nil(t, got.LockedUntil)

	err = repo.MarkFailedRetryable(ctx, record.ID, "LATE_FAILURE", now, now.Add(24*time.Hour))
	require.ErrorIs(t, err, sql.ErrNoRows)
	got, err = repo.GetByScopeActorScopeAndKeyHash(ctx, record.Scope, record.ActorScope, record.IdempotencyKeyHash)
	require.NoError(t, err)
	require.Equal(t, service.IdempotencyStatusSucceeded, got.Status, "late failure must not overwrite succeeded")
}

func TestIdempotencyRepo_TryReclaimOwned_CASFields(t *testing.T) {
	tx := testTx(t)
	repo := &idempotencyRepository{sql: tx}
	ctx := context.Background()
	now := time.Now().UTC()
	expectedLockedUntil := now.Add(-time.Second)
	record := &service.IdempotencyRecord{
		Scope:              uniqueTestValue(t, "idem-owned-cas"),
		ActorScope:         "user:202",
		IdempotencyKeyHash: hashedTestValue(t, "idem-owned-key"),
		RequestFingerprint: hashedTestValue(t, "idem-owned-fp"),
		Status:             service.IdempotencyStatusProcessing,
		LockedUntil:        ptrTime(expectedLockedUntil),
		ExpiresAt:          now.Add(time.Hour),
	}
	created, err := repo.CreateProcessing(ctx, record)
	require.NoError(t, err)
	require.True(t, created)

	wrongLock := expectedLockedUntil.Add(-time.Second)
	for _, tc := range []struct {
		name        string
		actorScope  string
		fingerprint string
		lock        *time.Time
	}{
		{name: "actor", actorScope: "user:other", fingerprint: record.RequestFingerprint, lock: &expectedLockedUntil},
		{name: "payload", actorScope: record.ActorScope, fingerprint: hashedTestValue(t, "other-fp"), lock: &expectedLockedUntil},
		{name: "lock", actorScope: record.ActorScope, fingerprint: record.RequestFingerprint, lock: &wrongLock},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reclaimed, reclaimErr := repo.TryReclaimOwned(
				ctx,
				record.ID,
				service.IdempotencyStatusProcessing,
				tc.actorScope,
				tc.fingerprint,
				tc.lock,
				now,
				now.Add(time.Minute),
				now.Add(time.Hour),
			)
			require.NoError(t, reclaimErr)
			require.False(t, reclaimed)
		})
	}

	newLockedUntil := now.Add(time.Minute)
	reclaimed, err := repo.TryReclaimOwned(
		ctx,
		record.ID,
		service.IdempotencyStatusProcessing,
		record.ActorScope,
		record.RequestFingerprint,
		&expectedLockedUntil,
		now,
		newLockedUntil,
		now.Add(time.Hour),
	)
	require.NoError(t, err)
	require.True(t, reclaimed)

	reclaimed, err = repo.TryReclaimOwned(
		ctx,
		record.ID,
		service.IdempotencyStatusProcessing,
		record.ActorScope,
		record.RequestFingerprint,
		&expectedLockedUntil,
		now,
		now.Add(2*time.Minute),
		now.Add(time.Hour),
	)
	require.NoError(t, err)
	require.False(t, reclaimed, "stale lock token must not reclaim the new owner")

	marked, err := repo.MarkFailedRetryableOwned(
		ctx,
		record.ID,
		record.ActorScope,
		record.RequestFingerprint,
		expectedLockedUntil,
		"STALE_OWNER_FAILURE",
		now.Add(10*time.Second),
		now.Add(time.Hour),
	)
	require.NoError(t, err)
	require.False(t, marked, "stale owner must not fail the reclaimed processing record")
	got, err := repo.GetByScopeActorScopeAndKeyHash(ctx, record.Scope, record.ActorScope, record.IdempotencyKeyHash)
	require.NoError(t, err)
	require.Equal(t, service.IdempotencyStatusProcessing, got.Status)
	require.NotNil(t, got.LockedUntil)
	require.WithinDuration(t, newLockedUntil, *got.LockedUntil, time.Microsecond)
}

func TestIdempotencyRepo_AtomicCommitSucceededButClientErrorDoesNotOverwriteSuccess(t *testing.T) {
	repo := &idempotencyRepository{sql: integrationDB}
	coordinator := service.NewIdempotencyCoordinator(repo, service.DefaultIdempotencyConfig())
	ctx := context.Background()
	key := uniqueTestValue(t, "idem-commit-uncertain")
	opts := service.IdempotencyExecuteOptions{
		Scope:          uniqueTestValue(t, "idem-commit-scope"),
		ActorScope:     "user:303",
		Method:         "POST",
		Route:          "/integration/atomic-commit",
		IdempotencyKey: key,
		Payload:        map[string]any{},
		RequireKey:     true,
	}
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `
			DELETE FROM idempotency_records
			WHERE operation_scope = $1 AND actor_scope = $2 AND idempotency_key_hash = $3
		`, opts.Scope, opts.ActorScope, service.HashIdempotencyKey(key))
	})
	commitErr := errors.New("client observed commit error after server commit")

	_, err := coordinator.ExecuteAtomic(ctx, opts, func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		tx, beginErr := integrationDB.BeginTx(ctx, nil)
		if beginErr != nil {
			return nil, beginErr
		}
		defer func() { _ = tx.Rollback() }()
		data := map[string]any{"ok": true}
		if persistErr := claim.PersistSuccess(ctx, tx, data); persistErr != nil {
			return nil, persistErr
		}
		if commitActualErr := tx.Commit(); commitActualErr != nil {
			return nil, commitActualErr
		}
		return nil, commitErr
	})
	require.ErrorIs(t, err, commitErr)

	stored, err := repo.GetByScopeActorScopeAndKeyHash(ctx, opts.Scope, opts.ActorScope, service.HashIdempotencyKey(key))
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.Equal(t, service.IdempotencyStatusSucceeded, stored.Status)
	require.Nil(t, stored.ErrorReason)
}

func TestIdempotencyRepo_AtomicExpiredProcessingConcurrentSingleOwner(t *testing.T) {
	repo := &idempotencyRepository{sql: integrationDB}
	cfg := service.DefaultIdempotencyConfig()
	cfg.ProcessingTimeout = time.Minute
	coordinator := service.NewIdempotencyCoordinator(repo, cfg)
	ctx := context.Background()
	key := uniqueTestValue(t, "idem-concurrent-key")
	opts := service.IdempotencyExecuteOptions{
		Scope:          uniqueTestValue(t, "idem-concurrent-scope"),
		ActorScope:     "admin:404",
		Method:         "POST",
		Route:          "/integration/atomic-concurrent",
		IdempotencyKey: key,
		Payload:        map[string]any{"amount": "1.00000000"},
		RequireKey:     true,
	}
	fingerprint, err := service.BuildIdempotencyFingerprint(opts.Method, opts.Route, opts.ActorScope, opts.Payload)
	require.NoError(t, err)
	expiredLock := time.Now().UTC().Add(-time.Second)
	seed := &service.IdempotencyRecord{
		Scope:              opts.Scope,
		ActorScope:         opts.ActorScope,
		IdempotencyKeyHash: service.HashIdempotencyKey(key),
		RequestFingerprint: fingerprint,
		Status:             service.IdempotencyStatusProcessing,
		LockedUntil:        &expiredLock,
		ExpiresAt:          time.Now().UTC().Add(time.Hour),
	}
	created, err := repo.CreateProcessing(ctx, seed)
	require.NoError(t, err)
	require.True(t, created)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `
			DELETE FROM idempotency_records
			WHERE operation_scope = $1 AND actor_scope = $2 AND idempotency_key_hash = $3
		`, opts.Scope, opts.ActorScope, service.HashIdempotencyKey(key))
	})

	start := make(chan struct{})
	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	results := make(chan error, 10)
	var ownerOnce sync.Once
	var calls atomic.Int32
	for i := 0; i < 10; i++ {
		go func() {
			<-start
			_, executeErr := coordinator.ExecuteAtomic(ctx, opts, func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
				calls.Add(1)
				ownerOnce.Do(func() { close(ownerStarted) })
				<-releaseOwner
				tx, beginErr := integrationDB.BeginTx(ctx, nil)
				if beginErr != nil {
					return nil, beginErr
				}
				defer func() { _ = tx.Rollback() }()
				data := map[string]any{"ok": true}
				if persistErr := claim.PersistSuccess(ctx, tx, data); persistErr != nil {
					return nil, persistErr
				}
				if commitErr := tx.Commit(); commitErr != nil {
					return nil, commitErr
				}
				return data, nil
			})
			results <- executeErr
		}()
	}
	close(start)
	<-ownerStarted
	for i := 0; i < 9; i++ {
		require.ErrorIs(t, <-results, service.ErrIdempotencyInProgress)
	}
	close(releaseOwner)
	require.NoError(t, <-results)
	require.Equal(t, int32(1), calls.Load())
}
