package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.PlayerInvitationRepository = (*playerInvitationRepository)(nil)

const invitationGraphLockKey int64 = 78231701

type playerInvitationRepository struct {
	client *dbent.Client
}

func NewPlayerInvitationRepository(client *dbent.Client, _ *sql.DB) service.PlayerInvitationRepository {
	return &playerInvitationRepository{client: client}
}

type invitationExecer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (r *playerInvitationRepository) withTx(ctx context.Context, fn func(context.Context, invitationExecer) error) error {
	if r == nil || r.client == nil {
		return service.ErrServiceUnavailable
	}
	if tx := dbent.TxFromContext(ctx); tx != nil {
		if err := lockInvitationGraph(ctx, tx.Client()); err != nil {
			return err
		}
		return fn(ctx, tx.Client())
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin invitation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockInvitationGraph(ctx, tx.Client()); err != nil {
		return err
	}
	if err := fn(dbent.NewTxContext(ctx, tx), tx.Client()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit invitation transaction: %w", err)
	}
	return nil
}

func (r *playerInvitationRepository) EnsureInitialQuota(ctx context.Context, userID int64) error {
	if userID <= 0 {
		return service.ErrUserNotFound
	}
	client := clientFromContext(ctx, r.client)
	return ensureInitialInvitationQuota(ctx, client, userID)
}

func ensureInitialInvitationQuota(ctx context.Context, client invitationExecer, userID int64) error {
	res, err := client.ExecContext(ctx, `
INSERT INTO player_invitation_quota_events (user_id, event_type, delta, idempotency_key, reason)
SELECT id, 'initial_grant', 1, 'initial:v1', 'lazy initial player invitation quota'
FROM users
WHERE id = $1
ON CONFLICT (user_id, idempotency_key) DO NOTHING`, userID)
	if err != nil {
		return fmt.Errorf("ensure initial invitation quota: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		var exists bool
		if err := scanInvitationSingleRow(ctx, client, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, []any{userID}, &exists); err != nil {
			return err
		}
		if !exists {
			return service.ErrUserNotFound
		}
	}
	return nil
}

func (r *playerInvitationRepository) GetSummary(ctx context.Context, userID int64) (*service.PlayerInvitationSummary, error) {
	client := clientFromContext(ctx, r.client)
	return queryInvitationSummary(ctx, client, userID, time.Now().UTC())
}

func queryInvitationSummary(ctx context.Context, client invitationExecer, userID int64, now time.Time) (*service.PlayerInvitationSummary, error) {
	var granted, reserved, consumed int64
	err := scanInvitationSingleRow(ctx, client, `
SELECT COALESCE((SELECT SUM(delta) FROM player_invitation_quota_events WHERE user_id=$1), 0)::bigint,
       COALESCE((SELECT COUNT(*) FROM player_invitation_reservations WHERE inviter_user_id=$1 AND status='reserved' AND expires_at > $2), 0)::bigint,
       COALESCE((SELECT COUNT(*) FROM player_invitation_reservations WHERE inviter_user_id=$1 AND status='claimed'), 0)::bigint`, []any{userID, now}, &granted, &reserved, &consumed)
	if err != nil {
		return nil, fmt.Errorf("query invitation summary: %w", err)
	}
	available := granted - reserved - consumed
	if available < 0 {
		return nil, fmt.Errorf("invitation quota invariant violated for user %d", userID)
	}
	return &service.PlayerInvitationSummary{
		UserID:       userID,
		Available:    available,
		Reserved:     reserved,
		Consumed:     consumed,
		TotalGranted: granted,
		UpdatedAt:    now,
	}, nil
}

func (r *playerInvitationRepository) ListReservations(ctx context.Context, userID int64, limit int) ([]service.PlayerInvitationReservation, error) {
	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, `
SELECT id, inviter_user_id, status, expires_at, claimed_user_id, claimed_at, cancelled_at, created_at
FROM player_invitation_reservations
WHERE inviter_user_id = $1
ORDER BY id DESC
LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list invitation reservations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.PlayerInvitationReservation, 0)
	for rows.Next() {
		var item service.PlayerInvitationReservation
		if err := rows.Scan(&item.ID, &item.InviterUserID, &item.Status, &item.ExpiresAt, &item.ClaimedUserID, &item.ClaimedAt, &item.CancelledAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *playerInvitationRepository) Reserve(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time, requestHashes ...string) (*service.PlayerInvitationReservation, error) {
	requestHash := ""
	if len(requestHashes) > 0 {
		requestHash = requestHashes[0]
	}
	var out service.PlayerInvitationReservation
	err := r.withTx(ctx, func(txCtx context.Context, client invitationExecer) error {
		var locked int64
		if err := scanInvitationSingleRow(txCtx, client, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL AND status='active' FOR UPDATE`, []any{userID}, &locked); err != nil {
			if err == sql.ErrNoRows {
				return service.ErrUserNotFound
			}
			return err
		}
		if requestHash != "" {
			var savedHash string
			replayErr := scanInvitationSingleRow(txCtx, client, `
SELECT id, inviter_user_id, status, expires_at, claimed_user_id, claimed_at, cancelled_at, created_at, token_hash
FROM player_invitation_reservations WHERE inviter_user_id=$1 AND request_key_hash=$2`, []any{userID, requestHash},
				&out.ID, &out.InviterUserID, &out.Status, &out.ExpiresAt, &out.ClaimedUserID, &out.ClaimedAt, &out.CancelledAt, &out.CreatedAt, &savedHash)
			if replayErr == nil {
				if savedHash != tokenHash {
					return service.ErrPlayerInvitationSourceConflict
				}
				return nil
			}
			if replayErr != sql.ErrNoRows {
				return replayErr
			}
		}
		if err := ensureInitialInvitationQuota(txCtx, client, userID); err != nil {
			return err
		}
		if _, err := expireInvitationReservationsTx(txCtx, client, userID, time.Now().UTC()); err != nil {
			return err
		}
		summary, err := queryInvitationSummary(txCtx, client, userID, time.Now().UTC())
		if err != nil {
			return err
		}
		if summary.Available < 1 {
			return service.ErrInvitationQuotaExhausted
		}
		var quotaEventID int64
		if err := scanInvitationSingleRow(txCtx, client, `
SELECT e.id FROM player_invitation_quota_events e
WHERE e.user_id=$1 AND e.event_type IN ('initial_grant','admin_grant','admin_revoke')
ORDER BY e.id
LIMIT 1`, []any{userID}, &quotaEventID); err != nil {
			return err
		}
		if err := scanInvitationSingleRow(txCtx, client, `
INSERT INTO player_invitation_reservations (inviter_user_id, quota_event_id, token_hash, status, expires_at, request_key_hash)
VALUES ($1, $2, $3, 'reserved', $4, NULLIF($5, ''))
RETURNING id, inviter_user_id, status, expires_at, claimed_user_id, claimed_at, cancelled_at, created_at`,
			[]any{userID, quotaEventID, tokenHash, expiresAt, requestHash},
			&out.ID, &out.InviterUserID, &out.Status, &out.ExpiresAt, &out.ClaimedUserID, &out.ClaimedAt, &out.CancelledAt, &out.CreatedAt); err != nil {
			return fmt.Errorf("create invitation reservation: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *playerInvitationRepository) CancelReservation(ctx context.Context, userID, reservationID int64, now time.Time) (bool, error) {
	var ok bool
	err := r.withTx(ctx, func(txCtx context.Context, client invitationExecer) error {
		res, err := client.ExecContext(txCtx, `
UPDATE player_invitation_reservations
SET status='cancelled', cancelled_at=$3, updated_at=$3
WHERE id=$1 AND inviter_user_id=$2 AND status='reserved' AND expires_at > $3`, reservationID, userID, now)
		if err != nil {
			return fmt.Errorf("cancel invitation reservation: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		ok = affected == 1
		return nil
	})
	return ok, err
}

func (r *playerInvitationRepository) ExpireReservations(ctx context.Context, userID int64, now time.Time) (int64, error) {
	client := clientFromContext(ctx, r.client)
	return expireInvitationReservationsTx(ctx, client, userID, now)
}

func expireInvitationReservationsTx(ctx context.Context, client invitationExecer, userID int64, now time.Time) (int64, error) {
	res, err := client.ExecContext(ctx, `
UPDATE player_invitation_reservations
SET status='expired', updated_at=$2
WHERE inviter_user_id=$1 AND status='reserved' AND expires_at <= $2`, userID, now)
	if err != nil {
		return 0, fmt.Errorf("expire invitation reservations: %w", err)
	}
	return res.RowsAffected()
}

func (r *playerInvitationRepository) GetReservationContextByHash(ctx context.Context, tokenHash string, now time.Time) (*service.PlayerInvitationContext, error) {
	client := clientFromContext(ctx, r.client)
	var out service.PlayerInvitationContext
	err := scanInvitationSingleRow(ctx, client, `
SELECT id, inviter_user_id, expires_at
FROM player_invitation_reservations
WHERE token_hash=$1 AND status='reserved' AND expires_at > $2`, []any{tokenHash, now}, &out.ReservationID, &out.InviterUserID, &out.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, service.ErrPlayerInvitationInvalid
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *playerInvitationRepository) ClaimReservation(ctx context.Context, tokenHash string, inviteeUserID int64, expectedInviterID *int64, now time.Time) (*service.InvitationRelationship, error) {
	var out service.InvitationRelationship
	err := r.withTx(ctx, func(txCtx context.Context, client invitationExecer) error {
		var reservationID, inviterID int64
		var status string
		var expiresAt time.Time
		var claimedUserID *int64
		err := scanInvitationSingleRow(txCtx, client, `
SELECT id, inviter_user_id, status, expires_at, claimed_user_id
FROM player_invitation_reservations
WHERE token_hash=$1
FOR UPDATE`, []any{tokenHash}, &reservationID, &inviterID, &status, &expiresAt, &claimedUserID)
		if err == sql.ErrNoRows {
			return service.ErrPlayerInvitationInvalid
		}
		if err != nil {
			return err
		}
		if status == "claimed" && claimedUserID != nil && *claimedUserID == inviteeUserID {
			return queryInvitationRelationshipTx(txCtx, client, inviteeUserID, &out)
		}
		if status != "reserved" {
			return service.ErrPlayerInvitationInvalid
		}
		if !expiresAt.After(now) {
			_, _ = client.ExecContext(txCtx, `UPDATE player_invitation_reservations SET status='expired', updated_at=$2 WHERE id=$1 AND status='reserved'`, reservationID, now)
			return service.ErrPlayerInvitationInvalid
		}
		if expectedInviterID != nil && *expectedInviterID != inviterID {
			return service.ErrPlayerInvitationSourceConflict
		}
		if inviterID == inviteeUserID {
			return service.ErrInvitationSelfReferral
		}
		if err := lockInvitationGraph(txCtx, client); err != nil {
			return err
		}
		var legacyInviterID *int64
		if err := scanInvitationSingleRow(txCtx, client, `SELECT inviter_id FROM user_affiliates WHERE user_id=$1 FOR UPDATE`, []any{inviteeUserID}, &legacyInviterID); err != nil && err != sql.ErrNoRows {
			return err
		}
		if legacyInviterID != nil && *legacyInviterID != inviterID {
			return service.ErrInvitationRelationshipExists
		}
		var existingInviterID int64
		err = scanInvitationSingleRow(txCtx, client, `SELECT inviter_user_id FROM player_invitation_relations WHERE invitee_user_id=$1 FOR UPDATE`, []any{inviteeUserID}, &existingInviterID)
		if err == nil {
			if existingInviterID != inviterID {
				return service.ErrInvitationRelationshipExists
			}
			return queryInvitationRelationshipTx(txCtx, client, inviteeUserID, &out)
		}
		if err != sql.ErrNoRows {
			return err
		}
		cycle, err := invitationWouldCreateCycle(txCtx, client, inviterID, inviteeUserID)
		if err != nil {
			return err
		}
		if cycle {
			return service.ErrInvitationRelationshipCycle
		}
		if _, err := ensureUserAffiliateWithClient(txCtx, client, inviterID); err != nil {
			return err
		}
		if _, err := ensureUserAffiliateWithClient(txCtx, client, inviteeUserID); err != nil {
			return err
		}
		if err := scanInvitationSingleRow(txCtx, client, `
INSERT INTO player_invitation_relations
    (invitee_user_id, inviter_user_id, source, reservation_id, effective_at, reason)
VALUES ($1, $2, 'player_invitation', $3, $4, 'player invitation registration')
RETURNING invitee_user_id, inviter_user_id, source, effective_at`,
			[]any{inviteeUserID, inviterID, reservationID, now}, &out.InviteeUserID, &out.InviterUserID, &out.Source, &out.EffectiveAt); err != nil {
			if isInvitationUniqueViolation(err) {
				return service.ErrInvitationRelationshipExists
			}
			return fmt.Errorf("create player invitation relationship: %w", err)
		}
		res, err := client.ExecContext(txCtx, `
UPDATE player_invitation_reservations
SET status='claimed', claimed_user_id=$2, claimed_at=$3, updated_at=$3
WHERE id=$1 AND status='reserved'`, reservationID, inviteeUserID, now)
		if err != nil {
			return err
		}
		if affected, _ := res.RowsAffected(); affected != 1 {
			return service.ErrPlayerInvitationInvalid
		}
		if err := bindAffiliateRelationshipTx(txCtx, client, inviteeUserID, inviterID, now); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func bindAffiliateRelationshipTx(ctx context.Context, client invitationExecer, inviteeUserID, inviterID int64, effectiveAt time.Time) error {
	res, err := client.ExecContext(ctx, `
UPDATE user_affiliates SET inviter_id=$2, inviter_effective_at=$3, updated_at=NOW()
WHERE user_id=$1 AND inviter_id IS NULL`, inviteeUserID, inviterID, effectiveAt)
	if err != nil {
		return fmt.Errorf("bind affiliate inviter: %w", err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		var existing int64
		if err := scanInvitationSingleRow(ctx, client, `SELECT inviter_id FROM user_affiliates WHERE user_id=$1`, []any{inviteeUserID}, &existing); err != nil {
			return err
		}
		if existing != inviterID {
			return service.ErrInvitationRelationshipExists
		}
		return nil
	}
	_, err = client.ExecContext(ctx, `UPDATE user_affiliates SET aff_count=aff_count+1, updated_at=NOW() WHERE user_id=$1`, inviterID)
	return err
}

func queryInvitationRelationshipTx(ctx context.Context, client invitationExecer, inviteeUserID int64, out *service.InvitationRelationship) error {
	return scanInvitationSingleRow(ctx, client, `
SELECT invitee_user_id, inviter_user_id, source, effective_at
FROM player_invitation_relations WHERE invitee_user_id=$1`, []any{inviteeUserID}, &out.InviteeUserID, &out.InviterUserID, &out.Source, &out.EffectiveAt)
}

func (r *playerInvitationRepository) ReleaseClaimedReservation(ctx context.Context, inviteeUserID int64, tokenHash string, now time.Time) error {
	return r.withTx(ctx, func(txCtx context.Context, client invitationExecer) error {
		var reservationID, inviterID int64
		if err := scanInvitationSingleRow(txCtx, client, `
SELECT id, inviter_user_id FROM player_invitation_reservations
WHERE token_hash=$1 AND claimed_user_id=$2 AND status='claimed' FOR UPDATE`, []any{tokenHash, inviteeUserID}, &reservationID, &inviterID); err != nil {
			if err == sql.ErrNoRows {
				return nil
			}
			return err
		}
		if err := lockInvitationGraph(txCtx, client); err != nil {
			return err
		}
		if _, err := client.ExecContext(txCtx, `DELETE FROM player_invitation_relations WHERE reservation_id=$1 AND invitee_user_id=$2`, reservationID, inviteeUserID); err != nil {
			return err
		}
		if _, err := client.ExecContext(txCtx, `
UPDATE user_affiliates SET inviter_id=NULL, inviter_effective_at=NULL, updated_at=NOW()
WHERE user_id=$1 AND inviter_id=$2`, inviteeUserID, inviterID); err != nil {
			return err
		}
		if _, err := client.ExecContext(txCtx, `UPDATE user_affiliates SET aff_count=GREATEST(aff_count-1,0), updated_at=NOW() WHERE user_id=$1`, inviterID); err != nil {
			return err
		}
		_, err := client.ExecContext(txCtx, `
UPDATE player_invitation_reservations
SET status=CASE WHEN expires_at>$3 THEN 'reserved' ELSE 'expired' END, claimed_user_id=NULL, claimed_at=NULL, cancelled_at=NULL, updated_at=$3
WHERE id=$1 AND claimed_user_id=$2 AND status='claimed'`, reservationID, inviteeUserID, now)
		return err
	})
}

func (r *playerInvitationRepository) AdjustQuota(ctx context.Context, input service.InvitationQuotaAdjustment, _, _ int64) error {
	return r.withTx(ctx, func(txCtx context.Context, client invitationExecer) error {
		var locked int64
		if err := scanInvitationSingleRow(txCtx, client, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, []any{input.TargetUserID}, &locked); err != nil {
			if err == sql.ErrNoRows {
				return service.ErrUserNotFound
			}
			return err
		}

		var priorDelta int
		var priorActor int64
		var priorReason string
		replayErr := scanInvitationSingleRow(txCtx, client, `SELECT delta,actor_user_id,reason FROM player_invitation_quota_events WHERE user_id=$1 AND idempotency_key=$2`, []any{input.TargetUserID, input.IdempotencyKey}, &priorDelta, &priorActor, &priorReason)
		if replayErr == nil {
			if priorDelta != input.Delta || priorActor != input.ActorUserID || priorReason != input.Reason {
				return service.ErrPlayerInvitationSourceConflict
			}
			return nil
		}
		if replayErr != sql.ErrNoRows {
			return replayErr
		}
		if err := ensureInitialInvitationQuota(txCtx, client, input.TargetUserID); err != nil {
			return err
		}
		if _, err := expireInvitationReservationsTx(txCtx, client, input.TargetUserID, time.Now().UTC()); err != nil {
			return err
		}
		before, err := queryInvitationSummary(txCtx, client, input.TargetUserID, time.Now().UTC())
		if err != nil {
			return err
		}
		afterTotal := before.TotalGranted + int64(input.Delta)
		if afterTotal < before.Reserved+before.Consumed {
			return service.ErrInvitationQuotaExhausted
		}
		eventType := "admin_grant"
		if input.Delta < 0 {
			eventType = "admin_revoke"
		}
		res, err := client.ExecContext(txCtx, `
INSERT INTO player_invitation_quota_events
    (user_id, event_type, delta, idempotency_key, reason, actor_user_id, request_id)
VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7,''))
ON CONFLICT (user_id, idempotency_key) DO NOTHING`, input.TargetUserID, eventType, input.Delta, input.IdempotencyKey, input.Reason, input.ActorUserID, input.RequestID)
		if err != nil {
			return fmt.Errorf("adjust invitation quota: %w", err)
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return nil
		}
		beforeJSON, _ := json.Marshal(map[string]int64{"total_granted": before.TotalGranted})
		afterJSON, _ := json.Marshal(map[string]int64{"total_granted": afterTotal})
		return insertInvitationAuditTx(txCtx, client, input.ActorUserID, input.TargetUserID, "invitation.quota.adjust", beforeJSON, afterJSON, input.Reason, input.RequestID)
	})
}

func (r *playerInvitationRepository) CreateAdminRelationship(ctx context.Context, input service.InvitationRelationshipCreateInput) (*service.InvitationRelationship, error) {
	var out service.InvitationRelationship
	err := r.withTx(ctx, func(txCtx context.Context, client invitationExecer) error {
		if err := lockInvitationGraph(txCtx, client); err != nil {
			return err
		}
		if strings.TrimSpace(input.RequestID) == "" {
			return service.ErrIdempotencyKeyRequired
		}
		var replayInviter, replayInvitee int64
		var replayReason string
		replayErr := scanInvitationSingleRow(txCtx, client, `SELECT inviter_user_id,invitee_user_id,reason FROM player_invitation_relations WHERE created_by_user_id=$1 AND request_id=$2`, []any{input.ActorUserID, input.RequestID}, &replayInviter, &replayInvitee, &replayReason)
		if replayErr == nil {
			if replayInviter != input.InviterUserID || replayInvitee != input.InviteeUserID || replayReason != input.Reason {
				return service.ErrPlayerInvitationSourceConflict
			}
			return queryInvitationRelationshipTx(txCtx, client, replayInvitee, &out)
		}
		if replayErr != sql.ErrNoRows {
			return replayErr
		}
		var existing int64
		err := scanInvitationSingleRow(txCtx, client, `SELECT inviter_user_id FROM player_invitation_relations WHERE invitee_user_id=$1 FOR UPDATE`, []any{input.InviteeUserID}, &existing)
		if err == nil {
			return service.ErrInvitationRelationshipExists
		}
		if err != sql.ErrNoRows {
			return err
		}
		cycle, err := invitationWouldCreateCycle(txCtx, client, input.InviterUserID, input.InviteeUserID)
		if err != nil {
			return err
		}
		if cycle {
			return service.ErrInvitationRelationshipCycle
		}
		if _, err := ensureUserAffiliateWithClient(txCtx, client, input.InviterUserID); err != nil {
			return err
		}
		if _, err := ensureUserAffiliateWithClient(txCtx, client, input.InviteeUserID); err != nil {
			return err
		}
		if err := scanInvitationSingleRow(txCtx, client, `
INSERT INTO player_invitation_relations
    (invitee_user_id, inviter_user_id, source, effective_at, created_by_user_id, reason, request_id)
VALUES ($1,$2,'admin_backfill',$3,$4,$5,NULLIF($6,''))
RETURNING invitee_user_id, inviter_user_id, source, effective_at`,
			[]any{input.InviteeUserID, input.InviterUserID, input.EffectiveAt, input.ActorUserID, input.Reason, input.RequestID}, &out.InviteeUserID, &out.InviterUserID, &out.Source, &out.EffectiveAt); err != nil {
			if isInvitationUniqueViolation(err) {
				return service.ErrInvitationRelationshipExists
			}
			return err
		}
		if err := bindAffiliateRelationshipTx(txCtx, client, input.InviteeUserID, input.InviterUserID, input.EffectiveAt); err != nil {
			return err
		}
		afterJSON, _ := json.Marshal(out)
		return insertInvitationAuditTx(txCtx, client, input.ActorUserID, input.InviteeUserID, "affiliates.relationship.create", nil, afterJSON, input.Reason, input.RequestID)
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func insertInvitationAuditTx(ctx context.Context, client invitationExecer, actorID, targetID int64, action string, before, after []byte, reason, requestID string) error {
	_, err := client.ExecContext(ctx, `
INSERT INTO player_invitation_audit_logs
    (actor_user_id, target_user_id, action, before_state, after_state, reason, request_id)
VALUES (NULLIF($1,0), NULLIF($2,0), $3, $4::jsonb, $5::jsonb, $6, NULLIF($7,''))`, actorID, targetID, action, nullableInvitationJSON(before), nullableInvitationJSON(after), reason, requestID)
	if err != nil {
		return fmt.Errorf("insert invitation audit: %w", err)
	}
	return nil
}

func lockInvitationGraph(ctx context.Context, client invitationExecer) error {
	_, err := client.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, invitationGraphLockKey)
	return err
}

func invitationWouldCreateCycle(ctx context.Context, client invitationExecer, inviterID, inviteeUserID int64) (bool, error) {
	var exists bool
	err := scanInvitationSingleRow(ctx, client, `
WITH RECURSIVE edges(invitee_user_id, inviter_user_id) AS (
    SELECT invitee_user_id, inviter_user_id FROM player_invitation_relations
    UNION SELECT user_id, inviter_id FROM user_affiliates WHERE inviter_id IS NOT NULL
), ancestors(user_id) AS (
    SELECT $1::bigint
    UNION
    SELECT r.inviter_user_id
    FROM edges r
    JOIN ancestors a ON r.invitee_user_id = a.user_id
)
SELECT EXISTS(SELECT 1 FROM ancestors WHERE user_id=$2)`, []any{inviterID, inviteeUserID}, &exists)
	if err != nil {
		return false, fmt.Errorf("check invitation cycle: %w", err)
	}
	return exists, nil
}

func scanInvitationSingleRow(ctx context.Context, client invitationExecer, query string, args []any, dest ...any) error {
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	return rows.Scan(dest...)
}

func isInvitationUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "SQLSTATE 23505") || strings.Contains(message, "duplicate key")
}

func nullableInvitationJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func (r *playerInvitationRepository) PreviewInvitationRelationship(ctx context.Context, inviter, invitee int64) (*service.InvitationAdminPreview, error) {
	client := clientFromContext(ctx, r.client)
	var valid int
	if err := scanInvitationSingleRow(ctx, client, `SELECT COUNT(*) FROM users WHERE id IN ($1,$2) AND deleted_at IS NULL AND status='active'`, []any{inviter, invitee}, &valid); err != nil {
		return nil, err
	}
	if valid != 2 {
		return nil, service.ErrUserNotFound
	}
	result := &service.InvitationAdminPreview{InviterUserID: inviter, InviteeUserID: invitee}
	var existing sql.NullInt64
	if err := scanInvitationSingleRow(ctx, client, `SELECT COALESCE((SELECT inviter_user_id FROM player_invitation_relations WHERE invitee_user_id=$1),(SELECT inviter_id FROM user_affiliates WHERE user_id=$1))`, []any{invitee}, &existing); err != nil {
		return nil, err
	}
	if existing.Valid {
		result.ExistingInviterID = &existing.Int64
	}
	cycle, err := invitationWouldCreateCycle(ctx, client, inviter, invitee)
	if err != nil {
		return nil, err
	}
	result.Cycle = cycle
	result.CanCreate = !existing.Valid && !cycle
	return result, nil
}
