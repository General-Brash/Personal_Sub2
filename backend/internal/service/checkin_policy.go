package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var beijingLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(fmt.Sprintf("load Asia/Shanghai location: %v", err))
	}
	return location
}()

const dailyCheckinMaxRewardDay = 365

type DailyCheckinRewardTier struct {
	Day             int     `json:"day"`
	Amount          float64 `json:"amount"`
	PermanentAmount float64 `json:"permanent_amount"`
}

type DailyCheckinPolicy struct {
	Enabled      bool                     `json:"enabled"`
	MaxRewardDay int                      `json:"max_reward_day"`
	RewardTiers  []DailyCheckinRewardTier `json:"reward_tiers"`
}

func DefaultDailyCheckinPolicy() DailyCheckinPolicy {
	tiers := make([]DailyCheckinRewardTier, 7)
	for day := range tiers {
		tiers[day] = DailyCheckinRewardTier{
			Day:             day + 1,
			Amount:          1,
			PermanentAmount: 0,
		}
	}
	return DailyCheckinPolicy{
		Enabled:      false,
		MaxRewardDay: len(tiers),
		RewardTiers:  tiers,
	}
}

func BeijingBusinessDate(now time.Time) string {
	return now.In(beijingLocation).Format("2006-01-02")
}

func NextBeijingMidnight(now time.Time) time.Time {
	beijingNow := now.In(beijingLocation)
	return time.Date(
		beijingNow.Year(),
		beijingNow.Month(),
		beijingNow.Day()+1,
		0,
		0,
		0,
		0,
		beijingLocation,
	)
}

func NextCheckinStreak(lastCheckinDate *string, lastStreakDay int, currentDate time.Time) int {
	if lastCheckinDate == nil {
		return 1
	}
	lastDate, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(*lastCheckinDate), beijingLocation)
	if err != nil {
		return 1
	}
	previousDate := currentDate.In(beijingLocation).AddDate(0, 0, -1).Format("2006-01-02")
	if lastDate.Format("2006-01-02") == previousDate {
		if lastStreakDay > 0 {
			return lastStreakDay + 1
		}
		return 2
	}
	return 1
}

func (p DailyCheckinPolicy) Validate() error {
	if p.MaxRewardDay < 1 || p.MaxRewardDay > dailyCheckinMaxRewardDay {
		return ErrDailyCheckinPolicyInvalid
	}
	if len(p.RewardTiers) != p.MaxRewardDay {
		return ErrDailyCheckinPolicyInvalid
	}
	for index, tier := range p.RewardTiers {
		if tier.Day != index+1 {
			return ErrDailyCheckinPolicyInvalid
		}
		if err := ValidateTemporaryCreditAmount(tier.Amount); err != nil {
			return ErrDailyCheckinPolicyInvalid
		}
		if err := validatePermanentRewardAmount(tier.PermanentAmount); err != nil {
			return ErrDailyCheckinPolicyInvalid
		}
	}
	return nil
}

func (p DailyCheckinPolicy) RewardForStreak(streakDay int) (int, float64, error) {
	rewardDay, temporaryAmount, _, err := p.RewardForStreakAmounts(streakDay)
	return rewardDay, temporaryAmount, err
}

// RewardForStreakAmounts resolves both credit balances for a streak day. The
// legacy RewardForStreak method above intentionally keeps its original return
// shape for callers that only consume temporary credit.
func (p DailyCheckinPolicy) RewardForStreakAmounts(streakDay int) (int, float64, float64, error) {
	if err := p.Validate(); err != nil {
		return 0, 0, 0, err
	}
	if streakDay < 1 {
		return 0, 0, 0, ErrDailyCheckinPolicyInvalid
	}
	rewardDay := streakDay
	if rewardDay > p.MaxRewardDay {
		rewardDay = p.MaxRewardDay
	}
	tier := p.RewardTiers[rewardDay-1]
	return rewardDay, tier.Amount, tier.PermanentAmount, nil
}

func (p DailyCheckinPolicy) settingValues() (map[string]string, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	tiers := make([]dailyCheckinRewardTierValue, len(p.RewardTiers))
	for index, tier := range p.RewardTiers {
		tiers[index] = dailyCheckinRewardTierValue{
			Day:             tier.Day,
			Amount:          formatLedgerAmount(tier.Amount),
			PermanentAmount: formatLedgerAmount(tier.PermanentAmount),
		}
	}
	rawTiers, err := json.Marshal(tiers)
	if err != nil {
		return nil, fmt.Errorf("marshal daily checkin reward tiers: %w", err)
	}
	return map[string]string{
		SettingKeyDailyCheckinEnabled:      fmt.Sprintf("%t", p.Enabled),
		SettingKeyDailyCheckinMaxRewardDay: fmt.Sprintf("%d", p.MaxRewardDay),
		SettingKeyDailyCheckinRewardTiers:  string(rawTiers),
	}, nil
}

type dailyCheckinRewardTierValue struct {
	Day             int    `json:"day"`
	Amount          string `json:"amount"`
	PermanentAmount string `json:"permanent_amount,omitempty"`
}

func parseDailyCheckinRewardTiers(raw string) ([]DailyCheckinRewardTier, error) {
	var values []dailyCheckinRewardTierValue
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, ErrDailyCheckinPolicyInvalid
	}
	tiers := make([]DailyCheckinRewardTier, len(values))
	for index, value := range values {
		amount, err := ParseStrictPositiveLedgerAmount(value.Amount)
		if err != nil {
			return nil, ErrDailyCheckinPolicyInvalid
		}
		permanentAmount := 0.0
		if value.PermanentAmount != "" {
			permanentAmount, err = ParseStrictLedgerAmount(value.PermanentAmount)
			if err != nil {
				return nil, ErrDailyCheckinPolicyInvalid
			}
		}
		tiers[index] = DailyCheckinRewardTier{
			Day:             value.Day,
			Amount:          amount,
			PermanentAmount: permanentAmount,
		}
	}
	return tiers, nil
}

func validatePermanentRewardAmount(amount float64) error {
	normalized, err := normalizeLedgerAmount(amount)
	if err != nil || normalized < 0 {
		return ErrDailyCheckinPolicyInvalid
	}
	return nil
}

func parseDailyCheckinPolicySettings(settings map[string]string) (*DailyCheckinPolicy, error) {
	keys := []string{
		SettingKeyDailyCheckinEnabled,
		SettingKeyDailyCheckinMaxRewardDay,
		SettingKeyDailyCheckinRewardTiers,
	}
	present := 0
	for _, key := range keys {
		if _, ok := settings[key]; ok {
			present++
		}
	}
	if present == 0 {
		policy := DefaultDailyCheckinPolicy()
		return &policy, nil
	}
	if present != len(keys) {
		return nil, ErrDailyCheckinPolicyInvalid
	}

	maxRewardDay, err := strconv.Atoi(settings[SettingKeyDailyCheckinMaxRewardDay])
	if err != nil {
		return nil, ErrDailyCheckinPolicyInvalid
	}
	tiers, err := parseDailyCheckinRewardTiers(settings[SettingKeyDailyCheckinRewardTiers])
	if err != nil {
		return nil, err
	}
	enabledRaw := settings[SettingKeyDailyCheckinEnabled]
	var enabled bool
	switch enabledRaw {
	case "true":
		enabled = true
	case "false":
		enabled = false
	default:
		return nil, ErrDailyCheckinPolicyInvalid
	}

	policy := &DailyCheckinPolicy{
		Enabled:      enabled,
		MaxRewardDay: maxRewardDay,
		RewardTiers:  tiers,
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return policy, nil
}
