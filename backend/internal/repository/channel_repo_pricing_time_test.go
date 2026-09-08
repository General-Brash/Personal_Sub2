//go:build unit

package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

var channelModelPricingTimePricingColumns = []string{
	"id", "channel_id", "platform", "models", "billing_mode", "input_price", "output_price",
	"cache_write_price", "cache_write_1h_price", "cache_read_price", "fast_multiplier", "flex_multiplier",
	"max_reasoning_effort_multiplier", "image_input_price", "image_output_price", "per_request_price",
	"time_pricing", "created_at", "updated_at",
}

var channelPricingIntervalColumns = []string{
	"id", "pricing_id", "min_tokens", "max_tokens", "tier_label", "input_price", "output_price",
	"cache_write_price", "cache_write_1h_price", "cache_read_price", "input_multiplier", "output_multiplier",
	"cache_write_multiplier", "cache_read_multiplier", "per_request_price", "sort_order", "created_at", "updated_at",
}

const channelModelPricingTimePricingJSON = `{"timezone":"Asia/Shanghai","periods":[{"start_time":"09:00","end_time":"12:00","multiplier":2}]}`

func newChannelModelPricingTimePricingRepo(t *testing.T) (*channelRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &channelRepository{db: db}, mock
}

func channelPricingFloat64(value float64) *float64 {
	return &value
}

func channelPricingInt(value int) *int {
	return &value
}

func modelPricingTimePricingRow(timePricing any) *sqlmock.Rows {
	return sqlmock.NewRows(channelModelPricingTimePricingColumns).AddRow(
		int64(11), int64(7), "openai", `["gpt-5"]`, service.BillingModeToken,
		0.000001, 0.000002, 0.000003, 0.000004, 0.000005, 1.5, 0.8, 2.4, 0.000006, 0.000007, 0.01,
		timePricing,
		time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
	)
}

func modelPricingNullRow(timePricing any) *sqlmock.Rows {
	return sqlmock.NewRows(channelModelPricingTimePricingColumns).AddRow(
		int64(11), int64(7), "openai", `["gpt-5"]`, service.BillingModeToken,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		timePricing,
		time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
	)
}

func expectModelPricingList(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT " + strings.Join(channelModelPricingTimePricingColumns, ", ") +
			" FROM channel_model_pricing WHERE channel_id = $1 ORDER BY id",
	)).WithArgs(int64(7)).WillReturnRows(rows)
}

func expectEmptyModelPricingIntervals(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT " + strings.Join(channelPricingIntervalColumns, ", ") +
			" FROM channel_pricing_intervals WHERE pricing_id = ANY($1) ORDER BY pricing_id, sort_order, id",
	)).WithArgs(pq.Array([]int64{11})).WillReturnRows(sqlmock.NewRows(channelPricingIntervalColumns))
}

func requireChannelPricingFloat64(t *testing.T, want float64, got *float64) {
	t.Helper()
	require.NotNil(t, got)
	require.InDelta(t, want, *got, 1e-12)
}

func TestChannelModelPricingTimePricingListRoundTrip(t *testing.T) {
	repo, mock := newChannelModelPricingTimePricingRepo(t)
	expectModelPricingList(mock, modelPricingTimePricingRow(channelModelPricingTimePricingJSON))
	expectEmptyModelPricingIntervals(mock)

	pricing, err := repo.ListModelPricing(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, pricing, 1)
	require.Equal(t, []string{"gpt-5"}, pricing[0].Models)
	require.Equal(t, service.BillingModeToken, pricing[0].BillingMode)
	requireChannelPricingFloat64(t, 0.000004, pricing[0].CacheWrite1hPrice)
	requireChannelPricingFloat64(t, 1.5, pricing[0].FastMultiplier)
	requireChannelPricingFloat64(t, 0.8, pricing[0].FlexMultiplier)
	requireChannelPricingFloat64(t, 2.4, pricing[0].MaxReasoningEffortMultiplier)
	require.NotNil(t, pricing[0].TimePricing)
	require.Equal(t, "Asia/Shanghai", pricing[0].TimePricing.Timezone)
	require.Len(t, pricing[0].TimePricing.Periods, 1)
	require.Equal(t, 2.0, pricing[0].TimePricing.Periods[0].Multiplier)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelModelPricingTimePricingListNullAndMalformed(t *testing.T) {
	t.Run("SQL NULL maps to nil", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		expectModelPricingList(mock, modelPricingNullRow(nil))
		expectEmptyModelPricingIntervals(mock)

		pricing, err := repo.ListModelPricing(context.Background(), 7)
		require.NoError(t, err)
		require.Len(t, pricing, 1)
		require.Nil(t, pricing[0].CacheWrite1hPrice)
		require.Nil(t, pricing[0].FastMultiplier)
		require.Nil(t, pricing[0].FlexMultiplier)
		require.Nil(t, pricing[0].MaxReasoningEffortMultiplier)
		require.Nil(t, pricing[0].TimePricing)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("malformed JSON returns repository error", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		expectModelPricingList(mock, modelPricingTimePricingRow(`{"timezone":`))

		_, err := repo.ListModelPricing(context.Background(), 7)
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "unmarshal time pricing"), "unexpected error: %v", err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestChannelModelPricingTimePricingCreateAndUpdateRoundTrip(t *testing.T) {
	pricing := &service.ChannelModelPricing{
		ID:                           11,
		ChannelID:                    7,
		Platform:                     "openai",
		Models:                       []string{"gpt-5"},
		InputPrice:                   channelPricingFloat64(0.000001),
		OutputPrice:                  channelPricingFloat64(0.000002),
		CacheWritePrice:              channelPricingFloat64(0.000003),
		CacheWrite1hPrice:            channelPricingFloat64(0.000004),
		CacheReadPrice:               channelPricingFloat64(0.000005),
		FastMultiplier:               channelPricingFloat64(1.5),
		FlexMultiplier:               channelPricingFloat64(0.8),
		MaxReasoningEffortMultiplier: channelPricingFloat64(2.4),
		ImageInputPrice:              channelPricingFloat64(0.000006),
		ImageOutputPrice:             channelPricingFloat64(0.000007),
		PerRequestPrice:              channelPricingFloat64(0.01),
		TimePricing: &service.ChannelTimePricing{
			Timezone: "Asia/Shanghai",
			Periods: []service.ChannelTimePricingPeriod{{
				StartTime: "09:00", EndTime: "12:00", Multiplier: 2,
			}},
		},
	}

	t.Run("create writes JSON and extended pricing fields in contract order", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		mock.ExpectQuery(regexp.QuoteMeta(
			"INSERT INTO channel_model_pricing (channel_id, platform, models, billing_mode, input_price, output_price, cache_write_price, cache_write_1h_price, cache_read_price, fast_multiplier, flex_multiplier, max_reasoning_effort_multiplier, image_input_price, image_output_price, per_request_price, time_pricing) "+
				"VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING id, created_at, updated_at",
		)).WithArgs(
			int64(7), "openai", []byte(`["gpt-5"]`), service.BillingModeToken,
			0.000001, 0.000002, 0.000003, 0.000004, 0.000005, 1.5, 0.8, 2.4, 0.000006, 0.000007, 0.01,
			channelModelPricingTimePricingJSON,
		).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(
			int64(11), time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
		))

		require.NoError(t, repo.CreateModelPricing(context.Background(), pricing))
		require.Equal(t, int64(11), pricing.ID)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("update writes JSON and extended pricing fields in contract order", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		mock.ExpectExec(regexp.QuoteMeta(
			"UPDATE channel_model_pricing SET models = $1, billing_mode = $2, input_price = $3, output_price = $4, cache_write_price = $5, cache_write_1h_price = $6, cache_read_price = $7, fast_multiplier = $8, flex_multiplier = $9, max_reasoning_effort_multiplier = $10, image_input_price = $11, image_output_price = $12, per_request_price = $13, time_pricing = $14, platform = $15, updated_at = NOW() WHERE id = $16",
		)).WithArgs(
			[]byte(`["gpt-5"]`), service.BillingModeToken,
			0.000001, 0.000002, 0.000003, 0.000004, 0.000005, 1.5, 0.8, 2.4, 0.000006, 0.000007, 0.01,
			channelModelPricingTimePricingJSON, "openai", int64(11),
		).WillReturnResult(sqlmock.NewResult(0, 1))

		require.NoError(t, repo.UpdateModelPricing(context.Background(), pricing))
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestChannelModelPricingTimePricingCreateAndUpdateWriteNullWhenDisabled(t *testing.T) {
	tests := []struct {
		name        string
		timePricing *service.ChannelTimePricing
	}{
		{name: "nil", timePricing: nil},
		{name: "empty periods", timePricing: &service.ChannelTimePricing{Timezone: "Asia/Shanghai"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newPricing := func() *service.ChannelModelPricing {
				return &service.ChannelModelPricing{
					ID:          11,
					ChannelID:   7,
					Platform:    "openai",
					Models:      []string{"gpt-5"},
					TimePricing: tt.timePricing,
				}
			}

			t.Run("create writes SQL NULL", func(t *testing.T) {
				repo, mock := newChannelModelPricingTimePricingRepo(t)
				mock.ExpectQuery(regexp.QuoteMeta(
					"INSERT INTO channel_model_pricing (channel_id, platform, models, billing_mode, input_price, output_price, cache_write_price, cache_write_1h_price, cache_read_price, fast_multiplier, flex_multiplier, max_reasoning_effort_multiplier, image_input_price, image_output_price, per_request_price, time_pricing) "+
						"VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING id, created_at, updated_at",
				)).WithArgs(
					int64(7), "openai", []byte(`["gpt-5"]`), service.BillingModeToken,
					nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
				).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(11), time.Time{}, time.Time{}))

				require.NoError(t, repo.CreateModelPricing(context.Background(), newPricing()))
				require.NoError(t, mock.ExpectationsWereMet())
			})

			t.Run("update writes SQL NULL", func(t *testing.T) {
				repo, mock := newChannelModelPricingTimePricingRepo(t)
				mock.ExpectExec(regexp.QuoteMeta(
					"UPDATE channel_model_pricing SET models = $1, billing_mode = $2, input_price = $3, output_price = $4, cache_write_price = $5, cache_write_1h_price = $6, cache_read_price = $7, fast_multiplier = $8, flex_multiplier = $9, max_reasoning_effort_multiplier = $10, image_input_price = $11, image_output_price = $12, per_request_price = $13, time_pricing = $14, platform = $15, updated_at = NOW() WHERE id = $16",
				)).WithArgs(
					[]byte(`["gpt-5"]`), service.BillingModeToken,
					nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
					"openai", int64(11),
				).WillReturnResult(sqlmock.NewResult(0, 1))

				require.NoError(t, repo.UpdateModelPricing(context.Background(), newPricing()))
				require.NoError(t, mock.ExpectationsWereMet())
			})
		})
	}
}

func TestChannelModelPricingIntervalsExtendedFieldsRoundTrip(t *testing.T) {
	t.Run("list scans cache1h and multiplier columns", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		expectModelPricingList(mock, modelPricingNullRow(nil))
		mock.ExpectQuery(regexp.QuoteMeta(
			"SELECT " + strings.Join(channelPricingIntervalColumns, ", ") +
				" FROM channel_pricing_intervals WHERE pricing_id = ANY($1) ORDER BY pricing_id, sort_order, id",
		)).WithArgs(pq.Array([]int64{11})).WillReturnRows(sqlmock.NewRows(channelPricingIntervalColumns).AddRow(
			int64(21), int64(11), 1000, 2000, "long-context", 0.001, 0.002, 0.003, 0.004, 0.005,
			1.1, 1.2, 1.3, 1.4, 0.01, 3,
			time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC),
		))

		pricing, err := repo.ListModelPricing(context.Background(), 7)
		require.NoError(t, err)
		require.Len(t, pricing, 1)
		require.Len(t, pricing[0].Intervals, 1)
		interval := pricing[0].Intervals[0]
		require.Equal(t, "long-context", interval.TierLabel)
		require.Equal(t, 1000, interval.MinTokens)
		require.NotNil(t, interval.MaxTokens)
		require.Equal(t, 2000, *interval.MaxTokens)
		requireChannelPricingFloat64(t, 0.004, interval.CacheWrite1hPrice)
		requireChannelPricingFloat64(t, 1.1, interval.InputMultiplier)
		requireChannelPricingFloat64(t, 1.2, interval.OutputMultiplier)
		requireChannelPricingFloat64(t, 1.3, interval.CacheWriteMultiplier)
		requireChannelPricingFloat64(t, 1.4, interval.CacheReadMultiplier)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("create writes interval columns in contract order", func(t *testing.T) {
		repo, mock := newChannelModelPricingTimePricingRepo(t)
		pricing := &service.ChannelModelPricing{
			ChannelID: 7,
			Platform:  "openai",
			Models:    []string{"gpt-5"},
			Intervals: []service.PricingInterval{{
				MinTokens:            1000,
				MaxTokens:            channelPricingInt(2000),
				TierLabel:            "long-context",
				InputPrice:           channelPricingFloat64(0.001),
				OutputPrice:          channelPricingFloat64(0.002),
				CacheWritePrice:      channelPricingFloat64(0.003),
				CacheWrite1hPrice:    channelPricingFloat64(0.004),
				CacheReadPrice:       channelPricingFloat64(0.005),
				InputMultiplier:      channelPricingFloat64(1.1),
				OutputMultiplier:     channelPricingFloat64(1.2),
				CacheWriteMultiplier: channelPricingFloat64(1.3),
				CacheReadMultiplier:  channelPricingFloat64(1.4),
				PerRequestPrice:      channelPricingFloat64(0.01),
				SortOrder:            3,
			}},
		}
		mock.ExpectQuery(regexp.QuoteMeta(
			"INSERT INTO channel_model_pricing (channel_id, platform, models, billing_mode, input_price, output_price, cache_write_price, cache_write_1h_price, cache_read_price, fast_multiplier, flex_multiplier, max_reasoning_effort_multiplier, image_input_price, image_output_price, per_request_price, time_pricing) "+
				"VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING id, created_at, updated_at",
		)).WithArgs(
			int64(7), "openai", []byte(`["gpt-5"]`), service.BillingModeToken,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(11), time.Time{}, time.Time{}))
		mock.ExpectQuery(regexp.QuoteMeta(
			"INSERT INTO channel_pricing_intervals (pricing_id, min_tokens, max_tokens, tier_label, input_price, output_price, cache_write_price, cache_write_1h_price, cache_read_price, input_multiplier, output_multiplier, cache_write_multiplier, cache_read_multiplier, per_request_price, sort_order) "+
				"VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING id, created_at, updated_at",
		)).WithArgs(
			int64(11), int64(1000), int64(2000), "long-context", 0.001, 0.002, 0.003, 0.004, 0.005,
			1.1, 1.2, 1.3, 1.4, 0.01, int64(3),
		).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(21), time.Time{}, time.Time{}))

		require.NoError(t, repo.CreateModelPricing(context.Background(), pricing))
		require.Equal(t, int64(11), pricing.ID)
		require.Equal(t, int64(11), pricing.Intervals[0].PricingID)
		require.Equal(t, int64(21), pricing.Intervals[0].ID)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
