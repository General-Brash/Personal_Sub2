package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type temporaryCreditAuditRepositoryStub struct {
	items    []TemporaryCreditAuditItem
	total    int64
	userID   int64
	page     int
	pageSize int
}

func (s *temporaryCreditAuditRepositoryStub) ListByUser(_ context.Context, userID int64, page, pageSize int) ([]TemporaryCreditAuditItem, int64, error) {
	s.userID = userID
	s.page = page
	s.pageSize = pageSize
	return s.items, s.total, nil
}

func newAtomicClaimForTest(expiresAt time.Time) *IdempotencyAtomicClaim {
	return &IdempotencyAtomicClaim{
		coordinator:        NewIdempotencyCoordinator(nil, DefaultIdempotencyConfig()),
		recordID:           91,
		requestFingerprint: "admin-grant-fingerprint",
		expiresAt:          expiresAt,
	}
}

func expectAdminGrantUserLocks(mock sqlmock.Sqlmock, userID, adminID int64) {
	mock.ExpectQuery(`SELECT id\s+FROM users\s+WHERE \(id = \$1 OR id = \$2\) AND deleted_at IS NULL\s+ORDER BY id\s+FOR UPDATE`).
		WithArgs(userID, adminID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID).AddRow(adminID))
}

func TestAdminTemporaryCreditServiceGrantPersistsDTOInBusinessTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	userID := int64(42)
	adminID := int64(99)
	businessNow := time.Date(2026, time.July, 16, 15, 59, 59, 0, time.UTC)
	creditRepo := &temporaryCreditRepositoryRecorder{}
	invalidator := &temporaryCreditAvailableCreditInvalidatorStub{}
	creditService := NewTemporaryCreditServiceWithAvailableCreditInvalidator(creditRepo, invalidator)
	auditRepo := &temporaryCreditAuditRepositoryStub{}
	service := NewAdminTemporaryCreditService(db, creditService, auditRepo)
	claim := newAtomicClaimForTest(businessNow.Add(24 * time.Hour))

	mock.ExpectBegin()
	expectAdminGrantUserLocks(mock, userID, adminID)
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(businessNow))
	mock.ExpectExec("UPDATE idempotency_records").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := service.GrantAtomic(context.Background(), userID, adminID, 1.25, "campaign", claim)

	require.NoError(t, err)
	require.Equal(t, int64(42), result.TemporaryCreditGrantID)
	require.Equal(t, "1.25000000", result.Amount)
	require.Equal(t, "1.25000000", result.RemainingAmount)
	require.Equal(t, "campaign", result.Notes)
	require.Equal(t, time.Date(2026, time.July, 16, 16, 0, 0, 0, time.UTC), result.ExpiresAt)
	require.NotNil(t, creditRepo.created)
	require.Equal(t, adminID, *creditRepo.created.GrantedBy)
	require.Equal(t, TemporaryCreditSourceAdminGrant, creditRepo.created.Source)
	require.Equal(t, 1, invalidator.calls)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminTemporaryCreditServiceRollsBackWhenSuccessDTOPersistenceFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	userID := int64(42)
	adminID := int64(99)
	businessNow := time.Date(2026, time.July, 16, 15, 59, 59, 0, time.UTC)
	creditRepo := &temporaryCreditRepositoryRecorder{}
	invalidator := &temporaryCreditAvailableCreditInvalidatorStub{}
	creditService := NewTemporaryCreditServiceWithAvailableCreditInvalidator(creditRepo, invalidator)
	service := NewAdminTemporaryCreditService(db, creditService, &temporaryCreditAuditRepositoryStub{})
	claim := newAtomicClaimForTest(businessNow.Add(24 * time.Hour))

	mock.ExpectBegin()
	expectAdminGrantUserLocks(mock, userID, adminID)
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(businessNow))
	mock.ExpectExec("UPDATE idempotency_records").
		WillReturnError(errors.New("idempotency store unavailable"))
	mock.ExpectRollback()

	_, err = service.GrantAtomic(context.Background(), userID, adminID, 1.25, "campaign", claim)

	require.ErrorIs(t, err, ErrIdempotencyStoreUnavail)
	require.Zero(t, invalidator.calls)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminTemporaryCreditServiceListAuditValidatesAndDelegates(t *testing.T) {
	auditRepo := &temporaryCreditAuditRepositoryStub{items: []TemporaryCreditAuditItem{}, total: 3}
	service := NewAdminTemporaryCreditService(nil, nil, auditRepo)

	items, total, err := service.ListAudit(context.Background(), 42, 2, 20)
	require.NoError(t, err)
	require.Empty(t, items)
	require.Equal(t, int64(3), total)
	require.Equal(t, int64(42), auditRepo.userID)
	require.Equal(t, 2, auditRepo.page)
	require.Equal(t, 20, auditRepo.pageSize)

	_, _, err = service.ListAudit(context.Background(), 42, 0, 20)
	require.ErrorIs(t, err, ErrTemporaryCreditPaginationInvalid)
	_, _, err = service.ListAudit(context.Background(), 42, 1, 1001)
	require.ErrorIs(t, err, ErrTemporaryCreditPaginationInvalid)
}
