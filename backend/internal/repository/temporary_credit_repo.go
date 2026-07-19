package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type temporaryCreditRepository struct {
	db *sql.DB
}

func NewTemporaryCreditRepository(db *sql.DB) service.TemporaryCreditRepository {
	return &temporaryCreditRepository{db: db}
}

func (r *temporaryCreditRepository) CreateGrant(ctx context.Context, grant service.TemporaryCreditGrant) (*service.TemporaryCreditGrant, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("temporary credit repository db is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin temporary credit grant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	out, err := r.CreateGrantTx(ctx, tx, grant)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit temporary credit grant transaction: %w", err)
	}
	return out, nil
}

func (r *temporaryCreditRepository) CreateGrantTx(ctx context.Context, tx *sql.Tx, grant service.TemporaryCreditGrant) (*service.TemporaryCreditGrant, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("temporary credit repository db is nil")
	}
	if tx == nil {
		return nil, errors.New("temporary credit grant transaction is nil")
	}
	if err := service.ValidateTemporaryCreditAmount(grant.Amount); err != nil {
		return nil, err
	}
	expiresAt := grant.ExpiresAt()
	if expiresAt.IsZero() {
		return nil, errors.New("temporary credit expiration is required")
	}
	out := grant
	var source string
	var checkinID, grantedBy sql.NullInt64
	var persistedExpiresAt time.Time
	err := tx.QueryRowContext(ctx, `
INSERT INTO temporary_credit_grants
    (user_id, source, checkin_id, amount, remaining_amount, expires_at, notes, granted_by)
VALUES ($1, $2, $3, $4, $4, $5, $6, $7)
RETURNING id, user_id, source, checkin_id, amount, remaining_amount, expires_at, notes, granted_by, created_at, updated_at`,
		grant.UserID, grant.Source, nullableInt64(grant.CheckinID), strconv.FormatFloat(grant.Amount, 'f', 8, 64), expiresAt,
		grant.Notes, nullableInt64(grant.GrantedBy),
	).Scan(&out.ID, &out.UserID, &source, &checkinID, &out.Amount, &out.RemainingAmount, &persistedExpiresAt, &out.Notes, &grantedBy, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create temporary credit grant: %w", err)
	}
	out.Source = service.TemporaryCreditSource(source)
	out.CheckinID = nullableInt64Ptr(checkinID)
	out.GrantedBy = nullableInt64Ptr(grantedBy)
	if out.Source != service.TemporaryCreditSourceBankAdvance {
		remaining, _, offsetErr := service.ApplyTemporaryCreditDebtOffsetTx(ctx, tx, out.UserID, out.ID, out.Amount)
		if offsetErr != nil {
			return nil, fmt.Errorf("apply temporary credit debt offset: %w", offsetErr)
		}
		out.RemainingAmount = remaining
	}
	return &out, nil
}

func (r *temporaryCreditRepository) AvailableSummary(ctx context.Context, userID int64) (float64, *time.Time, error) {
	if r == nil || r.db == nil {
		return 0, nil, errors.New("temporary credit repository db is nil")
	}
	var total float64
	var earliest sql.NullTime
	err := r.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(remaining_amount), 0), MIN(expires_at)
FROM temporary_credit_grants
WHERE user_id = $1 AND remaining_amount > 0 AND expires_at > clock_timestamp()`, userID).
		Scan(&total, &earliest)
	if err != nil {
		return 0, nil, err
	}
	if !earliest.Valid {
		return total, nil, nil
	}
	return total, &earliest.Time, nil
}

// ConsumeFEFO delegates to the shared transaction-bound allocator so direct
// repository callers and both usage billing chains have identical FEFO rules.
func (r *temporaryCreditRepository) ConsumeFEFO(ctx context.Context, tx *sql.Tx, userID int64, amount float64, reference service.TemporaryCreditConsumptionReference) (float64, error) {
	if tx == nil {
		return 0, service.ErrTemporaryCreditTransactionRequired
	}
	return service.NewTemporaryCreditAllocationExecutor().Allocate(ctx, tx, userID, amount, reference)
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

var _ service.TemporaryCreditRepository = (*temporaryCreditRepository)(nil)
