package service

import (
	"context"
	"time"
)

// AvailableCreditCache keeps the temporary-plus-permanent precheck value
// separate from the existing permanent balance cache.
type AvailableCreditCache interface {
	GetAvailableCredit(ctx context.Context, userID int64) (float64, error)
	SetAvailableCredit(ctx context.Context, userID int64, amount float64, ttl time.Duration) error
	InvalidateAvailableCredit(ctx context.Context, userID int64) error
}

// AvailableCreditInvalidator is the narrow post-commit hook used by credit
// grants and billing transactions. Implementations must not make a committed
// business operation fail when cache invalidation is unavailable.
type AvailableCreditInvalidator interface {
	InvalidateAvailableCredit(ctx context.Context, userID int64) error
}

// AvailableCreditSnapshot is the database-backed source used to rebuild and
// revalidate the available-credit cache.
type AvailableCreditSnapshot struct {
	PermanentBalance              float64
	TemporaryCredit               float64
	EarliestTemporaryCreditExpiry *time.Time
}

func (s AvailableCreditSnapshot) Total() float64 {
	total := s.PermanentBalance + s.TemporaryCredit
	normalized, err := normalizeDerivedLedgerAmount(total)
	if err != nil {
		// Preserve non-finite values so eligibility checks fail closed.
		return total
	}
	return normalized
}

// AvailableCreditSnapshotReader is intentionally narrower than
// UserRepository so existing repository fakes do not need a new method.
type AvailableCreditSnapshotReader interface {
	GetAvailableCreditSnapshot(ctx context.Context, userID int64) (AvailableCreditSnapshot, error)
}

// SubscriptionCacheData represents cached subscription data
type SubscriptionCacheData struct {
	Status       string
	ExpiresAt    time.Time
	DailyUsage   float64
	WeeklyUsage  float64
	MonthlyUsage float64
	Version      int64
}
