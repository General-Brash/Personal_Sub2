package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrAdminTemporaryCreditInvalidAmount = infraerrors.BadRequest("INVALID_TEMPORARY_CREDIT_AMOUNT", "temporary credit amount or notes is invalid")
	ErrTemporaryCreditPaginationInvalid  = infraerrors.BadRequest("INVALID_PAGINATION", "temporary credit pagination is invalid")
	ErrTemporaryCreditTargetNotFound     = infraerrors.NotFound("USER_NOT_FOUND", "user not found")
)

type AdminTemporaryCreditGrantResult struct {
	TemporaryCreditGrantID int64     `json:"temporary_credit_grant_id"`
	Amount                 string    `json:"amount"`
	RemainingAmount        string    `json:"remaining_amount"`
	ExpiresAt              time.Time `json:"expires_at"`
	Notes                  string    `json:"notes"`
}

type TemporaryCreditAuditItem struct {
	ID              int64                 `json:"id"`
	UserID          int64                 `json:"user_id"`
	Source          TemporaryCreditSource `json:"source"`
	CheckinID       *int64                `json:"checkin_id"`
	Amount          string                `json:"amount"`
	RemainingAmount string                `json:"remaining_amount"`
	ExpiresAt       time.Time             `json:"expires_at"`
	Notes           string                `json:"notes"`
	GrantedBy       *int64                `json:"granted_by"`
	CreatedAt       time.Time             `json:"created_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
}

type TemporaryCreditAuditRepository interface {
	ListByUser(ctx context.Context, userID int64, page, pageSize int) ([]TemporaryCreditAuditItem, int64, error)
}

type AdminTemporaryCreditService struct {
	db                     *sql.DB
	temporaryCreditService *TemporaryCreditService
	auditRepository        TemporaryCreditAuditRepository
}

func NewAdminTemporaryCreditService(
	db *sql.DB,
	temporaryCreditService *TemporaryCreditService,
	auditRepository TemporaryCreditAuditRepository,
) *AdminTemporaryCreditService {
	return &AdminTemporaryCreditService{
		db:                     db,
		temporaryCreditService: temporaryCreditService,
		auditRepository:        auditRepository,
	}
}

func (s *AdminTemporaryCreditService) GrantAtomic(
	ctx context.Context,
	userID, adminID int64,
	amount float64,
	notes string,
	claim *IdempotencyAtomicClaim,
) (*AdminTemporaryCreditGrantResult, error) {
	if s == nil || s.db == nil || s.temporaryCreditService == nil {
		return nil, errors.New("admin temporary credit service is not configured")
	}
	if claim == nil {
		return nil, ErrIdempotencyStoreUnavail
	}
	if userID <= 0 {
		return nil, ErrTemporaryCreditTargetNotFound
	}
	if adminID <= 0 {
		return nil, infraerrors.InternalServer("ADMIN_ID_INVALID", "authenticated administrator id is invalid")
	}
	if err := ValidateTemporaryCreditAmount(amount); err != nil {
		return nil, ErrAdminTemporaryCreditInvalidAmount
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin admin temporary credit transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := lockAdminGrantUsers(ctx, tx, userID, adminID); err != nil {
		return nil, err
	}
	businessNow, err := sampleTemporaryCreditTransactionClock(ctx, tx)
	if err != nil {
		return nil, err
	}
	grant, err := s.temporaryCreditService.CreateGrantTx(ctx, tx, CreateTemporaryCreditGrantInput{
		UserID:      userID,
		Source:      TemporaryCreditSourceAdminGrant,
		Amount:      amount,
		Notes:       notes,
		GrantedBy:   &adminID,
		businessNow: &businessNow,
	})
	if err != nil {
		return nil, fmt.Errorf("create admin temporary credit grant: %w", err)
	}
	result := &AdminTemporaryCreditGrantResult{
		TemporaryCreditGrantID: grant.ID,
		Amount:                 formatLedgerAmount(grant.Amount),
		RemainingAmount:        formatLedgerAmount(grant.RemainingAmount),
		ExpiresAt:              grant.ExpiresAt().UTC(),
		Notes:                  grant.Notes,
	}
	if err := claim.PersistSuccess(ctx, tx, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit admin temporary credit transaction: %w", err)
	}
	s.temporaryCreditService.invalidateAvailableCredit(ctx, userID)
	return result, nil
}

func (s *AdminTemporaryCreditService) ListAudit(ctx context.Context, userID int64, page, pageSize int) ([]TemporaryCreditAuditItem, int64, error) {
	if s == nil || s.auditRepository == nil {
		return nil, 0, errors.New("temporary credit audit repository is not configured")
	}
	if userID <= 0 {
		return nil, 0, ErrTemporaryCreditTargetNotFound
	}
	if page < 1 || pageSize < 1 || pageSize > 1000 {
		return nil, 0, ErrTemporaryCreditPaginationInvalid
	}
	return s.auditRepository.ListByUser(ctx, userID, page, pageSize)
}

func lockAdminGrantUsers(ctx context.Context, tx *sql.Tx, userID, adminID int64) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id
FROM users
WHERE (id = $1 OR id = $2) AND deleted_at IS NULL
ORDER BY id
FOR UPDATE`, userID, adminID)
	if err != nil {
		return fmt.Errorf("lock temporary credit users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	foundUser := false
	foundAdmin := false
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan temporary credit locked user: %w", err)
		}
		foundUser = foundUser || id == userID
		foundAdmin = foundAdmin || id == adminID
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate temporary credit locked users: %w", err)
	}
	if !foundUser {
		return ErrTemporaryCreditTargetNotFound
	}
	if !foundAdmin {
		return infraerrors.InternalServer("ADMIN_NOT_FOUND", "authenticated administrator was not found")
	}
	return nil
}

func sampleTemporaryCreditTransactionClock(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("sample temporary credit database clock: %w", err)
	}
	return now, nil
}
