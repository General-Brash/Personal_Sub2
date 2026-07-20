package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildContentModerationLogWhere_BlockedIncludesAllBlockActions(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{Result: "blocked"})

	require.Empty(t, args)
	sql := strings.Join(where, " AND ")
	require.Contains(t, sql, "l.action IN ('block', 'keyword_block', 'hash_block', 'intent_block', 'intent_error_block')")
	require.NotContains(t, sql, "l.action = 'block'")
}

func TestBuildContentModerationLogWhere_IntentReviewIsExplicit(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{Result: "intent_review"})

	require.Empty(t, args)
	require.Contains(t, strings.Join(where, " AND "), "l.action = 'intent_review'")
}

func TestContentModerationRepositoryCreateLogPersistsReviewMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	createdAt := time.Now().UTC()
	log := &service.ContentModerationLog{
		RequestID:         "request-1",
		UserEmail:         "user@example.com",
		APIKeyName:        "key",
		GroupName:         "group",
		Endpoint:          "/v1/responses",
		Provider:          "openai",
		Model:             "gpt-test",
		Mode:              "pre_block",
		Action:            service.ContentModerationActionIntentBlock,
		Flagged:           true,
		HighestCategory:   service.ContentModerationSecondaryReviewLabelActionableProbe,
		HighestScore:      0.96,
		CategoryScores:    map[string]float64{"actionable_probe": 0.96},
		ThresholdSnapshot: map[string]float64{"intent_block": 0.9},
		ReviewMetadata:    map[string]string{"model_version": "intent-v1"},
		InputExcerpt:      "input",
		MatchedKeyword:    "探测",
	}
	mock.ExpectQuery(`(?s)INSERT INTO content_moderation_logs .*review_metadata.*RETURNING id, created_at`).
		WithArgs(
			"request-1", nil, "user@example.com", nil, "key", nil, "group",
			"/v1/responses", "openai", "gpt-test", "pre_block", service.ContentModerationActionIntentBlock,
			true, service.ContentModerationSecondaryReviewLabelActionableProbe, 0.96,
			`{"actionable_probe":0.96}`, `{"intent_block":0.9}`, `{"model_version":"intent-v1"}`,
			"input", nil, "", 0, false, false, nil, "探测",
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(42, createdAt))

	err = repo.CreateLog(context.Background(), log)

	require.NoError(t, err)
	require.Equal(t, int64(42), log.ID)
	require.Equal(t, createdAt, log.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryListLogsReturnsReviewMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	createdAt := time.Now().UTC()
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM content_moderation_logs l WHERE l.id IS NOT NULL`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	columns := []string{
		"id", "request_id", "user_id", "user_email", "api_key_id", "api_key_name", "group_id", "group_name",
		"endpoint", "provider", "model", "mode", "action", "flagged", "highest_category", "highest_score",
		"category_scores", "threshold_snapshot", "review_metadata", "input_excerpt", "upstream_latency_ms", "error",
		"violation_count", "auto_banned", "email_sent", "user_status", "queue_delay_ms", "matched_keyword", "created_at",
	}
	mock.ExpectQuery(`(?s)SELECT .*review_metadata.*FROM content_moderation_logs`).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			42, "request-1", nil, "user@example.com", nil, "key", nil, "group",
			"/v1/responses", "openai", "gpt-test", "pre_block", service.ContentModerationActionIntentBlock, true,
			service.ContentModerationSecondaryReviewLabelActionableProbe, 0.96,
			`{"actionable_probe":0.96}`, `{"intent_block":0.9}`,
			`{"model_version":"intent-v1","trace_id":"trace-1","review_mode":"enforce","fallback":"none"}`,
			"input", 25, "", 0, false, false, "active", nil, "探测", createdAt,
		))

	logs, page, err := repo.ListLogs(context.Background(), service.ContentModerationLogFilter{})

	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, logs, 1)
	require.Equal(t, "intent-v1", logs[0].ReviewMetadata["model_version"])
	require.Equal(t, "trace-1", logs[0].ReviewMetadata["trace_id"])
	require.Equal(t, "enforce", logs[0].ReviewMetadata["review_mode"])
	require.Equal(t, "none", logs[0].ReviewMetadata["fallback"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryCountFlaggedByUserSince_ExcludesHashBlock(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	since := time.Now().Add(-time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("AND action <> 'hash_block'")).
		WithArgs(int64(1001), since, false).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	count, err := repo.CountFlaggedByUserSince(context.Background(), 1001, since, false)

	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryCountFlaggedByUserSince_ExcludesCyberPolicyWhenRequested(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	since := time.Now().Add(-time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("AND ($3::bool IS FALSE OR action <> 'cyber_policy')")).
		WithArgs(int64(1001), since, true).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	count, err := repo.CountFlaggedByUserSince(context.Background(), 1001, since, true)

	require.NoError(t, err)
	require.Equal(t, 3, count)
	require.NoError(t, mock.ExpectationsWereMet())
}
