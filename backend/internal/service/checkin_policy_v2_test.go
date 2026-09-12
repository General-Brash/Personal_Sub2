package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCheckinPolicyV2_ParameterBoundaries(t *testing.T) {
	valid := DefaultDailyCheckinPolicyV2()
	require.NoError(t, valid.Validate())
	require.Equal(t, 500, valid.AutoFeeBps)

	tests := []struct {
		name   string
		mutate func(*DailyCheckinPolicyV2)
	}{
		{name: "refresh hour", mutate: func(p *DailyCheckinPolicyV2) { p.RefreshTime = "24:00" }},
		{name: "refresh minute", mutate: func(p *DailyCheckinPolicyV2) { p.RefreshTime = "23:60" }},
		{name: "fee negative", mutate: func(p *DailyCheckinPolicyV2) { p.AutoFeeBps = -1 }},
		{name: "fee over 100 percent", mutate: func(p *DailyCheckinPolicyV2) { p.AutoFeeBps = 10001 }},
		{name: "normal zero", mutate: func(p *DailyCheckinPolicyV2) { p.Normal.MinBps = 0 }},
		{name: "normal reversed", mutate: func(p *DailyCheckinPolicyV2) { p.Normal.MinBps = 12000; p.Normal.MaxBps = 11000 }},
		{name: "normal too large", mutate: func(p *DailyCheckinPolicyV2) { p.Normal.MaxBps = checkinMaxMultiplierBps + 1 }},
		{name: "super enabled without cost", mutate: func(p *DailyCheckinPolicyV2) { p.Super.Enabled = true; p.Super.Cost = 0 }},
		{name: "super cost too precise", mutate: func(p *DailyCheckinPolicyV2) { p.Super.Cost = 0.000000001 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := DefaultDailyCheckinPolicyV2()
			tt.mutate(&policy)
			require.ErrorIs(t, policy.Validate(), ErrDailyCheckinPolicyInvalid)
		})
	}
}

func TestCheckinPeriodAt_UsesBeijingCalendarBoundary(t *testing.T) {
	policy := DefaultDailyCheckinPolicyV2()
	before := time.Date(2026, time.September, 11, 15, 59, 59, 0, time.UTC)
	period, err := checkinPeriodAt(policy, before)
	require.NoError(t, err)
	require.Equal(t, "2026-09-11", period.Date)
	require.Equal(t, "2026-09-12T00:00:00+08:00", period.NextReset.Format(time.RFC3339))

	at := time.Date(2026, time.September, 11, 16, 0, 0, 0, time.UTC)
	period, err = checkinPeriodAt(policy, at)
	require.NoError(t, err)
	require.Equal(t, "2026-09-12", period.Date)
}

func TestCheckinPeriodAt_RefreshTransitionIsNonOverlapping(t *testing.T) {
	policy := DefaultDailyCheckinPolicyV2()
	effective := time.Date(2026, time.September, 12, 0, 0, 0, 0, beijingLocation)
	policy.RefreshTime = "00:00"
	policy.PendingRefresh = &DailyCheckinPendingV2{RefreshTime: "06:00", EffectiveAt: effective}
	period, err := checkinPeriodAt(policy, effective)
	require.NoError(t, err)
	require.Equal(t, effective.UTC(), period.StartAt.UTC())
	require.Equal(t, "2026-09-12T06:00:00+08:00", period.NextReset.Format(time.RFC3339))
}

func TestCheckinV2_Floor8AndRandomBoundary(t *testing.T) {
	require.Equal(t, 3.80000000, floor8(4-floor8(4*500.0/10000)))
	require.Equal(t, 0.12345678, floor8(0.123456789))
	value, err := secureCheckinMultiplier(nil, 8000, 8000)
	require.NoError(t, err)
	require.Equal(t, 8000, value)
}

func TestCheckinPeriodAt_PendingClockDoesNotChangeActivePeriod(t *testing.T) {
	policy := DefaultDailyCheckinPolicyV2()
	effective := time.Date(2026, time.September, 12, 0, 0, 0, 0, beijingLocation)
	policy.PendingRefresh = &DailyCheckinPendingV2{RefreshTime: "06:00", EffectiveAt: effective}
	before, err := checkinPeriodAt(policy, effective.Add(-time.Minute))
	require.NoError(t, err)
	require.Equal(t, effective.AddDate(0, 0, -1), before.StartAt)
	require.Equal(t, effective, before.NextReset)
	transition, err := checkinPeriodAt(policy, effective)
	require.NoError(t, err)
	require.Equal(t, before.NextReset, transition.StartAt)
	after, err := checkinPeriodAt(policy, effective.Add(6*time.Hour))
	require.NoError(t, err)
	require.Equal(t, transition.NextReset, after.StartAt)
	require.NotEqual(t, transition.ID, after.ID)
}

func TestCheckinBasisPointAmount_UsesDecimalBeforeQuantization(t *testing.T) {
	require.Equal(t, 0.2, checkinBasisPointAmount(4, 500))
	require.Equal(t, 0.00000001, checkinBasisPointAmount(0.00000001, 10000))
	require.Equal(t, 0.0, checkinBasisPointAmount(0.00000001, 9999))
	require.Equal(t, 0.104, checkinBasisPointAmount(0.13, 8000))
	require.Equal(t, 4.0, checkinBasisPointAmount(4, 10000))
}

func TestCheckinPolicyVersionIncludesBaseRewardsAndRandomCost(t *testing.T) {
	base := DefaultDailyCheckinPolicy()
	extended := DefaultDailyCheckinPolicyV2()
	first := EffectiveCheckinPolicyVersion(&base, extended)
	base.RewardTiers[0].Amount = 2
	second := EffectiveCheckinPolicyVersion(&base, extended)
	require.NotEqual(t, first, second)
	extended.Super.Cost = 1.25
	third := EffectiveCheckinPolicyVersion(&base, extended)
	require.NotEqual(t, second, third)
}

func TestNextCheckinStreakFromPeriodKeepsTransitionStreak(t *testing.T) {
	currentStart := time.Date(2026, time.September, 12, 0, 0, 0, 0, beijingLocation)
	previousEnd := currentStart
	require.Equal(t, 8, NextCheckinStreakFromPeriod(&previousEnd, 7, currentStart))
	olderEnd := currentStart.Add(-48 * time.Hour)
	require.Equal(t, 1, NextCheckinStreakFromPeriod(&olderEnd, 7, currentStart))
}
