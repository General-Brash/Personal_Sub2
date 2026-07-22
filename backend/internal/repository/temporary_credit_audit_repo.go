package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type temporaryCreditAuditRepository struct {
	db *sql.DB
}

func NewTemporaryCreditAuditRepository(db *sql.DB) service.TemporaryCreditAuditRepository {
	return &temporaryCreditAuditRepository{db: db}
}

func (r *temporaryCreditAuditRepository) ListByUser(
	ctx context.Context,
	userID int64,
	page, pageSize int,
) ([]service.TemporaryCreditAuditItem, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, errors.New("temporary credit audit repository db is nil")
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM temporary_credit_grants WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count temporary credit audit items: %w", err)
	}
	offset := int64(page-1) * int64(pageSize)
	rows, err := r.db.QueryContext(ctx, `
SELECT id, user_id, source, checkin_id, amount::text, remaining_amount::text,
       available_at, expires_at,
       CASE
           WHEN available_at > clock_timestamp() THEN 'unused'
           WHEN remaining_amount = 0 THEN 'depleted'
           WHEN expires_at <= clock_timestamp() THEN 'expired'
           ELSE 'active'
       END AS status,
       notes, granted_by, created_at, updated_at
FROM temporary_credit_grants
WHERE user_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3`, userID, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list temporary credit audit items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.TemporaryCreditAuditItem, 0)
	for rows.Next() {
		var item service.TemporaryCreditAuditItem
		var source, status string
		var checkinID, grantedBy sql.NullInt64
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&source,
			&checkinID,
			&item.Amount,
			&item.RemainingAmount,
			&item.AvailableAt,
			&item.ExpiresAt,
			&status,
			&item.Notes,
			&grantedBy,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan temporary credit audit item: %w", err)
		}
		item.Source = service.TemporaryCreditSource(source)
		item.Status = service.TemporaryCreditStatus(status)
		item.CheckinID = nullableInt64Ptr(checkinID)
		item.GrantedBy = nullableInt64Ptr(grantedBy)
		item.AvailableAt = item.AvailableAt.UTC()
		item.ExpiresAt = item.ExpiresAt.UTC()
		item.CreatedAt = item.CreatedAt.UTC()
		item.UpdatedAt = item.UpdatedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate temporary credit audit items: %w", err)
	}
	return items, total, nil
}

var _ service.TemporaryCreditAuditRepository = (*temporaryCreditAuditRepository)(nil)
