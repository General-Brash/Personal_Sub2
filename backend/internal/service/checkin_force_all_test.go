package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// forceAllCheckinSettings 构造一份「全员强制自动签到」开关处于指定状态的设置快照，
// 并返回解析后的策略，方便直接算出周期与同意版本。
func forceAllCheckinSettings(t *testing.T, forceAll bool, feeBps int) (map[string]string, *DailyCheckinPolicy, DailyCheckinPolicyV2) {
	t.Helper()
	base := DefaultDailyCheckinPolicy()
	base.Enabled = true
	values, err := base.settingValues()
	require.NoError(t, err)
	extended := DefaultDailyCheckinPolicyV2()
	extended.AutoFeeBps = feeBps
	extended.AutoForceAll = forceAll
	raw, err := extended.settingValue()
	require.NoError(t, err)
	values[SettingKeyDailyCheckinPolicyV2] = raw
	parsedBase, parsedExtended, err := parseCheckinPolicyBundle(values)
	require.NoError(t, err)
	return values, parsedBase, parsedExtended
}

func expectCheckinPolicyBundle(mock sqlmock.Sqlmock, values map[string]string) {
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs(SettingKeyDailyCheckinEnabled, SettingKeyDailyCheckinMaxRewardDay, SettingKeyDailyCheckinRewardTiers, SettingKeyDailyCheckinPolicyV2).
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "max_reward_day", "reward_tiers", "policy_v2"}).AddRow(
			values[SettingKeyDailyCheckinEnabled],
			values[SettingKeyDailyCheckinMaxRewardDay],
			values[SettingKeyDailyCheckinRewardTiers],
			values[SettingKeyDailyCheckinPolicyV2],
		))
}

func expectCheckinPreferenceRow(mock sqlmock.Sqlmock, userID int64, autoEnabled bool, consentVersion string, consentFeeBps int) {
	mock.ExpectQuery("SELECT auto_enabled, consent_policy_version, consent_fee_bps, consented_at").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"auto_enabled", "consent_policy_version", "consent_fee_bps", "consented_at"}).
			AddRow(autoEnabled, consentVersion, consentFeeBps, nil))
}

func newForceAllCheckinService(db *sql.DB, base *DailyCheckinPolicy, now time.Time) (*CheckinService, *checkinTemporaryCreditRepositoryStub) {
	creditRepo := &checkinTemporaryCreditRepositoryStub{}
	svc := NewCheckinServiceV2WithClock(
		db,
		checkinPolicyProviderStub{policy: base},
		NewTemporaryCreditServiceWithClock(creditRepo, func() time.Time { return now }),
		func() time.Time { return now },
	)
	return svc, creditRepo
}

func expectForcedCheckinInsert(mock sqlmock.Sqlmock, userID int64, period CheckinPeriod, mode CheckinMode, policyVersion string, autoFeeBps int) {
	mock.ExpectQuery("INSERT INTO daily_checkins").
		WithArgs(
			userID, period.Date, 1, 1, "1.00000000", "0.00000000",
			period.ID, period.StartAt, period.NextReset, mode, "1.00000000",
			defaultCheckinRandomBps, "0.00000000", autoFeeBps, policyVersion, "",
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "checkin_date", "streak_day", "reward_day", "reward_amount", "permanent_reward_amount",
			"mode", "business_period_key", "period_start_at", "period_end_at", "base_reward_amount",
			"multiplier_bps", "super_cost", "auto_fee_bps", "policy_version", "random_rule_version",
		}).AddRow(
			int64(7), period.Date, 1, 1, "1.00000000", "0.00000000",
			string(mode), period.ID, period.StartAt, period.NextReset, "1.00000000",
			defaultCheckinRandomBps, "0.00000000", autoFeeBps, policyVersion, "",
		))
}

// ★ 第一红线守护测试：auto_force_all 绝不能进入 policy_version 的 sha256 输入。
// 用户的自动签到同意绑定 policy_version，一旦该开关进入哈希，管理员每次切换开关
// 都会静默作废全站所有用户的同意，导致大批用户断签。
func TestCheckinPolicyVersionExcludesAutoForceAll(t *testing.T) {
	base := DefaultDailyCheckinPolicy()
	base.Enabled = true

	off := DefaultDailyCheckinPolicyV2()
	off.AutoFeeBps = 0
	on := off
	on.AutoForceAll = true

	require.Equal(t, EffectiveCheckinPolicyVersion(&base, off), EffectiveCheckinPolicyVersion(&base, on),
		"切换 auto_force_all 不得改变 policy_version")

	off.Version = EffectiveCheckinPolicyVersion(&base, off)
	on.Version = EffectiveCheckinPolicyVersion(&base, on)
	require.Equal(t, off.ConsentVersion(), on.ConsentVersion(), "切换 auto_force_all 不得改变同意版本")

	// 走完整的存取链路（settingValue → parseCheckinPolicyBundle）也必须保持一致。
	offValues, _, offParsed := forceAllCheckinSettings(t, false, 0)
	onValues, _, onParsed := forceAllCheckinSettings(t, true, 0)
	require.NotEqual(t, offValues[SettingKeyDailyCheckinPolicyV2], onValues[SettingKeyDailyCheckinPolicyV2],
		"开关本身必须真的落库")
	require.False(t, offParsed.AutoForceAll)
	require.True(t, onParsed.AutoForceAll)
	require.Equal(t, offParsed.Version, onParsed.Version)
	require.Equal(t, offParsed.ConsentVersion(), onParsed.ConsentVersion())
	require.True(t, onParsed.AcceptsConsentVersion(offParsed.ConsentVersion()),
		"开启强制开关后，旧同意版本仍然有效")
}

func TestCheckinPolicyV2_ForcedAutoRejectsNonZeroFee(t *testing.T) {
	policy := DefaultDailyCheckinPolicyV2()
	policy.AutoForceAll = true
	policy.AutoFeeBps = 500
	require.ErrorIs(t, policy.Validate(), ErrCheckinForcedAutoFeeNotZero)
	_, err := policy.settingValue()
	require.ErrorIs(t, err, ErrCheckinForcedAutoFeeNotZero)

	policy.AutoFeeBps = 0
	require.NoError(t, policy.Validate())

	// 管理端保存路径同样被拒，且不会写入设置表。
	svc, repo := newFeeConsentSettingService(t, 500)
	gotBase, view, err := svc.GetDailyCheckinPolicyV2(context.Background())
	require.NoError(t, err)
	stored := repo.values[SettingKeyDailyCheckinPolicyV2]

	rejected := view
	rejected.AutoForceAll = true
	rejected.AutoFeeBps = 500
	require.ErrorIs(t,
		svc.UpdateDailyCheckinPolicyV2(context.Background(), gotBase, &rejected, view.Version),
		ErrCheckinForcedAutoFeeNotZero)
	require.Zero(t, repo.writes)
	require.Equal(t, stored, repo.values[SettingKeyDailyCheckinPolicyV2])

	accepted := view
	accepted.AutoForceAll = true
	accepted.AutoFeeBps = 0
	require.NoError(t, svc.UpdateDailyCheckinPolicyV2(context.Background(), gotBase, &accepted, view.Version))
	_, saved, err := svc.GetDailyCheckinPolicyV2(context.Background())
	require.NoError(t, err)
	require.True(t, saved.AutoForceAll)
	require.Zero(t, saved.AutoFeeBps)
}

func TestCheckinForceAll_AwardsNeverConsentedUserWithZeroFee(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.September, 23, 12, 0, 0, 0, beijingLocation)
	values, base, extended := forceAllCheckinSettings(t, true, 0)
	period, err := checkinPeriodAt(extended, now)
	require.NoError(t, err)
	svc, creditRepo := newForceAllCheckinService(db, base, now)

	mock.ExpectBegin()
	expectCheckinUserLock(mock, 42)
	expectCheckinPolicyBundle(mock, values)
	mock.ExpectQuery("AND business_period_key = \\$2").
		WithArgs(int64(42), period.ID).
		WillReturnError(sql.ErrNoRows)
	// 该用户 auto_enabled=false 且从未同意过任何版本。
	expectCheckinPreferenceRow(mock, 42, false, "", 0)
	mock.ExpectQuery("SELECT period_end_at, streak_day FROM daily_checkins").
		WithArgs(int64(42)).
		WillReturnError(sql.ErrNoRows)
	expectForcedCheckinInsert(mock, 42, period, CheckinModeDirectAuto, extended.ConsentVersion(), 0)
	mock.ExpectCommit()

	result, err := svc.checkInV2(context.Background(), 42, CheckinModeDirectAuto, nil)
	require.NoError(t, err)
	require.False(t, result.AlreadyCheckedIn)
	require.Equal(t, CheckinModeDirectAuto, result.Mode)
	require.Zero(t, result.AutoFeeBps, "强制期手续费必须为 0")
	require.Equal(t, "1.00000000", result.RewardAmount, "零手续费下奖励不被扣减")
	require.Len(t, creditRepo.created, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckinForceAll_UpdatePreferenceIsRejectedWithoutWriting(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.September, 23, 12, 0, 0, 0, beijingLocation)
	values, base, _ := forceAllCheckinSettings(t, true, 0)
	svc, _ := newForceAllCheckinService(db, base, now)

	for _, autoEnabled := range []bool{true, false} {
		mock.ExpectBegin()
		expectCheckinUserLock(mock, 42)
		expectCheckinPolicyBundle(mock, values)
		mock.ExpectRollback()

		_, err := svc.UpdatePreference(context.Background(), 42, autoEnabled, true, "", 0)
		require.ErrorIs(t, err, ErrCheckinAutoForcedByAdmin)
	}
	// 未出现 INSERT INTO daily_checkin_preferences：用户偏好原值被完整保留，
	// 关闭强制开关后才能回到用户自己的选择。
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckinForceAll_DisablingRestoresConsentRequirement(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.September, 23, 12, 0, 0, 0, beijingLocation)
	values, base, extended := forceAllCheckinSettings(t, false, 0)
	period, err := checkinPeriodAt(extended, now)
	require.NoError(t, err)
	svc, creditRepo := newForceAllCheckinService(db, base, now)

	mock.ExpectBegin()
	expectCheckinUserLock(mock, 42)
	expectCheckinPolicyBundle(mock, values)
	mock.ExpectQuery("AND business_period_key = \\$2").
		WithArgs(int64(42), period.ID).
		WillReturnError(sql.ErrNoRows)
	// 强制开关关闭后，用户偏好仍是未开启/未同意的原值。
	expectCheckinPreferenceRow(mock, 42, false, "", 0)
	mock.ExpectRollback()

	_, err = svc.checkInV2(context.Background(), 42, CheckinModeDirectAuto, nil)
	require.ErrorIs(t, err, ErrCheckinConsentRequired)
	require.Empty(t, creditRepo.created)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 运行时双重保险：历史脏数据（force_all=true 且费率非 0）不得绕过同意去扣费。
func TestCheckinForceAll_NonZeroFeeDoesNotBypassConsent(t *testing.T) {
	dirty := DefaultDailyCheckinPolicyV2()
	dirty.AutoForceAll = true
	dirty.AutoFeeBps = 500
	require.False(t, dirty.forcedAutoCheckin())

	preference := decorateCheckinPreference(CheckinPreference{}, dirty)
	require.False(t, preference.AutoForcedByAdmin)
	require.False(t, preference.AutoEnabled)
	require.False(t, preference.ConsentValid)

	clean := dirty
	clean.AutoFeeBps = 0
	forced := decorateCheckinPreference(CheckinPreference{}, clean)
	require.True(t, forced.AutoForcedByAdmin)
	require.True(t, forced.AutoEnabled)
	require.True(t, forced.ConsentValid)
}

func TestCheckinForceAll_ManualDirectClaimStaysAvailableOncePerPeriod(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.September, 23, 12, 0, 0, 0, beijingLocation)
	values, base, extended := forceAllCheckinSettings(t, true, 0)
	period, err := checkinPeriodAt(extended, now)
	require.NoError(t, err)
	svc, creditRepo := newForceAllCheckinService(db, base, now)

	mock.ExpectBegin()
	expectCheckinUserLock(mock, 42)
	expectCheckinPolicyBundle(mock, values)
	mock.ExpectQuery("AND business_period_key = \\$2").
		WithArgs(int64(42), period.ID).
		WillReturnError(sql.ErrNoRows)
	expectCheckinPreferenceRow(mock, 42, false, "", 0)
	mock.ExpectQuery("SELECT period_end_at, streak_day FROM daily_checkins").
		WithArgs(int64(42)).
		WillReturnError(sql.ErrNoRows)
	expectForcedCheckinInsert(mock, 42, period, CheckinModeDirect, extended.ConsentVersion(), 0)
	mock.ExpectCommit()

	first, err := svc.checkInV2(context.Background(), 42, CheckinModeDirect, nil)
	require.NoError(t, err)
	require.False(t, first.AlreadyCheckedIn)
	require.Equal(t, CheckinModeDirect, first.Mode)

	// 同周期再次手动领取只能命中重放，不会产生第二份发放。
	mock.ExpectBegin()
	expectCheckinUserLock(mock, 42)
	expectCheckinPolicyBundle(mock, values)
	mock.ExpectQuery("AND business_period_key = \\$2").
		WithArgs(int64(42), period.ID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "checkin_date", "streak_day", "reward_day", "reward_amount", "permanent_reward_amount",
			"mode", "business_period_key", "period_start_at", "period_end_at", "base_reward_amount",
			"multiplier_bps", "super_cost", "auto_fee_bps", "policy_version", "random_rule_version",
		}).AddRow(
			int64(7), period.Date, 1, 1, "1.00000000", "0.00000000",
			string(CheckinModeDirect), period.ID, period.StartAt, period.NextReset, "1.00000000",
			defaultCheckinRandomBps, "0.00000000", 0, extended.ConsentVersion(), "",
		))
	mock.ExpectQuery("SELECT id, expires_at FROM temporary_credit_grants").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "expires_at"}).AddRow(int64(91), period.NextReset))
	mock.ExpectCommit()

	second, err := svc.checkInV2(context.Background(), 42, CheckinModeDirect, nil)
	require.NoError(t, err)
	require.True(t, second.AlreadyCheckedIn)
	require.Len(t, creditRepo.created, 1, "重放不得产生第二份临时额度发放")
	require.NoError(t, mock.ExpectationsWereMet())
}
