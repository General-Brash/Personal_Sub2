package service

import (
	"context"
	"database/sql"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type temporaryCreditRepositoryRecorder struct {
	created          *TemporaryCreditGrant
	consumeAmount    float64
	consumeReference TemporaryCreditConsumptionReference
	consumeTx        *sql.Tx
}

func (r *temporaryCreditRepositoryRecorder) CreateGrant(_ context.Context, grant TemporaryCreditGrant) (*TemporaryCreditGrant, error) {
	r.created = &grant
	grant.ID = 42
	return &grant, nil
}

func (r *temporaryCreditRepositoryRecorder) CreateGrantTx(_ context.Context, _ *sql.Tx, grant TemporaryCreditGrant) (*TemporaryCreditGrant, error) {
	r.created = &grant
	grant.ID = 42
	return &grant, nil
}

func (r *temporaryCreditRepositoryRecorder) AvailableSummary(context.Context, int64) (float64, *time.Time, error) {
	return 0, nil, nil
}

func (r *temporaryCreditRepositoryRecorder) ConsumeFEFO(_ context.Context, tx *sql.Tx, _ int64, amount float64, reference TemporaryCreditConsumptionReference) (float64, error) {
	r.consumeTx = tx
	r.consumeAmount = amount
	r.consumeReference = reference
	return 0, nil
}

func TestTemporaryCreditServiceRejectsNonPositiveNonFiniteAndOverflowAmounts(t *testing.T) {
	require.NoError(t, ValidateTemporaryCreditAmount(1.23456789))

	for _, value := range []float64{
		0,
		-1,
		math.NaN(),
		math.Inf(1),
		maxLedgerAmount,
	} {
		require.Error(t, ValidateTemporaryCreditAmount(value))
	}
}

func TestTemporaryCreditServiceCreateGrantPersistsValidatedAmount(t *testing.T) {
	repo := &temporaryCreditRepositoryRecorder{}
	clock := func() time.Time {
		return time.Date(2026, 7, 13, 16, 30, 0, 0, time.UTC)
	}
	svc := NewTemporaryCreditServiceWithClock(repo, clock)
	adminID := int64(13)

	grant, err := svc.CreateGrant(context.Background(), CreateTemporaryCreditGrantInput{
		UserID:    7,
		Source:    TemporaryCreditSourceAdminGrant,
		Amount:    2.987654324,
		Notes:     "manual grant",
		GrantedBy: &adminID,
	})

	require.NoError(t, err)
	require.EqualValues(t, 42, grant.ID)
	require.NotNil(t, repo.created)
	require.Equal(t, "2.98765432", formatLedgerAmount(repo.created.Amount))
	require.Equal(t, repo.created.Amount, repo.created.RemainingAmount)
	require.Equal(t, clock(), repo.created.AvailableAt())
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 15, 0, 0, 0, 0, shanghai), repo.created.ExpiresAt())
}

func TestTemporaryCreditServiceCreateAdminGrantUsesLedgerAndShanghaiExpiry(t *testing.T) {
	repo := &temporaryCreditRepositoryRecorder{}
	clock := func() time.Time {
		return time.Date(2026, 7, 13, 16, 30, 0, 0, time.UTC)
	}
	svc := NewTemporaryCreditServiceWithClock(repo, clock)

	grant, err := svc.CreateAdminGrant(context.Background(), 7, 13, 3.5, "manual campaign grant")

	require.NoError(t, err)
	require.EqualValues(t, 42, grant.ID)
	require.NotNil(t, repo.created)
	require.Equal(t, TemporaryCreditSourceAdminGrant, repo.created.Source)
	require.Equal(t, clock(), repo.created.AvailableAt())
	require.Equal(t, "3.50000000", formatLedgerAmount(repo.created.Amount))
	require.Equal(t, "3.50000000", formatLedgerAmount(repo.created.RemainingAmount))
	require.Equal(t, int64(13), *repo.created.GrantedBy)
	require.Equal(t, "manual campaign grant", repo.created.Notes)
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 15, 0, 0, 0, 0, shanghai), repo.created.ExpiresAt())
}

func TestTemporaryCreditRequestReferencePreservesOriginalBillingIdentifier(t *testing.T) {
	raw := "upstream-request-123"
	reference, err := normalizeTemporaryCreditReference(TemporaryCreditConsumptionReference{RequestID: raw})

	require.NoError(t, err)
	require.Equal(t, raw, reference.RequestID)
}

func TestTemporaryCreditRequestReferenceAcceptsMaximumRawAuditIdentifier(t *testing.T) {
	raw := strings.Repeat("x", temporaryCreditRequestIDMaxLen)
	reference, err := normalizeTemporaryCreditReference(TemporaryCreditConsumptionReference{RequestID: raw})

	require.NoError(t, err)
	require.Equal(t, raw, reference.RequestID)
}

func TestTemporaryCreditGrantDoesNotExposeWritableExpiration(t *testing.T) {
	_, exposed := reflect.TypeOf(TemporaryCreditGrant{}).FieldByName("ExpiresAt")
	require.False(t, exposed, "repository callers must not be able to choose an arbitrary expiration")
}

func TestTemporaryCreditServiceConsumeFEFORejectsNilCallerTransaction(t *testing.T) {
	repo := &temporaryCreditRepositoryRecorder{}
	svc := NewTemporaryCreditService(repo)

	_, err := svc.ConsumeFEFO(context.Background(), nil, 4, 1.25, TemporaryCreditConsumptionReference{RequestID: "external-request"})

	require.ErrorIs(t, err, ErrTemporaryCreditTransactionRequired)
	require.Nil(t, repo.consumeTx)
}

func TestTemporaryCreditServiceConsumeFEFOPassesCallerTransactionAndOriginalReference(t *testing.T) {
	repo := &temporaryCreditRepositoryRecorder{}
	svc := NewTemporaryCreditService(repo)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
	}()
	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() {
		mock.ExpectRollback()
		require.NoError(t, tx.Rollback())
		require.NoError(t, mock.ExpectationsWereMet())
	}()

	remaining, err := svc.ConsumeFEFO(context.Background(), tx, 4, 1.25, TemporaryCreditConsumptionReference{RequestID: "external-request"})
	require.NoError(t, err)
	require.Zero(t, remaining)
	require.Same(t, tx, repo.consumeTx)
	require.Equal(t, "1.25000000", formatLedgerAmount(repo.consumeAmount))
	require.Equal(t, "external-request", repo.consumeReference.RequestID)
}
