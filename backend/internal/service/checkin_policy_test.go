package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestCheckinPolicy_Defaults(t *testing.T) {
	policy := DefaultDailyCheckinPolicy()

	require.False(t, policy.Enabled)
	require.Equal(t, 7, policy.MaxRewardDay)
	require.Len(t, policy.RewardTiers, 7)
	for day, tier := range policy.RewardTiers {
		require.Equal(t, day+1, tier.Day)
		require.Equal(t, 1.0, tier.Amount)
		require.Equal(t, 0.0, tier.PermanentAmount)
	}
}

func TestCheckinPolicy_BeijingBusinessDateAndNextMidnight(t *testing.T) {
	tests := []struct {
		name         string
		now          time.Time
		businessDate string
		nextMidnight string
	}{
		{
			name:         "23:59 Beijing time",
			now:          time.Date(2026, time.July, 13, 15, 59, 59, 0, time.UTC),
			businessDate: "2026-07-13",
			nextMidnight: "2026-07-14T00:00:00+08:00",
		},
		{
			name:         "00:00 Beijing time",
			now:          time.Date(2026, time.July, 13, 16, 0, 0, 0, time.UTC),
			businessDate: "2026-07-14",
			nextMidnight: "2026-07-15T00:00:00+08:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.businessDate, BeijingBusinessDate(tt.now))
			require.Equal(t, tt.nextMidnight, NextBeijingMidnight(tt.now).Format(time.RFC3339))
		})
	}
}

func TestCheckinPolicy_StreakAndRewardCap(t *testing.T) {
	day := time.Date(2026, time.July, 14, 0, 0, 0, 0, beijingLocation)
	require.Equal(t, 1, NextCheckinStreak(nil, 0, day))
	require.Equal(t, 8, NextCheckinStreak(ptrBusinessDate("2026-07-13"), 7, day))
	require.Equal(t, 1, NextCheckinStreak(ptrBusinessDate("2026-07-12"), 7, day))

	policy := DefaultDailyCheckinPolicy()
	rewardDay, amount, err := policy.RewardForStreak(8)
	require.NoError(t, err)
	require.Equal(t, 7, rewardDay)
	require.Equal(t, 1.0, amount)
	rewardDay, amount, permanentAmount, err := policy.RewardForStreakAmounts(8)
	require.NoError(t, err)
	require.Equal(t, 7, rewardDay)
	require.Equal(t, 1.0, amount)
	require.Equal(t, 0.0, permanentAmount)
}

func TestCheckinPolicy_AcceptsMaximumRewardDay(t *testing.T) {
	tiers := make([]DailyCheckinRewardTier, dailyCheckinMaxRewardDay)
	for index := range tiers {
		tiers[index] = DailyCheckinRewardTier{Day: index + 1, Amount: 1}
	}

	policy := DailyCheckinPolicy{
		MaxRewardDay: dailyCheckinMaxRewardDay,
		RewardTiers:  tiers,
	}

	require.NoError(t, policy.Validate())
}

func TestCheckinPolicy_RejectsInvalidPolicies(t *testing.T) {
	tests := []struct {
		name   string
		policy DailyCheckinPolicy
	}{
		{
			name: "max reward day must be positive",
			policy: DailyCheckinPolicy{
				MaxRewardDay: 0,
			},
		},
		{
			name: "max reward day cannot exceed limit",
			policy: DailyCheckinPolicy{
				MaxRewardDay: dailyCheckinMaxRewardDay + 1,
			},
		},
		{
			name: "tiers must match max reward day",
			policy: DailyCheckinPolicy{
				MaxRewardDay: 2,
				RewardTiers:  []DailyCheckinRewardTier{{Day: 1, Amount: 1}},
			},
		},
		{
			name: "tiers must cover every day in order",
			policy: DailyCheckinPolicy{
				MaxRewardDay: 2,
				RewardTiers: []DailyCheckinRewardTier{
					{Day: 1, Amount: 1},
					{Day: 3, Amount: 1},
				},
			},
		},
		{
			name: "reward amount must be positive",
			policy: DailyCheckinPolicy{
				MaxRewardDay: 1,
				RewardTiers:  []DailyCheckinRewardTier{{Day: 1, Amount: 0}},
			},
		},
		{
			name: "reward amount cannot be negative",
			policy: DailyCheckinPolicy{
				MaxRewardDay: 1,
				RewardTiers:  []DailyCheckinRewardTier{{Day: 1, Amount: -1}},
			},
		},
		{
			name: "reward amount cannot exceed eight decimal places",
			policy: DailyCheckinPolicy{
				MaxRewardDay: 1,
				RewardTiers:  []DailyCheckinRewardTier{{Day: 1, Amount: 0.000000001}},
			},
		},
		{
			name: "reward amount cannot exceed numeric max",
			policy: DailyCheckinPolicy{
				MaxRewardDay: 1,
				RewardTiers:  []DailyCheckinRewardTier{{Day: 1, Amount: maxLedgerAmount}},
			},
		},
		{
			name: "permanent reward amount cannot be negative",
			policy: DailyCheckinPolicy{
				MaxRewardDay: 1,
				RewardTiers:  []DailyCheckinRewardTier{{Day: 1, Amount: 1, PermanentAmount: -1}},
			},
		},
		{
			name: "permanent reward amount cannot exceed numeric max",
			policy: DailyCheckinPolicy{
				MaxRewardDay: 1,
				RewardTiers:  []DailyCheckinRewardTier{{Day: 1, Amount: 1, PermanentAmount: maxLedgerAmount}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			require.Error(t, err)
			require.Equal(t, "INVALID_DAILY_CHECKIN_POLICY", infraerrors.Reason(err))
		})
	}
}

func TestSettingService_UpdateSettings_DailyCheckinPolicyUsesSingleAtomicWrite(t *testing.T) {
	repo := &dailyCheckinPolicyRepoStub{}
	svc := NewSettingService(repo, &config.Config{})
	policy := DailyCheckinPolicy{
		Enabled:      true,
		MaxRewardDay: 2,
		RewardTiers: []DailyCheckinRewardTier{
			{Day: 1, Amount: 1.25},
			{Day: 2, Amount: 2.5},
		},
	}

	err := svc.UpdateDailyCheckinPolicy(context.Background(), &policy)
	require.NoError(t, err)
	require.Equal(t, 1, repo.setMultipleCalls)
	require.Equal(t, "true", repo.updates[SettingKeyDailyCheckinEnabled])
	require.Equal(t, "2", repo.updates[SettingKeyDailyCheckinMaxRewardDay])
	require.JSONEq(t, `[{"day":1,"amount":"1.25000000","permanent_amount":"0.00000000"},{"day":2,"amount":"2.50000000","permanent_amount":"0.00000000"}]`, repo.updates[SettingKeyDailyCheckinRewardTiers])
}

func TestSettingService_UpdateSettings_DailyCheckinPolicyRejectsInvalidWithoutWriting(t *testing.T) {
	repo := &dailyCheckinPolicyRepoStub{}
	svc := NewSettingService(repo, &config.Config{})
	policy := DailyCheckinPolicy{
		MaxRewardDay: 2,
		RewardTiers:  []DailyCheckinRewardTier{{Day: 1, Amount: 1}},
	}

	err := svc.UpdateDailyCheckinPolicy(context.Background(), &policy)
	require.Error(t, err)
	require.Equal(t, "INVALID_DAILY_CHECKIN_POLICY", infraerrors.Reason(err))
	require.Nil(t, repo.updates)
	require.Zero(t, repo.setMultipleCalls)
}

func TestSettingService_GetDailyCheckinPolicyDefaultsAndRejectsPartialStoredConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		svc := NewSettingService(&dailyCheckinPolicyRepoStub{values: map[string]string{}}, &config.Config{})

		policy, err := svc.GetDailyCheckinPolicy(context.Background())
		require.NoError(t, err)
		require.False(t, policy.Enabled)
		require.Equal(t, 7, policy.MaxRewardDay)
		require.Len(t, policy.RewardTiers, 7)
	})

	t.Run("partial stored config", func(t *testing.T) {
		svc := NewSettingService(&dailyCheckinPolicyRepoStub{values: map[string]string{
			SettingKeyDailyCheckinEnabled: "true",
		}}, &config.Config{})

		_, err := svc.GetDailyCheckinPolicy(context.Background())
		require.Error(t, err)
		require.Equal(t, "INVALID_DAILY_CHECKIN_POLICY", infraerrors.Reason(err))
	})

	t.Run("legacy reward tiers default permanent amount to zero", func(t *testing.T) {
		svc := NewSettingService(&dailyCheckinPolicyRepoStub{values: map[string]string{
			SettingKeyDailyCheckinEnabled:      "true",
			SettingKeyDailyCheckinMaxRewardDay: "1",
			SettingKeyDailyCheckinRewardTiers:  `[{"day":1,"amount":"1.00000000"}]`,
		}}, &config.Config{})

		policy, err := svc.GetDailyCheckinPolicy(context.Background())
		require.NoError(t, err)
		require.Equal(t, 0.0, policy.RewardTiers[0].PermanentAmount)
	})
}

func TestParseDailyCheckinPolicySettings_RejectsInvalidEnabledValues(t *testing.T) {
	for _, tt := range []struct {
		name    string
		enabled string
	}{
		{name: "numeric", enabled: "1"},
		{name: "uppercase", enabled: "TRUE"},
		{name: "empty", enabled: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDailyCheckinPolicySettings(map[string]string{
				SettingKeyDailyCheckinEnabled:      tt.enabled,
				SettingKeyDailyCheckinMaxRewardDay: "1",
				SettingKeyDailyCheckinRewardTiers:  `[{"day":1,"amount":"1"}]`,
			})

			require.ErrorIs(t, err, ErrDailyCheckinPolicyInvalid)
		})
	}
}

func TestParseDailyCheckinRewardTiers_RejectsNonCanonicalAmounts(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "JSON number", raw: `[{"day":1,"amount":1}]`},
		{name: "scientific notation", raw: `[{"day":1,"amount":"1e0"}]`},
		{name: "positive sign", raw: `[{"day":1,"amount":"+1"}]`},
		{name: "negative sign", raw: `[{"day":1,"amount":"-1"}]`},
		{name: "leading zero", raw: `[{"day":1,"amount":"01"}]`},
		{name: "leading whitespace", raw: `[{"day":1,"amount":" 1"}]`},
		{name: "trailing whitespace", raw: `[{"day":1,"amount":"1 "}]`},
		{name: "too many decimal places", raw: `[{"day":1,"amount":"1.000000001"}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDailyCheckinRewardTiers(tt.raw)
			require.ErrorIs(t, err, ErrDailyCheckinPolicyInvalid)
		})
	}
}

func TestParseDailyCheckinRewardTiersAcceptsZeroAndRejectsNegativePermanentAmounts(t *testing.T) {
	tiers, err := parseDailyCheckinRewardTiers(`[{"day":1,"amount":"1.00000000","permanent_amount":"0.00000000"}]`)
	require.NoError(t, err)
	require.Equal(t, 0.0, tiers[0].PermanentAmount)

	_, err = parseDailyCheckinRewardTiers(`[{"day":1,"amount":"1.00000000","permanent_amount":"-0.00000001"}]`)
	require.ErrorIs(t, err, ErrDailyCheckinPolicyInvalid)
}

func TestSettingService_GetAllSettingsIgnoresDailyCheckinPolicy(t *testing.T) {
	for _, tt := range []struct {
		name   string
		values map[string]string
	}{
		{
			name: "partial policy",
			values: map[string]string{
				SettingKeyDailyCheckinEnabled: "true",
			},
		},
		{
			name: "invalid enabled value",
			values: map[string]string{
				SettingKeyDailyCheckinEnabled:      "TRUE",
				SettingKeyDailyCheckinMaxRewardDay: "1",
				SettingKeyDailyCheckinRewardTiers:  `[{"day":1,"amount":"1"}]`,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &dailyCheckinPolicyRepoStub{values: tt.values}
			svc := NewSettingService(repo, &config.Config{})

			settings, err := svc.GetAllSettings(context.Background())

			require.NoError(t, err)
			require.NotNil(t, settings)
			require.Zero(t, repo.getMultipleCalls)
		})
	}
}

func TestSettingService_UpdateSettingsDoesNotWriteDailyCheckinKeys(t *testing.T) {
	repo := &dailyCheckinPolicyRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{BackendModeEnabled: false})

	require.NoError(t, err)
	require.Equal(t, 1, repo.setMultipleCalls)
	for _, key := range []string{
		SettingKeyDailyCheckinEnabled,
		SettingKeyDailyCheckinMaxRewardDay,
		SettingKeyDailyCheckinRewardTiers,
	} {
		require.NotContains(t, repo.updates, key)
	}
}

func ptrBusinessDate(value string) *string {
	return &value
}

type dailyCheckinPolicyRepoStub struct {
	values           map[string]string
	updates          map[string]string
	setMultipleCalls int
	getMultipleCalls int
}

func (s *dailyCheckinPolicyRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *dailyCheckinPolicyRepoStub) GetValue(context.Context, string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *dailyCheckinPolicyRepoStub) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (s *dailyCheckinPolicyRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	s.getMultipleCalls++
	result := make(map[string]string)
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *dailyCheckinPolicyRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	s.setMultipleCalls++
	s.updates = make(map[string]string, len(settings))
	if s.values == nil {
		s.values = make(map[string]string, len(settings))
	}
	for key, value := range settings {
		s.updates[key] = value
		s.values[key] = value
	}
	return nil
}

func (s *dailyCheckinPolicyRepoStub) GetAll(context.Context) (map[string]string, error) {
	result := make(map[string]string, len(s.values))
	for key, value := range s.values {
		result[key] = value
	}
	return result, nil
}

func (s *dailyCheckinPolicyRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}
