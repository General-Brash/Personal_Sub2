package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type atomicIdempotencySQLResult int64

func (r atomicIdempotencySQLResult) LastInsertId() (int64, error) { return 0, nil }
func (r atomicIdempotencySQLResult) RowsAffected() (int64, error) { return int64(r), nil }

type atomicIdempotencySuccessExecutor struct {
	repo *inMemoryIdempotencyRepo
	err  error
}

func (e *atomicIdempotencySuccessExecutor) ExecContext(ctx context.Context, _ string, args ...any) (sql.Result, error) {
	if e.err != nil {
		return nil, e.err
	}
	e.repo.mu.Lock()
	defer e.repo.mu.Unlock()
	for _, rec := range e.repo.data {
		if rec.ID != args[0].(int64) {
			continue
		}
		if rec.Status != IdempotencyStatusProcessing ||
			rec.RequestFingerprint != args[6].(string) ||
			rec.ActorScope != args[7].(string) ||
			rec.LockedUntil == nil ||
			!rec.LockedUntil.Equal(args[8].(time.Time)) {
			return atomicIdempotencySQLResult(0), nil
		}
		status := args[2].(int)
		body := args[3].(string)
		rec.Status = IdempotencyStatusSucceeded
		rec.ResponseStatus = &status
		rec.ResponseBody = &body
		rec.ErrorReason = nil
		rec.LockedUntil = nil
		rec.ExpiresAt = args[4].(time.Time)
		rec.UpdatedAt = time.Now()
		return atomicIdempotencySQLResult(1), nil
	}
	return atomicIdempotencySQLResult(0), nil
}

func seedAtomicProcessingRecord(
	t *testing.T,
	repo *inMemoryIdempotencyRepo,
	opts IdempotencyExecuteOptions,
	lockedUntil time.Time,
) *IdempotencyRecord {
	t.Helper()
	actorScope := opts.ActorScope
	if actorScope == "" {
		actorScope = "anonymous"
	}
	fingerprint, err := BuildIdempotencyFingerprint(opts.Method, opts.Route, actorScope, opts.Payload)
	require.NoError(t, err)
	record := &IdempotencyRecord{
		Scope:              opts.Scope,
		ActorScope:         actorScope,
		IdempotencyKeyHash: HashIdempotencyKey(opts.IdempotencyKey),
		RequestFingerprint: fingerprint,
		Status:             IdempotencyStatusProcessing,
		LockedUntil:        &lockedUntil,
		ExpiresAt:          time.Now().Add(time.Hour),
	}
	created, err := repo.CreateProcessing(context.Background(), record)
	require.NoError(t, err)
	require.True(t, created)
	return record
}

func TestIdempotencyExecuteAtomicConcurrentRequestAndReplay(t *testing.T) {
	repo := newInMemoryIdempotencyRepo()
	cfg := DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	coordinator := NewIdempotencyCoordinator(repo, cfg)
	opts := IdempotencyExecuteOptions{
		Scope:          "user.daily_checkin.create",
		ActorScope:     "user:42",
		Method:         "POST",
		Route:          "/api/v1/user/check-in",
		IdempotencyKey: "same-key",
		Payload:        map[string]any{},
		RequireKey:     true,
	}
	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	var calls atomic.Int32
	go func() {
		_, err := coordinator.ExecuteAtomic(context.Background(), opts, func(ctx context.Context, claim *IdempotencyAtomicClaim) (any, error) {
			calls.Add(1)
			close(started)
			<-release
			data := map[string]any{"checkin_date": "2026-07-16", "temporary_credit_grant_id": int64(9007199254740993)}
			if err := claim.PersistSuccess(ctx, &atomicIdempotencySuccessExecutor{repo: repo}, data); err != nil {
				return nil, err
			}
			return data, nil
		})
		firstDone <- err
	}()
	<-started

	_, err := coordinator.ExecuteAtomic(context.Background(), opts, func(context.Context, *IdempotencyAtomicClaim) (any, error) {
		calls.Add(1)
		return nil, nil
	})
	require.ErrorIs(t, err, ErrIdempotencyInProgress)
	require.Greater(t, RetryAfterSecondsFromError(err), 0)
	close(release)
	require.NoError(t, <-firstDone)

	replayed, err := coordinator.ExecuteAtomic(context.Background(), opts, func(context.Context, *IdempotencyAtomicClaim) (any, error) {
		calls.Add(1)
		return nil, nil
	})
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	replayedData := replayed.Data.(map[string]any)
	require.Equal(t, json.Number("9007199254740993"), replayedData["temporary_credit_grant_id"])
	require.Equal(t, int32(1), calls.Load())
}

func TestIdempotencyExecuteAtomicFailsClosedWithoutPersistedSuccess(t *testing.T) {
	repo := newInMemoryIdempotencyRepo()
	coordinator := NewIdempotencyCoordinator(repo, DefaultIdempotencyConfig())
	opts := IdempotencyExecuteOptions{
		Scope:          "admin.users.temporary_credits.grant",
		ActorScope:     "admin:99",
		Method:         "POST",
		Route:          "/api/v1/admin/users/:id/temporary-credits",
		IdempotencyKey: "grant-key",
		Payload:        map[string]any{"user_id": 42, "amount": "1.00000000"},
		RequireKey:     true,
	}

	_, err := coordinator.ExecuteAtomic(context.Background(), opts, func(context.Context, *IdempotencyAtomicClaim) (any, error) {
		return map[string]any{"temporary_credit_grant_id": 77}, nil
	})
	require.ErrorIs(t, err, ErrIdempotencyStoreUnavail)
	require.Equal(t, 1, RetryAfterSecondsFromError(err))

	other := opts
	other.IdempotencyKey = "persist-fails"
	_, err = coordinator.ExecuteAtomic(context.Background(), other, func(ctx context.Context, claim *IdempotencyAtomicClaim) (any, error) {
		persistErr := claim.PersistSuccess(ctx, &atomicIdempotencySuccessExecutor{repo: repo, err: errors.New("write failed")}, map[string]any{"ok": true})
		return nil, persistErr
	})
	require.Equal(t, 503, infraerrors.Code(err))
	require.Equal(t, "IDEMPOTENCY_STORE_UNAVAILABLE", infraerrors.Reason(err))
	_, err = coordinator.ExecuteAtomic(context.Background(), other, func(context.Context, *IdempotencyAtomicClaim) (any, error) {
		return nil, nil
	})
	require.ErrorIs(t, err, ErrIdempotencyInProgress)
	require.Equal(t, "IDEMPOTENCY_IN_PROGRESS", infraerrors.Reason(err))
	require.Greater(t, RetryAfterSecondsFromError(err), 0)
}

func TestIdempotencyExecuteAtomicRequiresKeyEvenInObserveOnlyMode(t *testing.T) {
	cfg := DefaultIdempotencyConfig()
	cfg.ObserveOnly = true
	coordinator := NewIdempotencyCoordinator(newInMemoryIdempotencyRepo(), cfg)
	_, err := coordinator.ExecuteAtomic(context.Background(), IdempotencyExecuteOptions{
		Scope:      "user.daily_checkin.create",
		ActorScope: "user:42",
		RequireKey: true,
	}, func(context.Context, *IdempotencyAtomicClaim) (any, error) {
		return nil, nil
	})
	require.ErrorIs(t, err, ErrIdempotencyKeyRequired)
}

func TestIdempotencyExecuteDoesNotReclaimExpiredProcessingLock(t *testing.T) {
	repo := newInMemoryIdempotencyRepo()
	coordinator := NewIdempotencyCoordinator(repo, DefaultIdempotencyConfig())
	opts := IdempotencyExecuteOptions{
		Scope:          "legacy.non_atomic.write",
		ActorScope:     "user:42",
		Method:         "POST",
		Route:          "/legacy/write",
		IdempotencyKey: "expired-processing",
		Payload:        map[string]any{"amount": 1},
		RequireKey:     true,
	}
	seedAtomicProcessingRecord(t, repo, opts, time.Now().Add(-time.Second))

	var calls atomic.Int32
	_, err := coordinator.Execute(context.Background(), opts, func(context.Context) (any, error) {
		calls.Add(1)
		return map[string]any{"ok": true}, nil
	})

	require.ErrorIs(t, err, ErrIdempotencyInProgress)
	require.Equal(t, int32(0), calls.Load())
}

func TestIdempotencyExecuteAtomicReclaimsOnlyExpiredProcessingLock(t *testing.T) {
	for _, tc := range []struct {
		name       string
		lockOffset time.Duration
		wantErr    error
		wantCalls  int32
	}{
		{name: "active lock", lockOffset: time.Minute, wantErr: ErrIdempotencyInProgress},
		{name: "expired lock", lockOffset: -time.Second, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newInMemoryIdempotencyRepo()
			coordinator := NewIdempotencyCoordinator(repo, DefaultIdempotencyConfig())
			opts := IdempotencyExecuteOptions{
				Scope:          "atomic.processing.reclaim",
				ActorScope:     "admin:7",
				Method:         "POST",
				Route:          "/atomic/write",
				IdempotencyKey: "processing-key",
				Payload:        map[string]any{"amount": "1.00000000"},
				RequireKey:     true,
			}
			seedAtomicProcessingRecord(t, repo, opts, time.Now().Add(tc.lockOffset))
			var calls atomic.Int32
			_, err := coordinator.ExecuteAtomic(context.Background(), opts, func(ctx context.Context, claim *IdempotencyAtomicClaim) (any, error) {
				calls.Add(1)
				data := map[string]any{"ok": true}
				if persistErr := claim.PersistSuccess(ctx, &atomicIdempotencySuccessExecutor{repo: repo}, data); persistErr != nil {
					return nil, persistErr
				}
				return data, nil
			})
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantCalls, calls.Load())
		})
	}
}

func TestIdempotencyExecuteAtomicExpiredProcessingConcurrentSingleOwner(t *testing.T) {
	repo := newInMemoryIdempotencyRepo()
	cfg := DefaultIdempotencyConfig()
	cfg.ProcessingTimeout = time.Minute
	coordinator := NewIdempotencyCoordinator(repo, cfg)
	opts := IdempotencyExecuteOptions{
		Scope:          "atomic.processing.concurrent",
		ActorScope:     "user:42",
		Method:         "POST",
		Route:          "/atomic/concurrent",
		IdempotencyKey: "same-expired-key",
		Payload:        map[string]any{},
		RequireKey:     true,
	}
	seedAtomicProcessingRecord(t, repo, opts, time.Now().Add(-time.Second))

	start := make(chan struct{})
	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	results := make(chan error, 10)
	var ownerOnce sync.Once
	var calls atomic.Int32
	for i := 0; i < 10; i++ {
		go func() {
			<-start
			_, err := coordinator.ExecuteAtomic(context.Background(), opts, func(ctx context.Context, claim *IdempotencyAtomicClaim) (any, error) {
				calls.Add(1)
				ownerOnce.Do(func() { close(ownerStarted) })
				<-releaseOwner
				data := map[string]any{"ok": true}
				if persistErr := claim.PersistSuccess(ctx, &atomicIdempotencySuccessExecutor{repo: repo}, data); persistErr != nil {
					return nil, persistErr
				}
				return data, nil
			})
			results <- err
		}()
	}
	close(start)
	<-ownerStarted
	for i := 0; i < 9; i++ {
		require.ErrorIs(t, <-results, ErrIdempotencyInProgress)
	}
	close(releaseOwner)
	require.NoError(t, <-results)
	require.Equal(t, int32(1), calls.Load())
}

func TestIdempotencyExecuteAtomicLateCommitErrorDoesNotOverwriteSuccess(t *testing.T) {
	repo := newInMemoryIdempotencyRepo()
	coordinator := NewIdempotencyCoordinator(repo, DefaultIdempotencyConfig())
	opts := IdempotencyExecuteOptions{
		Scope:          "atomic.commit.uncertain",
		ActorScope:     "user:42",
		Method:         "POST",
		Route:          "/atomic/commit",
		IdempotencyKey: "commit-uncertain",
		Payload:        map[string]any{},
		RequireKey:     true,
	}
	commitErr := errors.New("client observed commit error after server commit")

	_, err := coordinator.ExecuteAtomic(context.Background(), opts, func(ctx context.Context, claim *IdempotencyAtomicClaim) (any, error) {
		data := map[string]any{"ok": true}
		require.NoError(t, claim.PersistSuccess(ctx, &atomicIdempotencySuccessExecutor{repo: repo}, data))
		return nil, commitErr
	})
	require.ErrorIs(t, err, commitErr)

	stored, getErr := repo.GetByScopeActorScopeAndKeyHash(context.Background(), opts.Scope, opts.ActorScope, HashIdempotencyKey(opts.IdempotencyKey))
	require.NoError(t, getErr)
	require.NotNil(t, stored)
	require.Equal(t, IdempotencyStatusSucceeded, stored.Status)
}

func TestIdempotencyExecuteAtomicReclaimFencesStaleOwnerSuccess(t *testing.T) {
	repo := newInMemoryIdempotencyRepo()
	cfg := DefaultIdempotencyConfig()
	cfg.ProcessingTimeout = time.Minute
	coordinator := NewIdempotencyCoordinator(repo, cfg)
	opts := IdempotencyExecuteOptions{
		Scope:          "atomic.owner.fencing",
		ActorScope:     "user:42",
		Method:         "POST",
		Route:          "/atomic/fencing",
		IdempotencyKey: "stale-owner",
		Payload:        map[string]any{},
		RequireKey:     true,
	}
	oldLockedUntil := time.Now().Add(-time.Second)
	record := seedAtomicProcessingRecord(t, repo, opts, oldLockedUntil)
	staleClaim := &IdempotencyAtomicClaim{
		coordinator:        coordinator,
		recordID:           record.ID,
		actorScope:         record.ActorScope,
		requestFingerprint: record.RequestFingerprint,
		lockedUntil:        oldLockedUntil,
		expiresAt:          record.ExpiresAt,
	}

	newOwnerStarted := make(chan struct{})
	releaseNewOwner := make(chan struct{})
	newOwnerDone := make(chan error, 1)
	go func() {
		_, err := coordinator.ExecuteAtomic(context.Background(), opts, func(ctx context.Context, claim *IdempotencyAtomicClaim) (any, error) {
			close(newOwnerStarted)
			<-releaseNewOwner
			data := map[string]any{"owner": "new"}
			if persistErr := claim.PersistSuccess(ctx, &atomicIdempotencySuccessExecutor{repo: repo}, data); persistErr != nil {
				return nil, persistErr
			}
			return data, nil
		})
		newOwnerDone <- err
	}()
	<-newOwnerStarted

	err := staleClaim.PersistSuccess(context.Background(), &atomicIdempotencySuccessExecutor{repo: repo}, map[string]any{"owner": "stale"})
	require.Equal(t, infraerrors.Code(ErrIdempotencyStoreUnavail), infraerrors.Code(err))
	close(releaseNewOwner)
	require.NoError(t, <-newOwnerDone)

	stored, getErr := repo.GetByScopeActorScopeAndKeyHash(context.Background(), opts.Scope, opts.ActorScope, HashIdempotencyKey(opts.IdempotencyKey))
	require.NoError(t, getErr)
	require.NotNil(t, stored.ResponseBody)
	require.JSONEq(t, `{"owner":"new"}`, *stored.ResponseBody)
}
