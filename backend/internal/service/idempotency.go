package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

const (
	IdempotencyStatusProcessing      = "processing"
	IdempotencyStatusSucceeded       = "succeeded"
	IdempotencyStatusFailedRetryable = "failed_retryable"
)

var (
	ErrIdempotencyKeyRequired    = infraerrors.BadRequest("IDEMPOTENCY_KEY_REQUIRED", "idempotency key is required")
	ErrIdempotencyKeyInvalid     = infraerrors.BadRequest("IDEMPOTENCY_KEY_INVALID", "idempotency key is invalid")
	ErrIdempotencyKeyConflict    = infraerrors.Conflict("IDEMPOTENCY_KEY_CONFLICT", "idempotency key reused with different payload")
	ErrIdempotencyInProgress     = infraerrors.Conflict("IDEMPOTENCY_IN_PROGRESS", "idempotent request is still processing")
	ErrIdempotencyRetryBackoff   = infraerrors.Conflict("IDEMPOTENCY_RETRY_BACKOFF", "idempotent request is in retry backoff window")
	ErrIdempotencyStoreUnavail   = infraerrors.ServiceUnavailable("IDEMPOTENCY_STORE_UNAVAILABLE", "idempotency store unavailable").WithMetadata(map[string]string{"retry_after": "1"})
	ErrIdempotencyInvalidPayload = infraerrors.BadRequest("IDEMPOTENCY_PAYLOAD_INVALID", "failed to normalize request payload")
)

type IdempotencyRecord struct {
	ID int64
	// Scope is the application-level operation scope stored in operation_scope.
	Scope              string
	ActorScope         string
	IdempotencyKeyHash string
	RequestFingerprint string
	Status             string
	ResponseStatus     *int
	ResponseBody       *string
	ErrorReason        *string
	LockedUntil        *time.Time
	ExpiresAt          time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type IdempotencyRepository interface {
	CreateProcessing(ctx context.Context, record *IdempotencyRecord) (bool, error)
	GetByScopeActorScopeAndKeyHash(ctx context.Context, scope, actorScope, keyHash string) (*IdempotencyRecord, error)
	TryReclaim(ctx context.Context, id int64, fromStatus string, now, newLockedUntil, newExpiresAt time.Time) (bool, error)
	ExtendProcessingLock(ctx context.Context, id int64, requestFingerprint string, newLockedUntil, newExpiresAt time.Time) (bool, error)
	MarkSucceeded(ctx context.Context, id int64, responseStatus int, responseBody string, expiresAt time.Time) error
	MarkFailedRetryable(ctx context.Context, id int64, errorReason string, lockedUntil, expiresAt time.Time) error
	DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error)
}

type idempotencyOwnedCASRepository interface {
	TryReclaimOwned(
		ctx context.Context,
		id int64,
		fromStatus, actorScope, requestFingerprint string,
		expectedLockedUntil *time.Time,
		now, newLockedUntil, newExpiresAt time.Time,
	) (bool, error)
	MarkFailedRetryableOwned(
		ctx context.Context,
		id int64,
		actorScope, requestFingerprint string,
		expectedLockedUntil time.Time,
		errorReason string,
		lockedUntil, expiresAt time.Time,
	) (bool, error)
}

type IdempotencyConfig struct {
	DefaultTTL           time.Duration
	SystemOperationTTL   time.Duration
	ProcessingTimeout    time.Duration
	FailedRetryBackoff   time.Duration
	MaxStoredResponseLen int
	ObserveOnly          bool
}

func DefaultIdempotencyConfig() IdempotencyConfig {
	return IdempotencyConfig{
		DefaultTTL:           24 * time.Hour,
		SystemOperationTTL:   1 * time.Hour,
		ProcessingTimeout:    30 * time.Second,
		FailedRetryBackoff:   5 * time.Second,
		MaxStoredResponseLen: 64 * 1024,
		ObserveOnly:          true, // 默认先观察再强制，避免老客户端立刻中断
	}
}

type IdempotencyExecuteOptions struct {
	Scope          string
	ActorScope     string
	Method         string
	Route          string
	IdempotencyKey string
	Payload        any
	TTL            time.Duration
	RequireKey     bool
	// ReturnCommittedDataOnSuccessPersistenceFailure is an explicit opt-in for
	// business operations whose own transaction has committed before the
	// idempotency response can be stored. The result carries PersistencePending
	// so callers can disclose the recovery state instead of reporting failure.
	ReturnCommittedDataOnSuccessPersistenceFailure bool
}

type IdempotencyExecuteResult struct {
	Data               any
	Replayed           bool
	PersistencePending bool
}

// IdempotencyAtomicSuccessExecutor is implemented by *sql.Tx. It lets the
// business transaction persist the stable success DTO without opening a
// second transaction in the idempotency layer.
type IdempotencyAtomicSuccessExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// IdempotencyAtomicClaim represents an owned processing record. Callers must
// persist success through PersistSuccess before their business transaction is
// committed.
type IdempotencyAtomicClaim struct {
	coordinator        *IdempotencyCoordinator
	recordID           int64
	actorScope         string
	requestFingerprint string
	lockedUntil        time.Time
	expiresAt          time.Time
	persisted          bool
}

// PersistSuccess stores the exact success DTO using the caller's transaction.
func (c *IdempotencyAtomicClaim) PersistSuccess(ctx context.Context, executor IdempotencyAtomicSuccessExecutor, data any) error {
	if c == nil || c.coordinator == nil || c.recordID <= 0 || executor == nil || c.persisted {
		return ErrIdempotencyStoreUnavail
	}
	body, err := c.coordinator.marshalAtomicStoredResponse(data)
	if err != nil {
		return ErrIdempotencyStoreUnavail.WithCause(err)
	}
	result, err := executor.ExecContext(ctx, `
UPDATE idempotency_records
SET status = $2,
    response_status = $3,
    response_body = $4,
    error_reason = NULL,
    locked_until = NULL,
    expires_at = $5,
    updated_at = clock_timestamp()
WHERE id = $1
  AND status = $6
  AND request_fingerprint = $7
  AND actor_scope = $8
  AND locked_until IS NOT DISTINCT FROM $9`,
		c.recordID,
		IdempotencyStatusSucceeded,
		200,
		body,
		c.expiresAt,
		IdempotencyStatusProcessing,
		c.requestFingerprint,
		c.actorScope,
		c.lockedUntil,
	)
	if err != nil {
		return ErrIdempotencyStoreUnavail.WithCause(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ErrIdempotencyStoreUnavail.WithCause(err)
	}
	if affected != 1 {
		return ErrIdempotencyStoreUnavail
	}
	c.persisted = true
	return nil
}

type IdempotencyCoordinator struct {
	repo IdempotencyRepository
	cfg  IdempotencyConfig
}

var (
	defaultIdempotencyMu  sync.RWMutex
	defaultIdempotencySvc *IdempotencyCoordinator
)

func SetDefaultIdempotencyCoordinator(svc *IdempotencyCoordinator) {
	defaultIdempotencyMu.Lock()
	defaultIdempotencySvc = svc
	defaultIdempotencyMu.Unlock()
}

func DefaultIdempotencyCoordinator() *IdempotencyCoordinator {
	defaultIdempotencyMu.RLock()
	defer defaultIdempotencyMu.RUnlock()
	return defaultIdempotencySvc
}

func DefaultWriteIdempotencyTTL() time.Duration {
	defaultTTL := DefaultIdempotencyConfig().DefaultTTL
	if coordinator := DefaultIdempotencyCoordinator(); coordinator != nil && coordinator.cfg.DefaultTTL > 0 {
		return coordinator.cfg.DefaultTTL
	}
	return defaultTTL
}

func DefaultSystemOperationIdempotencyTTL() time.Duration {
	defaultTTL := DefaultIdempotencyConfig().SystemOperationTTL
	if coordinator := DefaultIdempotencyCoordinator(); coordinator != nil && coordinator.cfg.SystemOperationTTL > 0 {
		return coordinator.cfg.SystemOperationTTL
	}
	return defaultTTL
}

func NewIdempotencyCoordinator(repo IdempotencyRepository, cfg IdempotencyConfig) *IdempotencyCoordinator {
	return &IdempotencyCoordinator{
		repo: repo,
		cfg:  cfg,
	}
}

func NormalizeIdempotencyKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", nil
	}
	if len(key) > 128 {
		return "", ErrIdempotencyKeyInvalid
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return "", ErrIdempotencyKeyInvalid
		}
	}
	return key, nil
}

func HashIdempotencyKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func BuildIdempotencyFingerprint(method, route, actorScope string, payload any) (string, error) {
	if method == "" {
		method = "POST"
	}
	if route == "" {
		route = "/"
	}
	if actorScope == "" {
		actorScope = "anonymous"
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", ErrIdempotencyInvalidPayload.WithCause(err)
	}
	sum := sha256.Sum256([]byte(
		strings.ToUpper(method) + "\n" + route + "\n" + actorScope + "\n" + string(raw),
	))
	return hex.EncodeToString(sum[:]), nil
}

func RetryAfterSecondsFromError(err error) int {
	appErr := new(infraerrors.ApplicationError)
	if !errors.As(err, &appErr) || appErr == nil || appErr.Metadata == nil {
		return 0
	}
	v := strings.TrimSpace(appErr.Metadata["retry_after"])
	if v == "" {
		return 0
	}
	seconds, convErr := strconv.Atoi(v)
	if convErr != nil || seconds <= 0 {
		return 0
	}
	return seconds
}

func (c *IdempotencyCoordinator) Execute(
	ctx context.Context,
	opts IdempotencyExecuteOptions,
	execute func(context.Context) (any, error),
) (*IdempotencyExecuteResult, error) {
	return c.execute(ctx, opts, execute, nil)
}

// ExecuteAtomic claims/replays a request like Execute, but delegates success
// persistence to the business transaction through IdempotencyAtomicClaim.
func (c *IdempotencyCoordinator) ExecuteAtomic(
	ctx context.Context,
	opts IdempotencyExecuteOptions,
	execute func(context.Context, *IdempotencyAtomicClaim) (any, error),
) (*IdempotencyExecuteResult, error) {
	return c.execute(ctx, opts, nil, execute)
}

func (c *IdempotencyCoordinator) execute(
	ctx context.Context,
	opts IdempotencyExecuteOptions,
	execute func(context.Context) (any, error),
	executeAtomic func(context.Context, *IdempotencyAtomicClaim) (any, error),
) (*IdempotencyExecuteResult, error) {
	if execute == nil && executeAtomic == nil {
		return nil, infraerrors.InternalServer("IDEMPOTENCY_EXECUTOR_NIL", "idempotency executor is nil")
	}

	key, err := NormalizeIdempotencyKey(opts.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if key == "" {
		if executeAtomic != nil || (opts.RequireKey && !c.cfg.ObserveOnly) {
			return nil, ErrIdempotencyKeyRequired
		}
		data, execErr := execute(ctx)
		if execErr != nil {
			return nil, execErr
		}
		return &IdempotencyExecuteResult{Data: data}, nil
	}
	if c.repo == nil {
		RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "repo_nil")
		return nil, ErrIdempotencyStoreUnavail
	}

	if opts.Scope == "" {
		return nil, infraerrors.BadRequest("IDEMPOTENCY_SCOPE_REQUIRED", "idempotency scope is required")
	}

	actorScope := strings.TrimSpace(opts.ActorScope)
	if actorScope == "" {
		actorScope = "anonymous"
	}

	fingerprint, err := BuildIdempotencyFingerprint(opts.Method, opts.Route, actorScope, opts.Payload)
	if err != nil {
		return nil, err
	}

	ttl := opts.TTL
	if ttl <= 0 {
		ttl = c.cfg.DefaultTTL
	}
	now := time.Now()
	expiresAt := now.Add(ttl)
	lockedUntil := now.Add(c.cfg.ProcessingTimeout)
	keyHash := HashIdempotencyKey(key)

	record := &IdempotencyRecord{
		Scope:              opts.Scope,
		ActorScope:         actorScope,
		IdempotencyKeyHash: keyHash,
		RequestFingerprint: fingerprint,
		Status:             IdempotencyStatusProcessing,
		LockedUntil:        &lockedUntil,
		ExpiresAt:          expiresAt,
	}

	owner, err := c.repo.CreateProcessing(ctx, record)
	if err != nil {
		RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "create_processing_error")
		logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "unknown->store_unavailable", false, map[string]string{
			"operation": "create_processing",
		})
		return nil, ErrIdempotencyStoreUnavail.WithCause(err)
	}
	if owner {
		recordIdempotencyClaim(opts.Route, opts.Scope, map[string]string{"mode": "new_claim"})
		logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "none->processing", false, map[string]string{
			"claim_mode": "new",
		})
	}
	if !owner {
		existing, getErr := c.repo.GetByScopeActorScopeAndKeyHash(ctx, opts.Scope, actorScope, keyHash)
		if getErr != nil {
			RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "get_existing_error")
			logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "unknown->store_unavailable", false, map[string]string{
				"operation": "get_existing",
			})
			return nil, ErrIdempotencyStoreUnavail.WithCause(getErr)
		}
		if existing == nil {
			RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "missing_existing")
			logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "unknown->store_unavailable", false, map[string]string{
				"operation": "missing_existing",
			})
			return nil, ErrIdempotencyStoreUnavail
		}
		if existing.RequestFingerprint != fingerprint {
			recordIdempotencyConflict(opts.Route, opts.Scope, map[string]string{"reason": "fingerprint_mismatch"})
			logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "existing->fingerprint_mismatch", false, nil)
			return nil, ErrIdempotencyKeyConflict
		}
		reclaimed := false
		if existing.Status == IdempotencyStatusProcessing {
			if executeAtomic == nil || (existing.LockedUntil != nil && existing.LockedUntil.After(now)) {
				recordIdempotencyConflict(opts.Route, opts.Scope, map[string]string{"reason": "in_progress"})
				logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->conflict", false, nil)
				return nil, c.conflictWithRetryAfter(ErrIdempotencyInProgress, existing.LockedUntil, now)
			}
			taken, reclaimErr := c.tryReclaim(ctx, existing, now, lockedUntil, expiresAt, true)
			if reclaimErr != nil {
				RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "try_reclaim_processing_error")
				logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->store_unavailable", false, map[string]string{
					"operation": "try_reclaim_processing",
				})
				return nil, ErrIdempotencyStoreUnavail.WithCause(reclaimErr)
			}
			if !taken {
				recordIdempotencyConflict(opts.Route, opts.Scope, map[string]string{"reason": "reclaim_race"})
				logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->conflict", false, map[string]string{
					"conflict": "reclaim_race",
				})
				return nil, c.conflictWithRetryAfter(ErrIdempotencyInProgress, existing.LockedUntil, now)
			}
			reclaimed = true
			recordIdempotencyClaim(opts.Route, opts.Scope, map[string]string{"mode": "processing_timeout_reclaim"})
			logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->processing", false, map[string]string{
				"claim_mode": "processing_timeout_reclaim",
			})
			record.ID = existing.ID
			record.LockedUntil = &lockedUntil
		} else if !existing.ExpiresAt.After(now) {
			taken, reclaimErr := c.tryReclaim(ctx, existing, now, lockedUntil, expiresAt, false)
			if reclaimErr != nil {
				RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "try_reclaim_expired_error")
				logIdempotencyAudit(opts.Route, opts.Scope, keyHash, existing.Status+"->store_unavailable", false, map[string]string{
					"operation": "try_reclaim_expired",
				})
				return nil, ErrIdempotencyStoreUnavail.WithCause(reclaimErr)
			}
			if taken {
				reclaimed = true
				recordIdempotencyClaim(opts.Route, opts.Scope, map[string]string{"mode": "expired_reclaim"})
				logIdempotencyAudit(opts.Route, opts.Scope, keyHash, existing.Status+"->processing", false, map[string]string{
					"claim_mode": "expired_reclaim",
				})
				record.ID = existing.ID
				record.LockedUntil = &lockedUntil
			} else {
				latest, latestErr := c.repo.GetByScopeActorScopeAndKeyHash(ctx, opts.Scope, actorScope, keyHash)
				if latestErr != nil {
					RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "get_existing_after_expired_reclaim_error")
					logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "unknown->store_unavailable", false, map[string]string{
						"operation": "get_existing_after_expired_reclaim",
					})
					return nil, ErrIdempotencyStoreUnavail.WithCause(latestErr)
				}
				if latest == nil {
					RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "missing_existing_after_expired_reclaim")
					logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "unknown->store_unavailable", false, map[string]string{
						"operation": "missing_existing_after_expired_reclaim",
					})
					return nil, ErrIdempotencyStoreUnavail
				}
				if latest.RequestFingerprint != fingerprint {
					recordIdempotencyConflict(opts.Route, opts.Scope, map[string]string{"reason": "fingerprint_mismatch"})
					logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "existing->fingerprint_mismatch", false, nil)
					return nil, ErrIdempotencyKeyConflict
				}
				existing = latest
			}
		}

		if !reclaimed {
			switch existing.Status {
			case IdempotencyStatusSucceeded:
				decode := c.decodeStoredResponse
				if executeAtomic != nil {
					decode = c.decodeAtomicStoredResponse
				}
				data, parseErr := decode(existing.ResponseBody)
				if parseErr != nil {
					RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "decode_stored_response_error")
					logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "succeeded->store_unavailable", false, map[string]string{
						"operation": "decode_stored_response",
					})
					return nil, ErrIdempotencyStoreUnavail.WithCause(parseErr)
				}
				recordIdempotencyReplay(opts.Route, opts.Scope, nil)
				logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "succeeded->replayed", true, nil)
				return &IdempotencyExecuteResult{Data: data, Replayed: true}, nil
			case IdempotencyStatusFailedRetryable:
				if existing.LockedUntil != nil && existing.LockedUntil.After(now) {
					recordIdempotencyConflict(opts.Route, opts.Scope, map[string]string{"reason": "retry_backoff"})
					recordIdempotencyRetryBackoff(opts.Route, opts.Scope, nil)
					logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "failed_retryable->retry_backoff_conflict", false, nil)
					conflict := ErrIdempotencyRetryBackoff
					if executeAtomic != nil {
						conflict = ErrIdempotencyInProgress
					}
					return nil, c.conflictWithRetryAfter(conflict, existing.LockedUntil, now)
				}
				taken, reclaimErr := c.tryReclaim(ctx, existing, now, lockedUntil, expiresAt, false)
				if reclaimErr != nil {
					RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "try_reclaim_error")
					logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "failed_retryable->store_unavailable", false, map[string]string{
						"operation": "try_reclaim",
					})
					return nil, ErrIdempotencyStoreUnavail.WithCause(reclaimErr)
				}
				if !taken {
					recordIdempotencyConflict(opts.Route, opts.Scope, map[string]string{"reason": "reclaim_race"})
					logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "failed_retryable->conflict", false, map[string]string{
						"conflict": "reclaim_race",
					})
					return nil, c.conflictWithRetryAfter(ErrIdempotencyInProgress, existing.LockedUntil, now)
				}
				recordIdempotencyClaim(opts.Route, opts.Scope, map[string]string{"mode": "reclaim"})
				logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "failed_retryable->processing", false, map[string]string{
					"claim_mode": "reclaim",
				})
				record.ID = existing.ID
				record.LockedUntil = &lockedUntil
			default:
				recordIdempotencyConflict(opts.Route, opts.Scope, map[string]string{"reason": "unexpected_status"})
				logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "existing->conflict", false, map[string]string{
					"status": existing.Status,
				})
				return nil, ErrIdempotencyKeyConflict
			}
		}
	}

	if record.ID == 0 {
		RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "record_id_missing")
		logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->store_unavailable", false, map[string]string{
			"operation": "record_id_missing",
		})
		return nil, ErrIdempotencyStoreUnavail
	}

	execStart := time.Now()
	defer func() {
		recordIdempotencyProcessingDuration(opts.Route, opts.Scope, time.Since(execStart), nil)
	}()

	var atomicClaim *IdempotencyAtomicClaim
	var data any
	var execErr error
	if executeAtomic != nil {
		if record.LockedUntil == nil {
			RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "atomic_owner_lock_missing")
			return nil, ErrIdempotencyStoreUnavail
		}
		atomicClaim = &IdempotencyAtomicClaim{
			coordinator:        c,
			recordID:           record.ID,
			actorScope:         record.ActorScope,
			requestFingerprint: record.RequestFingerprint,
			lockedUntil:        *record.LockedUntil,
			expiresAt:          expiresAt,
		}
		data, execErr = executeAtomic(ctx, atomicClaim)
	} else {
		data, execErr = execute(ctx)
	}
	if execErr != nil {
		backoffUntil := time.Now().Add(c.cfg.FailedRetryBackoff)
		reason := infraerrors.Reason(execErr)
		if reason == "" {
			reason = "EXECUTION_FAILED"
		}
		if executeAtomic != nil {
			ownedRepo, ok := c.repo.(idempotencyOwnedCASRepository)
			if !ok {
				RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "mark_atomic_failed_unsupported")
				return nil, execErr
			}
			marked, markErr := ownedRepo.MarkFailedRetryableOwned(
				ctx,
				record.ID,
				record.ActorScope,
				record.RequestFingerprint,
				atomicClaim.lockedUntil,
				reason,
				backoffUntil,
				expiresAt,
			)
			if markErr != nil {
				RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "mark_atomic_failed_retryable_error")
				logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->store_unavailable", false, map[string]string{
					"operation": "mark_atomic_failed_retryable",
				})
			} else if marked {
				recordIdempotencyRetryBackoff(opts.Route, opts.Scope, nil)
				logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->failed_retryable", false, map[string]string{
					"reason": reason,
				})
			} else {
				logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->owner_lost", false, nil)
			}
			return nil, execErr
		}
		recordIdempotencyRetryBackoff(opts.Route, opts.Scope, nil)
		logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->failed_retryable", false, map[string]string{
			"reason": reason,
		})
		if markErr := c.repo.MarkFailedRetryable(ctx, record.ID, reason, backoffUntil, expiresAt); markErr != nil {
			RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "mark_failed_retryable_error")
			logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->store_unavailable", false, map[string]string{
				"operation": "mark_failed_retryable",
			})
		}
		return nil, execErr
	}
	if executeAtomic != nil {
		if atomicClaim == nil || !atomicClaim.persisted {
			RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "atomic_success_not_persisted")
			return nil, ErrIdempotencyStoreUnavail
		}
		logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->succeeded", false, nil)
		return &IdempotencyExecuteResult{Data: data}, nil
	}

	storedBody, marshalErr := c.marshalStoredResponse(data)
	if marshalErr != nil {
		RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "marshal_response_error")
		logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->store_unavailable", false, map[string]string{
			"operation": "marshal_response",
		})
		if opts.ReturnCommittedDataOnSuccessPersistenceFailure {
			return &IdempotencyExecuteResult{Data: data, PersistencePending: true}, nil
		}
		return nil, ErrIdempotencyStoreUnavail.WithCause(marshalErr)
	}
	if markErr := c.repo.MarkSucceeded(ctx, record.ID, 200, storedBody, expiresAt); markErr != nil {
		RecordIdempotencyStoreUnavailable(opts.Route, opts.Scope, "mark_succeeded_error")
		logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->store_unavailable", false, map[string]string{
			"operation": "mark_succeeded",
		})
		// MarkSucceeded may have reached the database before returning an error.
		// Do not overwrite it with failed_retryable. The non-atomic path cannot
		// safely reclaim an uncertain processing record and execute business again.
		if opts.ReturnCommittedDataOnSuccessPersistenceFailure {
			return &IdempotencyExecuteResult{Data: data, PersistencePending: true}, nil
		}
		return nil, ErrIdempotencyStoreUnavail.WithCause(markErr)
	}
	logIdempotencyAudit(opts.Route, opts.Scope, keyHash, "processing->succeeded", false, nil)

	return &IdempotencyExecuteResult{Data: data}, nil
}

func (c *IdempotencyCoordinator) tryReclaim(
	ctx context.Context,
	existing *IdempotencyRecord,
	now, newLockedUntil, newExpiresAt time.Time,
	requireOwnedCAS bool,
) (bool, error) {
	if ownedRepo, ok := c.repo.(idempotencyOwnedCASRepository); ok {
		return ownedRepo.TryReclaimOwned(
			ctx,
			existing.ID,
			existing.Status,
			existing.ActorScope,
			existing.RequestFingerprint,
			existing.LockedUntil,
			now,
			newLockedUntil,
			newExpiresAt,
		)
	}
	if requireOwnedCAS {
		return false, nil
	}
	return c.repo.TryReclaim(ctx, existing.ID, existing.Status, now, newLockedUntil, newExpiresAt)
}

func (c *IdempotencyCoordinator) marshalAtomicStoredResponse(data any) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	if c.cfg.MaxStoredResponseLen > 0 && len(raw) > c.cfg.MaxStoredResponseLen {
		return "", fmt.Errorf("idempotency response exceeds %d bytes", c.cfg.MaxStoredResponseLen)
	}
	return string(raw), nil
}

func (c *IdempotencyCoordinator) conflictWithRetryAfter(base *infraerrors.ApplicationError, lockedUntil *time.Time, now time.Time) error {
	if lockedUntil == nil {
		return base
	}
	sec := int(lockedUntil.Sub(now).Seconds())
	if sec <= 0 {
		sec = 1
	}
	return base.WithMetadata(map[string]string{"retry_after": strconv.Itoa(sec)})
}

func (c *IdempotencyCoordinator) marshalStoredResponse(data any) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	redacted := logredact.RedactText(string(raw))
	if c.cfg.MaxStoredResponseLen > 0 && len(redacted) > c.cfg.MaxStoredResponseLen {
		redacted = truncateUTF8(redacted, c.cfg.MaxStoredResponseLen) + "...(truncated)"
	}
	return redacted, nil
}

func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.ValidString(s[:maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

func (c *IdempotencyCoordinator) decodeStoredResponse(stored *string) (any, error) {
	if stored == nil || strings.TrimSpace(*stored) == "" {
		return map[string]any{}, nil
	}
	var out any
	if err := json.Unmarshal([]byte(*stored), &out); err != nil {
		return nil, fmt.Errorf("decode stored response: %w", err)
	}
	return out, nil
}

func (c *IdempotencyCoordinator) decodeAtomicStoredResponse(stored *string) (any, error) {
	if stored == nil || strings.TrimSpace(*stored) == "" {
		return map[string]any{}, nil
	}
	var out any
	decoder := json.NewDecoder(strings.NewReader(*stored))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return nil, fmt.Errorf("decode stored response: %w", err)
	}
	return out, nil
}
