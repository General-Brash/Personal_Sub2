package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

type usageHistoryMetadataRepository interface {
	GetUsageHistoryMetadata(context.Context, time.Time, time.Time, int64) (usagestats.HistoryMetadata, error)
}

func (s *UsageService) GetUsageHistoryMetadata(ctx context.Context, start, end time.Time, userID int64) (usagestats.HistoryMetadata, error) {
	if r, ok := s.usageRepo.(usageHistoryMetadataRepository); ok {
		return r.GetUsageHistoryMetadata(ctx, start, end, userID)
	}
	return usagestats.HistoryMetadata{}, errors.New("usage history metadata is unavailable")
}

func (s *AccountUsageService) GetAccountUsageHistoryMetadata(ctx context.Context, start, end time.Time, accountID int64) (usagestats.HistoryMetadata, error) {
	if r, ok := s.usageLogRepo.(interface {
		GetAccountUsageHistoryMetadata(context.Context, time.Time, time.Time, int64) (usagestats.HistoryMetadata, error)
	}); ok {
		return r.GetAccountUsageHistoryMetadata(ctx, start, end, accountID)
	}
	return usagestats.HistoryMetadata{}, errors.New("account usage history metadata is unavailable")
}

func (s *UsageService) GetAPIKeyUsageHistoryMetadata(ctx context.Context, start, end time.Time, userID, apiKeyID int64) (usagestats.HistoryMetadata, error) {
	if r, ok := s.usageRepo.(interface {
		GetAPIKeyUsageHistoryMetadata(context.Context, time.Time, time.Time, int64, int64) (usagestats.HistoryMetadata, error)
	}); ok {
		return r.GetAPIKeyUsageHistoryMetadata(ctx, start, end, userID, apiKeyID)
	}
	return usagestats.HistoryMetadata{}, errors.New("api key usage history metadata is unavailable")
}

func (s *DashboardAggregationService) GetUsageHistoryMetadata(ctx context.Context, start, end time.Time) (usagestats.HistoryMetadata, error) {
	if s != nil {
		if r, ok := s.repo.(usageHistoryMetadataRepository); ok {
			return r.GetUsageHistoryMetadata(ctx, start, end, 0)
		}
	}
	return usagestats.HistoryMetadata{}, errors.New("dashboard usage history metadata is unavailable")
}

func (s *OpsService) GetDashboardHistoryMetadata(ctx context.Context, filter *OpsDashboardFilter) (usagestats.HistoryMetadata, error) {
	if r, ok := s.opsRepo.(interface {
		GetDashboardHistoryMetadata(context.Context, *OpsDashboardFilter) (usagestats.HistoryMetadata, error)
	}); ok {
		return r.GetDashboardHistoryMetadata(ctx, filter)
	}
	return usagestats.HistoryMetadata{}, nil
}
