package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogInsertCarriesUpstreamRequestIDInRawShape(t *testing.T) {
	id := "upstream-123"
	prepared := prepareUsageLogInsert(&service.UsageLog{
		APIKeyID: 7, Model: "gpt-4.1", UpstreamRequestID: &id,
	})
	require.Len(t, prepared.args, len(usageLogInsertArgTypes))

	index := len(usageLogInsertArgTypes) - 4
	got, ok := prepared.args[index].(sql.NullString)
	require.True(t, ok)
	require.True(t, got.Valid)
	require.Equal(t, "upstream-123", got.String)
	require.Equal(t, "text", usageLogInsertArgTypes[index])
}

func TestUsageLogSelectColumnsIncludeUpstreamRequestIDOnce(t *testing.T) {
	require.Equal(t, 1, strings.Count(usageLogSelectColumns, "upstream_request_id"))
	require.Contains(t, usageLogSelectColumns, "account_stats_cost, upstream_request_id, session_id, native_compaction_v2, created_at")
}

func TestUsageLogRepositoryUpstreamRequestIDConfiguredRoundTrip(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	createdAt := time.Date(2026, 9, 7, 15, 30, 0, 0, time.UTC)
	upstreamRequestID := "configured-upstream-501"
	sessionID := "session-501"
	requestedEffort := "max"
	log := &service.UsageLog{
		UserID:                   11,
		APIKeyID:                 21,
		AccountID:                31,
		RequestID:                "client-501",
		Model:                    "gpt-5.4",
		RequestedModel:           "gpt-5.4",
		InputTokens:              10,
		OutputTokens:             5,
		TotalCost:                1.25,
		ActualCost:               1.0,
		RateMultiplier:           1.0,
		BillingType:              service.BillingTypeBalance,
		RequestType:              service.RequestTypeSync,
		RequestedReasoningEffort: &requestedEffort,
		UpstreamRequestID:        &upstreamRequestID,
		SessionID:                &sessionID,
		NativeCompactionV2:       true,
		CreatedAt:                createdAt,
	}
	prepared := prepareUsageLogInsert(log)

	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(anySliceToDriverValues(prepared.args)...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(501), createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.Equal(t, int64(501), log.ID)

	columns := strings.Split(usageLogSelectColumns, ", ")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + usageLogSelectColumns + " FROM usage_logs WHERE id = $1")).
		WithArgs(int64(501)).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(usageLogSQLMockRow(
			501,
			sql.NullString{String: upstreamRequestID, Valid: true},
			sql.NullString{String: sessionID, Valid: true},
			"max",
			int64(service.RequestTypeSync),
			false,
			false,
			true,
			createdAt,
		)...))

	got, err := repo.GetByID(context.Background(), 501)
	require.NoError(t, err)
	require.NotNil(t, got.UpstreamRequestID)
	require.Equal(t, upstreamRequestID, *got.UpstreamRequestID)
	require.NotNil(t, got.SessionID)
	require.Equal(t, sessionID, *got.SessionID)
	require.NotNil(t, got.RequestedReasoningEffort)
	require.Equal(t, "max", *got.RequestedReasoningEffort)
	require.True(t, got.NativeCompactionV2)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryUpstreamRequestIDNilPersistsSQLNull(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	createdAt := time.Date(2026, 9, 7, 15, 45, 0, 0, time.UTC)
	log := &service.UsageLog{
		UserID:      12,
		APIKeyID:    22,
		AccountID:   32,
		RequestID:   "client-nil-upstream",
		Model:       "gpt-5",
		RequestType: service.RequestTypeSync,
		CreatedAt:   createdAt,
	}
	prepared := prepareUsageLogInsert(log)
	upstreamIndex := len(usageLogInsertArgTypes) - 4
	require.Equal(t, sql.NullString{}, prepared.args[upstreamIndex])

	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(anySliceToDriverValues(prepared.args)...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(502), createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.Nil(t, log.UpstreamRequestID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryUpstreamRequestIDLegacyNullRecord(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	createdAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	columns := strings.Split(usageLogSelectColumns, ", ")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT " + usageLogSelectColumns + " FROM usage_logs WHERE id = $1")).
		WithArgs(int64(503)).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(usageLogSQLMockRow(
			503,
			sql.NullString{},
			sql.NullString{String: "legacy-session", Valid: true},
			nil,
			int64(service.RequestTypeUnknown),
			true,
			false,
			false,
			createdAt,
		)...))

	got, err := repo.GetByID(context.Background(), 503)
	require.NoError(t, err)
	require.Nil(t, got.UpstreamRequestID)
	require.NotNil(t, got.SessionID)
	require.Equal(t, "legacy-session", *got.SessionID)
	require.Nil(t, got.RequestedReasoningEffort)
	require.False(t, got.NativeCompactionV2)
	require.Equal(t, service.RequestTypeStream, got.RequestType)
	require.True(t, got.Stream)
	require.False(t, got.OpenAIWSMode)
	require.NoError(t, mock.ExpectationsWereMet())
}

func usageLogSQLMockRow(
	id int64,
	upstreamRequestID sql.NullString,
	sessionID sql.NullString,
	requestedReasoningEffort driver.Value,
	requestType int64,
	stream bool,
	openAIWSMode bool,
	nativeCompactionV2 bool,
	createdAt time.Time,
) []driver.Value {
	return []driver.Value{
		id, int64(11), int64(21), int64(31), "client-request", "gpt-5.4", "gpt-5.4",
		nil, nil, nil, nil, nil,
		int64(10), int64(5), int64(0), int64(0), int64(0), int64(0),
		int64(0), float64(0), int64(0), float64(0),
		float64(0.5), float64(0.25), float64(0), float64(0), float64(1.25), float64(1.0), float64(1.0), nil,
		int64(service.BillingTypeBalance), requestType, stream, openAIWSMode,
		nil, nil, nil, nil,
		int64(0), nil, nil, nil, nil, nil,
		int64(0), nil, nil,
		"priority", "high", requestedReasoningEffort, "/v1/responses", "/v1/responses", false, false,
		nil, nil, nil, nil, nil,
		upstreamRequestID, sessionID, nativeCompactionV2, createdAt,
	}
}
