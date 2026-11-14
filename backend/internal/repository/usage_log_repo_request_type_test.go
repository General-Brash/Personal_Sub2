package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositoryCreateSyncRequestTypeAndLegacyFields(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	createdAt := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	log := &service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-1",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		InputTokens:    10,
		OutputTokens:   20,
		TotalCost:      1,
		ActualCost:     1,
		BillingType:    service.BillingTypeBalance,
		RequestType:    service.RequestTypeWSV2,
		Stream:         false,
		OpenAIWSMode:   false,
		CreatedAt:      createdAt,
	}

	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(
			log.UserID,
			log.APIKeyID,
			log.AccountID,
			log.RequestID,
			log.Model,
			log.RequestedModel,
			sqlmock.AnyArg(), // upstream_model
			sqlmock.AnyArg(), // upstream_response_model
			sqlmock.AnyArg(), // upstream_model_mismatch
			sqlmock.AnyArg(), // group_id
			sqlmock.AnyArg(), // subscription_id
			log.InputTokens,
			log.OutputTokens,
			log.CacheCreationTokens,
			log.CacheReadTokens,
			log.CacheCreation5mTokens,
			log.CacheCreation1hTokens,
			log.ImageOutputTokens,
			log.ImageOutputCost,
			log.ImageInputTokens,
			log.ImageInputCost,
			log.InputCost,
			log.OutputCost,
			log.CacheCreationCost,
			log.CacheReadCost,
			log.TotalCost,
			log.ActualCost,
			log.RateMultiplier,
			log.AccountRateMultiplier,
			log.BillingType,
			int16(service.RequestTypeWSV2),
			true,
			true,
			sqlmock.AnyArg(), // duration_ms
			sqlmock.AnyArg(), // first_token_ms
			sqlmock.AnyArg(), // user_agent
			sqlmock.AnyArg(), // ip_address
			log.ImageCount,
			sqlmock.AnyArg(), // image_size
			sqlmock.AnyArg(), // image_input_size
			sqlmock.AnyArg(), // image_output_size
			sqlmock.AnyArg(), // image_size_source
			sqlmock.AnyArg(), // image_size_breakdown
			sqlmock.AnyArg(), // video_count
			sqlmock.AnyArg(), // video_resolution
			sqlmock.AnyArg(), // video_duration_seconds
			sqlmock.AnyArg(), // service_tier
			sqlmock.AnyArg(), // reasoning_effort
			sqlmock.AnyArg(), // requested_reasoning_effort
			sqlmock.AnyArg(), // inbound_endpoint
			sqlmock.AnyArg(), // upstream_endpoint
			log.CacheTTLOverridden,
			log.LongContextBillingApplied,
			sqlmock.AnyArg(), // channel_id
			sqlmock.AnyArg(), // model_mapping_chain
			sqlmock.AnyArg(), // billing_tier
			sqlmock.AnyArg(), // billing_mode
			sqlmock.AnyArg(), // account_stats_cost
			sql.NullString{}, // upstream_request_id
			sql.NullString{}, // session_id
			log.NativeCompactionV2,
			createdAt,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(99), createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.Equal(t, int64(99), log.ID)
	require.Nil(t, log.ServiceTier)
	require.Equal(t, service.RequestTypeWSV2, log.RequestType)
	require.True(t, log.Stream)
	require.True(t, log.OpenAIWSMode)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryCreate_PersistsServiceTier(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	createdAt := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	serviceTier := "priority"
	log := &service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-service-tier",
		Model:          "gpt-5.4",
		RequestedModel: "gpt-5.4",
		ServiceTier:    &serviceTier,
		CreatedAt:      createdAt,
	}

	mock.ExpectQuery("INSERT INTO usage_logs").
		WithArgs(
			log.UserID,
			log.APIKeyID,
			log.AccountID,
			log.RequestID,
			log.Model,
			log.RequestedModel,
			sqlmock.AnyArg(), // upstream_model
			sqlmock.AnyArg(), // upstream_response_model
			sqlmock.AnyArg(), // upstream_model_mismatch
			sqlmock.AnyArg(), // group_id
			sqlmock.AnyArg(), // subscription_id
			log.InputTokens,
			log.OutputTokens,
			log.CacheCreationTokens,
			log.CacheReadTokens,
			log.CacheCreation5mTokens,
			log.CacheCreation1hTokens,
			log.ImageOutputTokens,
			log.ImageOutputCost,
			log.ImageInputTokens,
			log.ImageInputCost,
			log.InputCost,
			log.OutputCost,
			log.CacheCreationCost,
			log.CacheReadCost,
			log.TotalCost,
			log.ActualCost,
			log.RateMultiplier,
			log.AccountRateMultiplier,
			log.BillingType,
			int16(service.RequestTypeSync),
			false,
			false,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			log.ImageCount,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(), // image_input_size
			sqlmock.AnyArg(), // image_output_size
			sqlmock.AnyArg(), // image_size_source
			sqlmock.AnyArg(), // image_size_breakdown
			sqlmock.AnyArg(), // video_count
			sqlmock.AnyArg(), // video_resolution
			sqlmock.AnyArg(), // video_duration_seconds
			serviceTier,
			sqlmock.AnyArg(), // reasoning_effort
			sqlmock.AnyArg(), // requested_reasoning_effort
			sqlmock.AnyArg(), // inbound_endpoint
			sqlmock.AnyArg(), // upstream_endpoint
			log.CacheTTLOverridden,
			log.LongContextBillingApplied,
			sqlmock.AnyArg(), // channel_id
			sqlmock.AnyArg(), // model_mapping_chain
			sqlmock.AnyArg(), // billing_tier
			sqlmock.AnyArg(), // billing_mode
			sqlmock.AnyArg(), // account_stats_cost
			sql.NullString{}, // upstream_request_id
			sql.NullString{}, // session_id
			log.NativeCompactionV2,
			createdAt,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(100), createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildUsageLogBestEffortInsertQuery_IncludesRequestedModelColumn(t *testing.T) {
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-best-effort-query",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		CreatedAt:      time.Date(2025, 1, 3, 12, 0, 0, 0, time.UTC),
	})

	query, args := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})

	require.Contains(t, query, "INSERT INTO usage_logs (")
	require.Contains(t, query, "\n\t\t\tmodel,\n\t\t\trequested_model,\n\t\t\tupstream_model,\n\t\t\tupstream_response_model,\n\t\t\tupstream_model_mismatch,")
	require.Contains(t, query, "\n\t\t\trequest_id,\n\t\t\tmodel,\n\t\t\trequested_model,\n\t\t\tupstream_model,\n\t\t\tupstream_response_model,\n\t\t\tupstream_model_mismatch,")
	require.Len(t, args, len(prepared.args))
	require.Equal(t, prepared.args[5], args[5])
}

func TestExecUsageLogInsertNoResult_PersistsRequestedModel(t *testing.T) {
	db, mock := newSQLMock(t)
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-best-effort-exec",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		CreatedAt:      time.Date(2025, 1, 4, 12, 0, 0, 0, time.UTC),
	})

	mock.ExpectExec("INSERT INTO usage_logs").
		WithArgs(anySliceToDriverValues(prepared.args)...).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := execUsageLogInsertNoResult(context.Background(), db, prepared)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPrepareUsageLogInsert_ArgCountMatchesTypes(t *testing.T) {
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:         1,
		APIKeyID:       2,
		AccountID:      3,
		RequestID:      "req-arg-count",
		Model:          "gpt-5",
		RequestedModel: "gpt-5",
		CreatedAt:      time.Date(2025, 1, 5, 12, 0, 0, 0, time.UTC),
	})

	require.Len(t, prepared.args, len(usageLogInsertArgTypes))
}

func TestPrepareUsageLogInsert_PersistsImageSizeMetadata(t *testing.T) {
	imageSize := "4K"
	inputSize := "1024x1024"
	outputSize := "3840x2160"
	source := "output"
	prepared := prepareUsageLogInsert(&service.UsageLog{
		UserID:             1,
		APIKeyID:           2,
		AccountID:          3,
		RequestID:          "req-image-metadata",
		Model:              "gpt-image-2",
		RequestedModel:     "gpt-image-2",
		ImageCount:         2,
		ImageSize:          &imageSize,
		ImageInputSize:     &inputSize,
		ImageOutputSize:    &outputSize,
		ImageSizeSource:    &source,
		ImageSizeBreakdown: map[string]int{"1K": 1, "4K": 1},
		CreatedAt:          time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC),
	})

	require.Equal(t, sql.NullString{String: imageSize, Valid: true}, prepared.args[38])
	require.Equal(t, sql.NullString{String: inputSize, Valid: true}, prepared.args[39])
	require.Equal(t, sql.NullString{String: outputSize, Valid: true}, prepared.args[40])
	require.Equal(t, sql.NullString{String: source, Valid: true}, prepared.args[41])
	breakdownJSON, ok := prepared.args[42].(string)
	require.True(t, ok)
	require.JSONEq(t, `{"1K":1,"4K":1}`, breakdownJSON)
}

func TestCoalesceTrimmedString(t *testing.T) {
	require.Equal(t, "fallback", coalesceTrimmedString(sql.NullString{}, "fallback"))
	require.Equal(t, "fallback", coalesceTrimmedString(sql.NullString{Valid: true, String: "   "}, "fallback"))
	require.Equal(t, "value", coalesceTrimmedString(sql.NullString{Valid: true, String: "value"}, "fallback"))
}

func TestAppendUsageLogBillingModeWhereCondition(t *testing.T) {
	tests := []struct {
		name          string
		billingMode   string
		wantCondition string
	}{
		{
			name:          "image includes explicit image and legacy image rows",
			billingMode:   string(service.BillingModeImage),
			wantCondition: "(billing_mode = $1 OR ((billing_mode IS NULL OR billing_mode = '') AND COALESCE(image_count, 0) > 0))",
		},
		{
			name:          "video remains exact",
			billingMode:   string(service.BillingModeVideo),
			wantCondition: "billing_mode = $1",
		},
		{
			name:          "token includes legacy non-image rows",
			billingMode:   string(service.BillingModeToken),
			wantCondition: "(billing_mode = $1 OR ((billing_mode IS NULL OR billing_mode = '') AND COALESCE(image_count, 0) <= 0))",
		},
		{
			name:          "per request remains exact",
			billingMode:   string(service.BillingModePerRequest),
			wantCondition: "billing_mode = $1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conditions, args := appendUsageLogBillingModeWhereCondition(nil, nil, tt.billingMode)
			require.Equal(t, []string{tt.wantCondition}, conditions)
			require.Equal(t, []any{tt.billingMode}, args)
		})
	}
}

func TestAppendUsageLogBillingModeWhereConditionWithAlias(t *testing.T) {
	conditions, args := appendUsageLogBillingModeWhereConditionWithAlias(nil, nil, string(service.BillingModeImage), "ul")

	require.Equal(t, []string{"(ul.billing_mode = $1 OR ((ul.billing_mode IS NULL OR ul.billing_mode = '') AND COALESCE(ul.image_count, 0) > 0))"}, conditions)
	require.Equal(t, []any{string(service.BillingModeImage)}, args)
}

func TestAppendUsageLogBillingModeQueryFilter(t *testing.T) {
	query, args := appendUsageLogBillingModeQueryFilter("SELECT * FROM usage_logs WHERE user_id = $1", []any{int64(42)}, string(service.BillingModeToken), "")

	require.Equal(t, "SELECT * FROM usage_logs WHERE user_id = $1 AND (billing_mode = $2 OR ((billing_mode IS NULL OR billing_mode = '') AND COALESCE(image_count, 0) <= 0))", query)
	require.Equal(t, []any{int64(42), string(service.BillingModeToken)}, args)
}

func anySliceToDriverValues(values []any) []driver.Value {
	out := make([]driver.Value, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func TestUsageLogRepositoryListWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	requestType := int16(service.RequestTypeWSV2)
	stream := false
	filters := usagestats.UsageLogFilters{
		RequestType: &requestType,
		Stream:      &stream,
		ExactTotal:  true,
	}

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM usage_logs WHERE \\(request_type = \\$1 OR \\(request_type = 0 AND openai_ws_mode = TRUE\\)\\)").
		WithArgs(requestType).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery("SELECT .* FROM usage_logs WHERE \\(request_type = \\$1 OR \\(request_type = 0 AND openai_ws_mode = TRUE\\)\\) ORDER BY id DESC LIMIT \\$2 OFFSET \\$3").
		WithArgs(requestType, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	logs, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, filters)
	require.NoError(t, err)
	require.Empty(t, logs)
	require.NotNil(t, page)
	require.Equal(t, int64(0), page.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryListWithFiltersRequestID(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	filters := usagestats.UsageLogFilters{RequestID: " req-0123 "}

	mock.ExpectQuery("SELECT .* FROM usage_logs WHERE request_id = \\$1 ORDER BY id DESC LIMIT \\$2 OFFSET \\$3").
		WithArgs("req-0123", 21, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	logs, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, filters)
	require.NoError(t, err)
	require.Empty(t, logs)
	require.NotNil(t, page)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryListWithFiltersRequestedModelSource(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	filters := usagestats.UsageLogFilters{
		Model:             "gpt-5",
		ModelFilterSource: usagestats.ModelSourceRequested,
	}

	mock.ExpectQuery("SELECT .* FROM usage_logs WHERE COALESCE\\(NULLIF\\(TRIM\\(requested_model\\), ''\\), model\\) = \\$1 ORDER BY id DESC LIMIT \\$2 OFFSET \\$3").
		WithArgs("gpt-5", 21, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	logs, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 20}, filters)
	require.NoError(t, err)
	require.Empty(t, logs)
	require.NotNil(t, page)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetUsageTrendWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	requestType := int16(service.RequestTypeStream)
	stream := true

	mock.ExpectQuery("AND \\(request_type = \\$3 OR \\(request_type = 0 AND stream = TRUE AND openai_ws_mode = FALSE\\)\\)").
		WithArgs(start, end, requestType).
		WillReturnRows(sqlmock.NewRows([]string{"date", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost"}))

	trend, err := repo.GetUsageTrendWithFilters(context.Background(), start, end, "day", 0, 0, 0, 0, "", &requestType, &stream, nil)
	require.NoError(t, err)
	require.Empty(t, trend)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetUsageTrendWithUsageFiltersRequestedModelSource(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	filters := usagestats.UsageLogFilters{
		Model:             "gpt-5",
		ModelFilterSource: usagestats.ModelSourceRequested,
	}

	mock.ExpectQuery("AND COALESCE\\(NULLIF\\(TRIM\\(requested_model\\), ''\\), model\\) = \\$3").
		WithArgs(start, end, "gpt-5").
		WillReturnRows(sqlmock.NewRows([]string{"date", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost"}))

	trend, err := repo.GetUsageTrendWithUsageFilters(context.Background(), start, end, "day", filters)
	require.NoError(t, err)
	require.Empty(t, trend)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetModelStatsWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	requestType := int16(service.RequestTypeWSV2)
	stream := false

	mock.ExpectQuery("AND \\(request_type = \\$3 OR \\(request_type = 0 AND openai_ws_mode = TRUE\\)\\)").
		WithArgs(start, end, requestType).
		WillReturnRows(sqlmock.NewRows([]string{"model", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost", "account_cost"}))

	stats, err := repo.GetModelStatsWithFilters(context.Background(), start, end, 0, 0, 0, 0, &requestType, &stream, nil)
	require.NoError(t, err)
	require.Empty(t, stats)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetUserModelStatsUsesRequestedModel(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery("^"+regexp.QuoteMeta(`SELECT
		COALESCE(NULLIF(TRIM(requested_model), ''), model) as model,
		COALESCE(SUM(request_count), 0) as requests,
		COALESCE(SUM(input_tokens), 0) as input_tokens,
		COALESCE(SUM(output_tokens), 0) as output_tokens,
		COALESCE(SUM(cache_creation_tokens), 0) as cache_creation_tokens,
		COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens,
		COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as total_tokens,
		COALESCE(SUM(total_cost), 0) as cost,
		COALESCE(SUM(actual_cost), 0) as actual_cost,
		COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as account_cost
		FROM usage_statistics_range($1, $2) WHERE TRUE AND user_id = $3
		GROUP BY COALESCE(NULLIF(TRIM(requested_model), ''), model) ORDER BY total_tokens DESC`)+"$").
		WithArgs(start, end, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"model", "requests", "input_tokens", "output_tokens",
			"cache_creation_tokens", "cache_read_tokens", "total_tokens",
			"cost", "actual_cost", "account_cost",
		}).AddRow("gpt-5.5", int64(2), int64(10), int64(20), int64(0), int64(0), int64(30), 0.1, 0.08, 0.07))

	stats, err := repo.GetUserModelStats(context.Background(), 7, start, end)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	require.Equal(t, "gpt-5.5", stats[0].Model)
	require.Equal(t, int64(2), stats[0].Requests)
	require.Equal(t, int64(30), stats[0].TotalTokens)
	require.Equal(t, 0.1, stats[0].Cost)
	require.Equal(t, 0.08, stats[0].ActualCost)
	require.Equal(t, 0.07, stats[0].AccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetStatsWithFiltersRequestedModelSource(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	filters := usagestats.UsageLogFilters{
		Model:             "gpt-5",
		ModelFilterSource: usagestats.ModelSourceRequested,
		StartTime:         &start,
		EndTime:           &end,
	}

	source := "usage_statistics_range($2, $3) WHERE COALESCE(NULLIF(TRIM(requested_model), ''), model) = $1"
	mock.ExpectQuery(usageStatsSummaryExpectation(source)).
		WithArgs("gpt-5", start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests",
			"total_input_tokens",
			"total_output_tokens",
			"total_cache_tokens",
			"total_cache_creation_tokens",
			"total_cache_read_tokens",
			"total_cost",
			"total_actual_cost",
			"total_account_cost",
			"avg_duration_ms",
		}).AddRow(int64(1), int64(2), int64(3), int64(4), int64(1), int64(3), 1.2, 1.0, 1.2, 20.0))
	expectUsageStatsEndpointQueries(mock, " AND COALESCE(NULLIF(TRIM(requested_model), ''), model) = $3", start, end, "gpt-5")
	mock.ExpectQuery(usageStatsHistoryExpectation(source)).
		WithArgs("gpt-5", start, end).
		WillReturnRows(sqlmock.NewRows([]string{"includes_history", "history_start", "history_end", "history_timezone"}).
			AddRow(true, start, end, "Asia/Shanghai"))

	stats, err := repo.GetStatsWithFilters(context.Background(), filters)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.TotalRequests)
	require.True(t, stats.IncludesHistory)
	require.True(t, stats.Approximate)
	require.Equal(t, "day", stats.HistoryGranularity)
	require.Equal(t, "whole_intersecting_days", stats.HistoryRangePolicy)
	require.Equal(t, "Asia/Shanghai", stats.HistoryTimezone)
	require.Equal(t, &start, stats.HistoryStart)
	require.Equal(t, &end, stats.HistoryEnd)
	require.Len(t, stats.Endpoints, 1)
	require.Len(t, stats.UpstreamEndpoints, 1)
	require.Len(t, stats.EndpointPaths, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetStatsWithFiltersRequestTypePriority(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	requestType := int16(service.RequestTypeSync)
	stream := true
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	filters := usagestats.UsageLogFilters{
		RequestType: &requestType,
		Stream:      &stream,
		StartTime:   &start,
		EndTime:     &end,
	}

	source := "usage_statistics_range($2, $3) WHERE (request_type = $1 OR (request_type = 0 AND stream = FALSE AND openai_ws_mode = FALSE))"
	mock.ExpectQuery(usageStatsSummaryExpectation(source)).
		WithArgs(requestType, start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests",
			"total_input_tokens",
			"total_output_tokens",
			"total_cache_tokens",
			"total_cache_creation_tokens",
			"total_cache_read_tokens",
			"total_cost",
			"total_actual_cost",
			"total_account_cost",
			"avg_duration_ms",
		}).AddRow(int64(1), int64(2), int64(3), int64(4), int64(1), int64(3), 1.2, 1.0, 1.2, 20.0))
	expectUsageStatsEndpointQueries(mock, " AND (request_type = $3 OR (request_type = 0 AND stream = FALSE AND openai_ws_mode = FALSE))", start, end, requestType)
	mock.ExpectQuery(usageStatsHistoryExpectation(source)).
		WithArgs(requestType, start, end).
		WillReturnRows(sqlmock.NewRows([]string{"includes_history", "history_start", "history_end", "history_timezone"}).
			AddRow(false, nil, nil, nil))

	stats, err := repo.GetStatsWithFilters(context.Background(), filters)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.TotalRequests)
	require.Equal(t, int64(9), stats.TotalTokens)
	require.NotNil(t, stats.TotalAccountCost, "TotalAccountCost should always be returned")
	require.Equal(t, 1.2, *stats.TotalAccountCost)
	require.False(t, stats.IncludesHistory)
	require.Len(t, stats.Endpoints, 1)
	require.Len(t, stats.UpstreamEndpoints, 1)
	require.Len(t, stats.EndpointPaths, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetModelStatsAccountCostColumn(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery("FROM usage_statistics").
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"model", "requests", "input_tokens", "output_tokens",
			"cache_creation_tokens", "cache_read_tokens", "total_tokens",
			"cost", "actual_cost", "account_cost",
		}).
			AddRow("claude-opus-4-6", int64(10), int64(100), int64(200), int64(5), int64(3), int64(308), 2.5, 2.0, 1.8).
			AddRow("claude-sonnet-4-6", int64(5), int64(50), int64(100), int64(0), int64(0), int64(150), 1.0, 0.8, 0.7))

	results, err := repo.GetModelStatsWithFilters(context.Background(), start, end, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "claude-opus-4-6", results[0].Model)
	require.Equal(t, 2.5, results[0].Cost)
	require.Equal(t, 2.0, results[0].ActualCost)
	require.Equal(t, 1.8, results[0].AccountCost)
	require.Equal(t, "claude-sonnet-4-6", results[1].Model)
	require.Equal(t, 0.7, results[1].AccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetModelStatsWithUsageFiltersAppliesRequestedModelFilter(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	filters := usagestats.UsageLogFilters{Model: "gpt-5"}

	mock.ExpectQuery("AND COALESCE\\(NULLIF\\(TRIM\\(requested_model\\), ''\\), model\\) = \\$3").
		WithArgs(start, end, "gpt-5").
		WillReturnRows(sqlmock.NewRows([]string{
			"model", "requests", "input_tokens", "output_tokens",
			"cache_creation_tokens", "cache_read_tokens", "total_tokens",
			"cost", "actual_cost", "account_cost",
		}).AddRow("gpt-5", int64(1), int64(10), int64(20), int64(0), int64(0), int64(30), 0.1, 0.08, 0.07))

	results, err := repo.GetModelStatsWithUsageFiltersBySource(context.Background(), start, end, filters, usagestats.ModelSourceRequested)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "gpt-5", results[0].Model)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetGroupStatsAccountCostColumn(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery("FROM usage_statistics").
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"group_id", "group_name", "requests", "total_tokens",
			"cost", "actual_cost", "account_cost",
		}).
			AddRow(int64(1), "azure-cc", int64(100), int64(5000), 10.0, 8.5, 7.2).
			AddRow(int64(2), "max", int64(50), int64(2000), 5.0, 4.0, 3.5))

	results, err := repo.GetGroupStatsWithFilters(context.Background(), start, end, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, int64(1), results[0].GroupID)
	require.Equal(t, "azure-cc", results[0].GroupName)
	require.Equal(t, 10.0, results[0].Cost)
	require.Equal(t, 8.5, results[0].ActualCost)
	require.Equal(t, 7.2, results[0].AccountCost)
	require.Equal(t, int64(2), results[1].GroupID)
	require.Equal(t, 3.5, results[1].AccountCost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetGroupStatsWithUsageFiltersAppliesRequestedModelFilter(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	filters := usagestats.UsageLogFilters{Model: "gpt-5"}

	mock.ExpectQuery("AND COALESCE\\(NULLIF\\(TRIM\\(ul.requested_model\\), ''\\), ul.model\\) = \\$3").
		WithArgs(start, end, "gpt-5").
		WillReturnRows(sqlmock.NewRows([]string{
			"group_id", "group_name", "requests", "total_tokens",
			"cost", "actual_cost", "account_cost",
		}).AddRow(int64(1), "default", int64(1), int64(30), 0.1, 0.08, 0.07))

	results, err := repo.GetGroupStatsWithUsageFilters(context.Background(), start, end, filters)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, int64(1), results[0].GroupID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetStatsWithFiltersAlwaysReturnsAccountCost(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	// No AccountID filter set - TotalAccountCost should still be returned
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	filters := usagestats.UsageLogFilters{StartTime: &start, EndTime: &end}

	source := "usage_statistics_range($1, $2)"
	mock.ExpectQuery(usageStatsSummaryExpectation(source)).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_requests", "total_input_tokens", "total_output_tokens",
			"total_cache_tokens", "total_cache_creation_tokens", "total_cache_read_tokens",
			"total_cost", "total_actual_cost",
			"total_account_cost", "avg_duration_ms",
		}).AddRow(int64(50), int64(1000), int64(2000), int64(100), int64(60), int64(40), 15.0, 12.5, 11.0, 100.0))
	expectUsageStatsEndpointQueries(mock, "", start, end)
	mock.ExpectQuery(usageStatsHistoryExpectation(source)).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{"includes_history", "history_start", "history_end", "history_timezone"}).
			AddRow(false, nil, nil, nil))

	stats, err := repo.GetStatsWithFilters(context.Background(), filters)
	require.NoError(t, err)
	require.NotNil(t, stats.TotalAccountCost, "TotalAccountCost must always be returned, even without AccountID filter")
	require.Equal(t, 11.0, *stats.TotalAccountCost)
	require.Equal(t, int64(50), stats.TotalRequests)
	require.Equal(t, int64(3100), stats.TotalTokens)
	require.Equal(t, 15.0, stats.TotalCost)
	require.Equal(t, 12.5, stats.TotalActualCost)
	require.Equal(t, 100.0, stats.AverageDurationMs)
	require.Len(t, stats.Endpoints, 1)
	require.Len(t, stats.UpstreamEndpoints, 1)
	require.Len(t, stats.EndpointPaths, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func usageStatsSummaryExpectation(source string) string {
	return "^" + regexp.QuoteMeta(`SELECT
		COALESCE(SUM(request_count), 0) as total_requests,
		COALESCE(SUM(input_tokens), 0) as total_input_tokens,
		COALESCE(SUM(output_tokens), 0) as total_output_tokens,
		COALESCE(SUM(cache_creation_tokens + cache_read_tokens), 0) as total_cache_tokens,
		COALESCE(SUM(cache_creation_tokens), 0) as total_cache_creation_tokens,
		COALESCE(SUM(cache_read_tokens), 0) as total_cache_read_tokens,
		COALESCE(SUM(total_cost), 0) as total_cost,
		COALESCE(SUM(actual_cost), 0) as total_actual_cost,
		COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as total_account_cost,
		COALESCE(SUM(duration_ms)::numeric / NULLIF(SUM(duration_samples), 0), 0) as avg_duration_ms
		FROM `+source) + "$"
}

func usageStatsHistoryExpectation(source string) string {
	rangeCall, suffix, _ := strings.Cut(source, ")")
	source = strings.Replace(rangeCall, "usage_statistics_range(", "usage_statistics_archive_range(", 1) + ") ul" + suffix
	return "^" + regexp.QuoteMeta(`SELECT COALESCE(BOOL_OR(ul.is_archived), false),
		MIN(ul.created_at) FILTER (WHERE ul.is_archived), MAX(ul.bucket_end) FILTER (WHERE ul.is_archived),
		MIN(ul.bucket_timezone) FILTER (WHERE ul.is_archived) FROM `+source) + "$"
}

func expectUsageStatsEndpointQueries(mock sqlmock.Sqlmock, filter string, args ...driver.Value) {
	for _, dimension := range []string{
		"COALESCE(NULLIF(TRIM(inbound_endpoint), ''), 'unknown')",
		"COALESCE(NULLIF(TRIM(upstream_endpoint), ''), 'unknown')",
		"CONCAT( COALESCE(NULLIF(TRIM(inbound_endpoint), ''), 'unknown'), ' -> ', COALESCE(NULLIF(TRIM(upstream_endpoint), ''), 'unknown') )",
	} {
		query := `SELECT ` + dimension + ` AS endpoint,
			COALESCE(SUM(request_count), 0) AS requests,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) AS total_tokens,
			COALESCE(SUM(total_cost), 0) as cost,
			COALESCE(SUM(actual_cost), 0) as actual_cost
			FROM usage_statistics_range($1, $2) WHERE TRUE` + filter + ` GROUP BY endpoint ORDER BY requests DESC`
		mock.ExpectQuery("^" + regexp.QuoteMeta(query) + "$").WithArgs(args...).
			WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}).
				AddRow("/v1/responses", int64(1), int64(9), 1.2, 1.0))
	}
}

func TestUsageLogRepositoryGetUserSpendingRanking(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	rows := sqlmock.NewRows([]string{"user_id", "email", "username", "actual_cost", "requests", "tokens", "total_actual_cost", "total_requests", "total_tokens"}).
		AddRow(int64(2), "beta@example.com", "beta", 12.5, int64(9), int64(900), 40.0, int64(30), int64(2600)).
		AddRow(int64(1), "alpha@example.com", "alpha", 12.5, int64(8), int64(800), 40.0, int64(30), int64(2600)).
		AddRow(int64(3), "gamma@example.com", "", 4.25, int64(5), int64(300), 40.0, int64(30), int64(2600))

	mock.ExpectQuery("WITH user_spend AS \\(").
		WithArgs(start, end, 12).
		WillReturnRows(rows)

	got, err := repo.GetUserSpendingRanking(context.Background(), start, end, 12)
	require.NoError(t, err)
	require.Equal(t, &usagestats.UserSpendingRankingResponse{
		Ranking: []usagestats.UserSpendingRankingItem{
			{UserID: 2, Email: "beta@example.com", Username: "beta", ActualCost: 12.5, Requests: 9, Tokens: 900},
			{UserID: 1, Email: "alpha@example.com", Username: "alpha", ActualCost: 12.5, Requests: 8, Tokens: 800},
			{UserID: 3, Email: "gamma@example.com", ActualCost: 4.25, Requests: 5, Tokens: 300},
		},
		TotalActualCost: 40.0,
		TotalRequests:   30,
		TotalTokens:     2600,
	}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildRequestTypeFilterConditionLegacyFallback(t *testing.T) {
	tests := []struct {
		name      string
		request   int16
		wantWhere string
		wantArg   int16
	}{
		{
			name:      "sync_with_legacy_fallback",
			request:   int16(service.RequestTypeSync),
			wantWhere: "(request_type = $3 OR (request_type = 0 AND stream = FALSE AND openai_ws_mode = FALSE))",
			wantArg:   int16(service.RequestTypeSync),
		},
		{
			name:      "stream_with_legacy_fallback",
			request:   int16(service.RequestTypeStream),
			wantWhere: "(request_type = $3 OR (request_type = 0 AND stream = TRUE AND openai_ws_mode = FALSE))",
			wantArg:   int16(service.RequestTypeStream),
		},
		{
			name:      "ws_v2_with_legacy_fallback",
			request:   int16(service.RequestTypeWSV2),
			wantWhere: "(request_type = $3 OR (request_type = 0 AND openai_ws_mode = TRUE))",
			wantArg:   int16(service.RequestTypeWSV2),
		},
		{
			name:      "invalid_request_type_normalized_to_unknown",
			request:   int16(99),
			wantWhere: "request_type = $3",
			wantArg:   int16(service.RequestTypeUnknown),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := buildRequestTypeFilterCondition(3, tt.request)
			require.Equal(t, tt.wantWhere, where)
			require.Equal(t, []any{tt.wantArg}, args)
		})
	}
}

type usageLogScannerStub struct {
	values []any
}

func (s usageLogScannerStub) Scan(dest ...any) error {
	if len(dest) != len(s.values) {
		return fmt.Errorf("scan arg count mismatch: got %d want %d", len(dest), len(s.values))
	}
	for i := range dest {
		dv := reflect.ValueOf(dest[i])
		if dv.Kind() != reflect.Pointer {
			return fmt.Errorf("dest[%d] is not pointer", i)
		}
		dv.Elem().Set(reflect.ValueOf(s.values[i]))
	}
	return nil
}

func TestAppendNativeCompactionV2WhereCondition(t *testing.T) {
	enabled := true
	conditions, args := appendNativeCompactionV2WhereCondition(nil, nil, &enabled, "ul")
	require.Equal(t, []string{"ul.native_compaction_v2 = $1"}, conditions)
	require.Equal(t, []any{true}, args)

	conditions, args = appendNativeCompactionV2WhereCondition(conditions, args, nil, "ul")
	require.Len(t, conditions, 1)
	require.Len(t, args, 1)
}

func TestScanUsageLogRequestTypeAndLegacyFallback(t *testing.T) {
	t.Run("image_size_metadata_is_scanned", func(t *testing.T) {
		now := time.Now().UTC()
		log, err := scanUsageLog(usageLogScannerStub{values: []any{
			int64(4),
			int64(13),
			int64(23),
			int64(33),
			sql.NullString{Valid: true, String: "req-image-metadata"},
			"gpt-image-2",
			sql.NullString{Valid: true, String: "gpt-image-2"},
			sql.NullString{},
			sql.NullString{},
			sql.NullBool{},
			sql.NullInt64{},
			sql.NullInt64{},
			0, 0, 0, 0, 0, 0,
			0, 0.0, // image_output_tokens, image_output_cost
			0, 0.0, // image_input_tokens, image_input_cost
			0.0, 0.0, 0.0, 0.0, 0.8, 0.8,
			1.0,
			sql.NullFloat64{},
			int16(service.BillingTypeBalance),
			int16(service.RequestTypeSync),
			false,
			false,
			sql.NullInt64{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			2,
			sql.NullString{Valid: true, String: "4K"},
			sql.NullString{Valid: true, String: "1024x1024"},
			sql.NullString{Valid: true, String: "3840x2160"},
			sql.NullString{Valid: true, String: "output"},
			sql.NullString{Valid: true, String: `{"4K":2}`},
			0,                // video_count
			sql.NullString{}, // video_resolution
			sql.NullInt64{},  // video_duration_seconds
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			false,
			false,
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			sql.NullString{},
			sql.NullFloat64{}, // account_stats_cost
			sql.NullString{},  // upstream_request_id
			sql.NullString{},  // session_id
			false,             // native_compaction_v2
			now,
		}})
		require.NoError(t, err)
		require.Equal(t, 2, log.ImageCount)
		require.NotNil(t, log.ImageSize)
		require.Equal(t, "4K", *log.ImageSize)
		require.NotNil(t, log.ImageInputSize)
		require.Equal(t, "1024x1024", *log.ImageInputSize)
		require.NotNil(t, log.ImageOutputSize)
		require.Equal(t, "3840x2160", *log.ImageOutputSize)
		require.NotNil(t, log.ImageSizeSource)
		require.Equal(t, "output", *log.ImageSizeSource)
		require.Equal(t, map[string]int{"4K": 2}, log.ImageSizeBreakdown)
	})

	t.Run("request_type_ws_v2_overrides_legacy", func(t *testing.T) {
		now := time.Now().UTC()
		log, err := scanUsageLog(usageLogScannerStub{values: []any{
			int64(1),  // id
			int64(10), // user_id
			int64(20), // api_key_id
			int64(30), // account_id
			sql.NullString{Valid: true, String: "req-1"},
			"gpt-5", // model
			sql.NullString{Valid: true, String: "gpt-5"}, // requested_model
			sql.NullString{},  // upstream_model
			sql.NullString{},  // upstream_response_model
			sql.NullBool{},    // upstream_model_mismatch
			sql.NullInt64{},   // group_id
			sql.NullInt64{},   // subscription_id
			1,                 // input_tokens
			2,                 // output_tokens
			3,                 // cache_creation_tokens
			4,                 // cache_read_tokens
			5,                 // cache_creation_5m_tokens
			6,                 // cache_creation_1h_tokens
			0,                 // image_output_tokens
			0.0,               // image_output_cost
			0,                 // image_input_tokens
			0.0,               // image_input_cost
			0.1,               // input_cost
			0.2,               // output_cost
			0.3,               // cache_creation_cost
			0.4,               // cache_read_cost
			1.0,               // total_cost
			0.9,               // actual_cost
			1.0,               // rate_multiplier
			sql.NullFloat64{}, // account_rate_multiplier
			int16(service.BillingTypeBalance),
			int16(service.RequestTypeWSV2),
			false, // legacy stream
			false, // legacy openai ws
			sql.NullInt64{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			0,
			sql.NullString{},
			sql.NullString{}, // image_input_size
			sql.NullString{}, // image_output_size
			sql.NullString{}, // image_size_source
			sql.NullString{}, // image_size_breakdown
			0,                // video_count
			sql.NullString{}, // video_resolution
			sql.NullInt64{},  // video_duration_seconds
			sql.NullString{Valid: true, String: "priority"},
			sql.NullString{}, // reasoning_effort
			sql.NullString{}, // requested_reasoning_effort
			sql.NullString{}, // inbound_endpoint
			sql.NullString{}, // upstream_endpoint
			false,
			false,
			sql.NullInt64{},   // channel_id
			sql.NullString{},  // model_mapping_chain
			sql.NullString{},  // billing_tier
			sql.NullString{},  // billing_mode
			sql.NullFloat64{}, // account_stats_cost
			sql.NullString{},  // upstream_request_id
			sql.NullString{},  // session_id
			false,             // native_compaction_v2
			now,
		}})
		require.NoError(t, err)
		require.NotNil(t, log.ServiceTier)
		require.Equal(t, "priority", *log.ServiceTier)
		require.Equal(t, service.RequestTypeWSV2, log.RequestType)
		require.True(t, log.Stream)
		require.True(t, log.OpenAIWSMode)
	})

	t.Run("request_type_unknown_falls_back_to_legacy", func(t *testing.T) {
		now := time.Now().UTC()
		log, err := scanUsageLog(usageLogScannerStub{values: []any{
			int64(2),
			int64(11),
			int64(21),
			int64(31),
			sql.NullString{Valid: true, String: "req-2"},
			"gpt-5",
			sql.NullString{Valid: true, String: "gpt-5"},
			sql.NullString{},
			sql.NullString{},
			sql.NullBool{},
			sql.NullInt64{},
			sql.NullInt64{},
			1, 2, 3, 4, 5, 6,
			0, 0.0, // image_output_tokens, image_output_cost
			0, 0.0, // image_input_tokens, image_input_cost
			0.1, 0.2, 0.3, 0.4, 1.0, 0.9,
			1.0,
			sql.NullFloat64{},
			int16(service.BillingTypeBalance),
			int16(service.RequestTypeUnknown),
			true,
			false,
			sql.NullInt64{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			0,
			sql.NullString{},
			sql.NullString{}, // image_input_size
			sql.NullString{}, // image_output_size
			sql.NullString{}, // image_size_source
			sql.NullString{}, // image_size_breakdown
			0,                // video_count
			sql.NullString{}, // video_resolution
			sql.NullInt64{},  // video_duration_seconds
			sql.NullString{Valid: true, String: "flex"},
			sql.NullString{}, // reasoning_effort
			sql.NullString{}, // requested_reasoning_effort
			sql.NullString{}, // inbound_endpoint
			sql.NullString{}, // upstream_endpoint
			false,
			false,
			sql.NullInt64{},   // channel_id
			sql.NullString{},  // model_mapping_chain
			sql.NullString{},  // billing_tier
			sql.NullString{},  // billing_mode
			sql.NullFloat64{}, // account_stats_cost
			sql.NullString{},  // upstream_request_id
			sql.NullString{},  // session_id
			false,             // native_compaction_v2
			now,
		}})
		require.NoError(t, err)
		require.NotNil(t, log.ServiceTier)
		require.Equal(t, "flex", *log.ServiceTier)
		require.Equal(t, service.RequestTypeStream, log.RequestType)
		require.True(t, log.Stream)
		require.False(t, log.OpenAIWSMode)
	})

	t.Run("service_tier_is_scanned", func(t *testing.T) {
		now := time.Now().UTC()
		log, err := scanUsageLog(usageLogScannerStub{values: []any{
			int64(3),
			int64(12),
			int64(22),
			int64(32),
			sql.NullString{Valid: true, String: "req-3"},
			"gpt-5.4",
			sql.NullString{Valid: true, String: "gpt-5.4"},
			sql.NullString{},
			sql.NullString{},
			sql.NullBool{},
			sql.NullInt64{},
			sql.NullInt64{},
			1, 2, 3, 4, 5, 6,
			0, 0.0, // image_output_tokens, image_output_cost
			0, 0.0, // image_input_tokens, image_input_cost
			0.1, 0.2, 0.3, 0.4, 1.0, 0.9,
			1.0,
			sql.NullFloat64{},
			int16(service.BillingTypeBalance),
			int16(service.RequestTypeSync),
			false,
			false,
			sql.NullInt64{},
			sql.NullInt64{},
			sql.NullString{},
			sql.NullString{},
			0,
			sql.NullString{},
			sql.NullString{}, // image_input_size
			sql.NullString{}, // image_output_size
			sql.NullString{}, // image_size_source
			sql.NullString{}, // image_size_breakdown
			0,                // video_count
			sql.NullString{}, // video_resolution
			sql.NullInt64{},  // video_duration_seconds
			sql.NullString{Valid: true, String: "priority"},
			sql.NullString{}, // reasoning_effort
			sql.NullString{Valid: true, String: "max"}, // requested_reasoning_effort
			sql.NullString{}, // inbound_endpoint
			sql.NullString{}, // upstream_endpoint
			false,
			false,
			sql.NullInt64{},   // channel_id
			sql.NullString{},  // model_mapping_chain
			sql.NullString{},  // billing_tier
			sql.NullString{},  // billing_mode
			sql.NullFloat64{}, // account_stats_cost
			sql.NullString{},  // upstream_request_id
			sql.NullString{},  // session_id
			true,              // native_compaction_v2
			now,
		}})
		require.NoError(t, err)
		require.NotNil(t, log.ServiceTier)
		require.Equal(t, "priority", *log.ServiceTier)
		require.NotNil(t, log.RequestedReasoningEffort)
		require.Equal(t, "max", *log.RequestedReasoningEffort)
		require.True(t, log.NativeCompactionV2)
	})

}
