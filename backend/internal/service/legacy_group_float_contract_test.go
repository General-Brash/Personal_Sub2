package service

import (
	"testing"
	"time"
)

func TestLegacyGroupAmountContractsRemainFloat64(t *testing.T) {
	amount := 1.25
	group := Group{
		RateMultiplier:               amount,
		PeakRateMultiplier:           amount,
		DailyLimitUSD:                &amount,
		WeeklyLimitUSD:               &amount,
		MonthlyLimitUSD:              &amount,
		ImageRateMultiplier:          amount,
		BatchImageDiscountMultiplier: amount,
		BatchImageHoldMultiplier:     amount,
		VideoRateMultiplier:          amount,
		ImagePrice1K:                 &amount,
		ImagePrice2K:                 &amount,
		ImagePrice4K:                 &amount,
		VideoPrice480P:               &amount,
		VideoPrice720P:               &amount,
		VideoPrice1080P:              &amount,
		WebSearchPricePerCall:        &amount,
		SubscriptionType:             SubscriptionTypeSubscription,
		PeakRateEnabled:              true,
		PeakStart:                    "09:00",
		PeakEnd:                      "10:00",
	}
	create := CreateGroupInput{
		RateMultiplier:               amount,
		DailyLimitUSD:                &amount,
		ImageRateMultiplier:          &amount,
		BatchImageDiscountMultiplier: &amount,
		BatchImageHoldMultiplier:     &amount,
		VideoRateMultiplier:          &amount,
		PeakRateMultiplier:           &amount,
		ImagePrice1K:                 &amount,
		VideoPrice480P:               &amount,
		WebSearchPricePerCall:        &amount,
	}
	update := UpdateGroupInput{
		RateMultiplier:               &amount,
		DailyLimitUSD:                &amount,
		ImageRateMultiplier:          &amount,
		BatchImageDiscountMultiplier: &amount,
		BatchImageHoldMultiplier:     &amount,
		VideoRateMultiplier:          &amount,
		PeakRateMultiplier:           &amount,
		ImagePrice1K:                 &amount,
		VideoPrice480P:               &amount,
		WebSearchPricePerCall:        &amount,
	}

	if got := group.PeakMultiplierAt(time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)); got != amount {
		t.Fatalf("peak multiplier = %v, want %v", got, amount)
	}
	if err := ValidatePeakRateConfig(SubscriptionTypeSubscription, true, "09:00", "10:00", amount); err != nil {
		t.Fatalf("float64 peak rate configuration should be accepted: %v", err)
	}
	if create.RateMultiplier != amount || *update.RateMultiplier != amount {
		t.Fatal("legacy group input values changed")
	}
	availableGroup := AvailableGroupRef{
		RateMultiplier:     amount,
		PeakRateMultiplier: amount,
	}
	if availableGroup.RateMultiplier != amount || availableGroup.PeakRateMultiplier != amount {
		t.Fatal("available group rate multipliers changed")
	}

	var _ func(*Group, time.Time) float64 = (*Group).PeakMultiplierAt
	var _ func(string, bool, string, string, float64) error = ValidatePeakRateConfig
	var _ func(string, bool, string, string, float64) (bool, string, string, float64) = NormalizePeakRateConfig
	var _ func(*APIKey, float64, time.Time) (float64, float64) = computePeakAwareMultipliers
	var _ float64 = availableGroup.RateMultiplier
	var _ float64 = availableGroup.PeakRateMultiplier
}
