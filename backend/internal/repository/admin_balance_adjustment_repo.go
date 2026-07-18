package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/ent/userallowedgroup"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AdminBalanceAdjustmentRepository = (*userRepository)(nil)

func (r *userRepository) ApplyAdminBalanceAdjustment(
	ctx context.Context,
	userID int64,
	adjustment service.AdminBalanceAdjustment,
) (*service.AdminBalanceAdjustmentResult, error) {
	return r.applyAdminBalanceAdjustment(ctx, userID, adjustment, nil, func(user *service.User) any { return user })
}

func (r *userRepository) ApplyAdminBalanceAdjustmentAtomic(
	ctx context.Context,
	userID int64,
	adjustment service.AdminBalanceAdjustment,
	claim *service.IdempotencyAtomicClaim,
	responseFactory service.AdminBalanceAdjustmentResponseFactory,
) (*service.AdminBalanceAdjustmentResult, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("user repository is not configured")
	}
	if claim == nil {
		return nil, service.ErrIdempotencyStoreUnavail
	}
	if responseFactory == nil {
		return nil, errors.New("admin balance response factory is nil")
	}
	return r.applyAdminBalanceAdjustment(ctx, userID, adjustment, claim, responseFactory)
}

func (r *userRepository) applyAdminBalanceAdjustment(
	ctx context.Context,
	userID int64,
	adjustment service.AdminBalanceAdjustment,
	claim *service.IdempotencyAtomicClaim,
	responseFactory service.AdminBalanceAdjustmentResponseFactory,
) (*service.AdminBalanceAdjustmentResult, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("user repository is not configured")
	}
	if responseFactory == nil {
		return nil, errors.New("admin balance response factory is nil")
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin admin balance adjustment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	txClient := tx.Client()

	locked, err := txClient.User.Query().
		Where(dbuser.IDEQ(userID)).
		ForUpdate().
		Only(txCtx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrUserNotFound, nil)
	}

	nextBalance, balanceDelta, err := adjustment.ApplyTo(locked.Balance)
	if err != nil {
		return nil, err
	}
	updated, err := txClient.User.UpdateOneID(userID).
		SetBalance(nextBalance).
		Save(txCtx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrUserNotFound, nil)
	}

	resultUser := userEntityToService(updated)
	allowedGroupRows, err := txClient.UserAllowedGroup.Query().
		Where(userallowedgroup.UserIDEQ(userID)).
		All(txCtx)
	if err != nil {
		return nil, fmt.Errorf("load adjusted user allowed groups: %w", err)
	}
	for i := range allowedGroupRows {
		resultUser.AllowedGroups = append(resultUser.AllowedGroups, allowedGroupRows[i].GroupID)
	}
	sort.Slice(resultUser.AllowedGroups, func(i, j int) bool {
		return resultUser.AllowedGroups[i] < resultUser.AllowedGroups[j]
	})

	if balanceDelta != 0 {
		code := strings.TrimSpace(adjustment.AuditCode)
		if code == "" {
			code, err = service.GenerateRedeemCode()
			if err != nil {
				return nil, fmt.Errorf("generate balance adjustment audit code: %w", err)
			}
		}
		now := time.Now().UTC()
		auditCode, err := txClient.RedeemCode.Create().
			SetCode(code).
			SetType(service.AdjustmentTypeAdminBalance).
			SetValue(balanceDelta).
			SetStatus(service.StatusUsed).
			SetUsedBy(userID).
			SetUsedAt(now).
			SetNotes(adjustment.Notes).
			Save(txCtx)
		if err != nil {
			return nil, fmt.Errorf("create balance adjustment audit: %w", err)
		}
		if adjustment.Operation == "add" && balanceDelta > 0 {
			if _, err := txClient.ExecContext(txCtx, `
INSERT INTO affiliate_rebate_jobs (
    invitee_user_id,
    source_redeem_code_id,
    source_kind,
    base_amount,
    status,
    next_retry_at,
    created_at,
    updated_at
)
VALUES ($1, $2, 'admin_recharge', $3, 'pending', NOW(), NOW(), NOW())
ON CONFLICT (source_redeem_code_id) DO NOTHING`, userID, auditCode.ID, balanceDelta); err != nil {
				return nil, fmt.Errorf("enqueue admin affiliate rebate job: %w", err)
			}
		}
	}

	responseData := responseFactory(resultUser)
	if claim != nil {
		if err := claim.PersistSuccess(txCtx, tx, responseData); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit admin balance adjustment transaction: %w", err)
	}

	return &service.AdminBalanceAdjustmentResult{
		User:         resultUser,
		BalanceDelta: balanceDelta,
		Response:     responseData,
	}, nil
}
