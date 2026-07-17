package service

import "time"

const (
	SettingKeyDailyCheckinEnabled      = "daily_checkin_enabled"
	SettingKeyDailyCheckinMaxRewardDay = "daily_checkin_max_reward_day"
	SettingKeyDailyCheckinRewardTiers  = "daily_checkin_reward_tiers"
)

type Setting struct {
	ID        int64
	Key       string
	Value     string
	UpdatedAt time.Time
}
