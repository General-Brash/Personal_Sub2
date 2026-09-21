package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func newFeeConsentSettingService(t *testing.T, feeBps int) (*SettingService, *p35CheckinSettingsRepository) {
	t.Helper()
	base := DefaultDailyCheckinPolicy()
	raw := DefaultDailyCheckinPolicyV2()
	raw.AutoFeeBps = feeBps
	values, err := base.settingValues()
	require.NoError(t, err)
	serialized, err := raw.settingValue()
	require.NoError(t, err)
	values[SettingKeyDailyCheckinPolicyV2] = serialized
	repo := &p35CheckinSettingsRepository{values: values}
	return &SettingService{settingRepo: repo}, repo
}

// A fee reduction stays within the ceiling the user already agreed to ("at most
// N bps"), so an existing auto check-in consent must remain valid. Otherwise the
// auto check-in is silently blocked, the day is missed, and the streak resets.
func TestCheckinConsentSurvivesFeeReduction(t *testing.T) {
	svc, _ := newFeeConsentSettingService(t, 500)

	gotBase, view, err := svc.GetDailyCheckinPolicyV2(context.Background())
	require.NoError(t, err)

	// The user consented at the 500 bps ceiling under the current version.
	consented := CheckinPreference{AutoEnabled: true, ConsentPolicyVersion: view.Version, ConsentFeeBps: view.AutoFeeBps}
	require.True(t, decorateCheckinPreference(consented, view).ConsentValid, "consent valid at the agreed fee")

	reduced := view
	reduced.AutoFeeBps = 200
	require.NoError(t, svc.UpdateDailyCheckinPolicyV2(context.Background(), gotBase, &reduced, view.Version))

	_, afterReduce, err := svc.GetDailyCheckinPolicyV2(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, view.Version, afterReduce.Version, "the fee is embedded in the policy version")
	require.True(t, afterReduce.AcceptsConsentVersion(view.Version), "the old consent version is registered as compatible")
	require.True(t, decorateCheckinPreference(consented, afterReduce).ConsentValid, "consent survives a fee reduction")

	// A further reduction keeps the very first consent valid (the chain is re-pointed).
	further := afterReduce
	further.AutoFeeBps = 100
	require.NoError(t, svc.UpdateDailyCheckinPolicyV2(context.Background(), gotBase, &further, afterReduce.Version))
	_, afterSecond, err := svc.GetDailyCheckinPolicyV2(context.Background())
	require.NoError(t, err)
	require.True(t, afterSecond.AcceptsConsentVersion(view.Version), "the original consent version is still compatible after a second reduction")
	require.True(t, decorateCheckinPreference(consented, afterSecond).ConsentValid, "consent survives repeated fee reductions")
}

// A fee increase above the agreed ceiling must force re-consent.
func TestCheckinConsentInvalidatedByFeeIncrease(t *testing.T) {
	svc, _ := newFeeConsentSettingService(t, 500)

	gotBase, view, err := svc.GetDailyCheckinPolicyV2(context.Background())
	require.NoError(t, err)
	consented := CheckinPreference{AutoEnabled: true, ConsentPolicyVersion: view.Version, ConsentFeeBps: view.AutoFeeBps}

	increased := view
	increased.AutoFeeBps = 800
	require.NoError(t, svc.UpdateDailyCheckinPolicyV2(context.Background(), gotBase, &increased, view.Version))

	_, afterIncrease, err := svc.GetDailyCheckinPolicyV2(context.Background())
	require.NoError(t, err)
	require.False(t, afterIncrease.AcceptsConsentVersion(view.Version), "a fee increase drops consent compatibility")
	require.False(t, decorateCheckinPreference(consented, afterIncrease).ConsentValid, "a fee increase forces re-consent")
}
