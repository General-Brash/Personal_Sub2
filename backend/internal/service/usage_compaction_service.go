package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

type UsageCompactionResult struct {
	ProcessedRows int64
	DeletedRows   int64
	ProtectedRows int64
}

type UsageCompactionRepository interface {
	CompactUsageLogsBatch(context.Context, UsageCleanupFilters, int) (UsageCompactionResult, error)
}

// UsageCompactionEnd keeps the current natural month entirely in the raw store.
func UsageCompactionEnd(now time.Time) time.Time {
	return timezone.StartOfMonth(now).UTC()
}

func (s *UsageCleanupService) compactBatch(ctx context.Context, filters UsageCleanupFilters, limit int) (UsageCompactionResult, error) {
	if repo, ok := s.repo.(UsageCompactionRepository); ok {
		return repo.CompactUsageLogsBatch(ctx, filters, limit)
	}
	// Small test doubles retain the original repository contract.
	n, err := s.repo.DeleteUsageLogsBatch(ctx, filters, limit)
	return UsageCompactionResult{ProcessedRows: n, DeletedRows: n}, err
}
