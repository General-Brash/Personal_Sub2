package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func newDataCleanupSQLMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

func testDataCleanupService(db *sql.DB) *DataCleanupService {
	return &DataCleanupService{db: db, previewKey: []byte(strings.Repeat("k", 32))}
}

func TestDataCleanupPreviewTokenBindsNormalizedFilterAndSnapshot(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	end := start.Add(24 * time.Hour)
	_, filter, err := validateDataCleanupFilter(DataCleanupFilter{
		Category:  " OPS_ERROR_LOGS ",
		Mode:      " RANGE ",
		StartTime: &start,
		EndTime:   &end,
	})
	require.NoError(t, err)

	svc := testDataCleanupService(nil)
	preview := &DataCleanupPreview{
		MatchedRows:   7,
		BlockedRows:   2,
		snapshotMaxID: 91,
	}
	token, err := svc.signPreviewToken(filter, preview)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	payload, err := svc.verifyPreviewToken(token)
	require.NoError(t, err)
	require.True(t, sameDataCleanupFilter(filter, payload.Filter))
	require.Equal(t, int64(7), payload.PreviewRows)
	require.Equal(t, int64(2), payload.BlockedRows)
	require.Equal(t, int64(91), payload.SnapshotMaxID)

	changed := filter
	changedEnd := end.Add(time.Hour).UTC()
	changed.EndTime = &changedEnd
	require.False(t, sameDataCleanupFilter(changed, payload.Filter))
	_, err = svc.verifyPreviewToken(token[:len(token)-1] + "x")
	require.Error(t, err)
}

func TestDataCleanupExecuteUsesSnapshotBoundAndIndependentAudit(t *testing.T) {
	db, mock := newDataCleanupSQLMock(t)
	svc := testDataCleanupService(db)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	filter := DataCleanupFilter{Category: "ops_error_logs", Mode: DataCleanupModeRange, StartTime: &start, EndTime: &end}
	preview := &DataCleanupPreview{MatchedRows: 2, snapshotMaxID: 10}
	token, err := svc.signPreviewToken(filter, preview)
	require.NoError(t, err)

	mock.ExpectQuery("INSERT INTO data_cleanup_audits").
		WithArgs(int64(7), "admin@example.com", "jwt", "ops_error_logs", "range", sqlmock.AnyArg(), int64(2), int64(0), "running").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(3)))
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*), COALESCE(MAX(target.id), 0), MIN(target.created_at), MAX(target.created_at) FROM ops_error_logs AS target WHERE target.created_at >= $1 AND target.created_at < $2 AND target.id <= $3")).
		WithArgs(start, end, int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"count", "max_id", "min_time", "max_time"}).AddRow(int64(2), int64(10), start, end.Add(-time.Second)))
	mock.ExpectExec("DELETE FROM ops_error_logs AS target").
		WithArgs(start, end, int64(10), int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	mock.ExpectExec("UPDATE data_cleanup_audits").
		WithArgs(int64(3), "succeeded", int64(2), "").
		WillReturnResult(sqlmock.NewResult(0, 1))

	result, err := svc.Execute(context.Background(), filter, 2, "DELETE ops_error_logs 2", token, DataCleanupOperator{
		UserID: 7, Email: "admin@example.com", AuthMethod: "jwt",
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), result.DeletedRows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDataCleanupFailureAuditUsesDetachedContext(t *testing.T) {
	db, mock := newDataCleanupSQLMock(t)
	svc := testDataCleanupService(db)
	preview := &DataCleanupPreview{MatchedRows: 3, snapshotMaxID: 8}
	mock.ExpectExec("UPDATE data_cleanup_audits").
		WithArgs(int64(4), "failed", int64(0), "request canceled").
		WillReturnResult(sqlmock.NewResult(0, 1))

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, canceled.Err())
	svc.finishAuditFailureDetached(4, DataCleanupFilter{Category: "ops_error_logs", Mode: "all"}, preview, DataCleanupOperator{UserID: 1}, errors.New("request canceled"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDataCleanupDateBoundaryUsesBusinessTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	instant := time.Date(2026, 7, 1, 16, 0, 0, 0, time.UTC)
	require.Equal(t, "2026-07-02", dataCleanupDateBoundary(instant, loc))
}
