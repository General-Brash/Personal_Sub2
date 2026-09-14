package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"reflect"
	"testing"
	"time"
)

func TestCheckinAdminClockViewRejectsStaleFormAndPreservesTransition(t *testing.T) {
	base := DefaultDailyCheckinPolicy()
	raw := DefaultDailyCheckinPolicyV2()
	boundary := time.Date(2026, 9, 12, 0, 0, 0, 0, beijingLocation)
	raw.PendingRefresh = &DailyCheckinPendingV2{RefreshTime: "08:00", EffectiveAt: boundary}
	raw.Version = EffectiveCheckinPolicyVersion(&base, raw)
	oldView := checkinAdminPolicyView(&base, raw, boundary.Add(-time.Second))
	now := boundary.Add(time.Hour)
	view := checkinAdminPolicyView(&base, raw, now)
	require.Equal(t, "08:00", view.RefreshTime)
	require.Nil(t, view.PendingRefresh)
	require.Equal(t, "00:00", raw.RefreshTime)
	require.NotNil(t, raw.PendingRefresh)
	require.NotEqual(t, oldView.Version, view.Version)
	before, err := checkinPeriodAt(raw, now)
	require.NoError(t, err)
	shown, err := view.CurrentPeriod(now)
	require.NoError(t, err)
	require.Equal(t, before, shown)
	stale := oldView
	stale.AutoFeeBps = 700
	require.ErrorIs(t, prepareCheckinPolicyV2Change(&base, raw, &stale, oldView.Version, now), ErrCheckinPolicyVersionStale)
	unrelated := view
	unrelated.AutoFeeBps = 700
	require.NoError(t, prepareCheckinPolicyV2Change(&base, raw, &unrelated, view.Version, now))
	stored, err := unrelated.settingValue()
	require.NoError(t, err)
	roundTrip, err := parseDailyCheckinPolicyV2(stored)
	require.NoError(t, err)
	after, err := checkinPeriodAt(roundTrip, now)
	require.NoError(t, err)
	require.Equal(t, "08:00", roundTrip.RefreshTime)
	require.Equal(t, before, after)
	require.Equal(t, 700, roundTrip.AutoFeeBps)
}

func TestCheckinAdminFreshFormCanIntentionallyReturnToPreviousClock(t *testing.T) {
	base := DefaultDailyCheckinPolicy()
	raw := DefaultDailyCheckinPolicyV2()
	boundary := time.Date(2026, 9, 12, 0, 0, 0, 0, beijingLocation)
	raw.PendingRefresh = &DailyCheckinPendingV2{RefreshTime: "08:00", EffectiveAt: boundary}
	now := boundary.Add(time.Hour)
	view := checkinAdminPolicyView(&base, raw, now)
	updated := view
	updated.RefreshTime = "00:00"
	require.NoError(t, prepareCheckinPolicyV2Change(&base, raw, &updated, view.Version, now))
	require.Equal(t, "08:00", updated.RefreshTime)
	require.Equal(t, "00:00", updated.PendingRefresh.RefreshTime)
	require.Equal(t, boundary.Add(8*time.Hour), updated.PendingRefresh.EffectiveAt)
	current, err := updated.CurrentPeriod(now)
	require.NoError(t, err)
	require.Equal(t, boundary, current.StartAt)
	require.Equal(t, boundary.Add(8*time.Hour), current.NextReset)
	next, err := updated.CurrentPeriod(current.NextReset)
	require.NoError(t, err)
	require.Equal(t, current.NextReset, next.StartAt)
	require.Equal(t, boundary.AddDate(0, 0, 1), next.NextReset)
}

type p35CheckinSettingsRepository struct {
	SettingRepository
	values    map[string]string
	writes    int
	rejectCAS bool
}

func (r *p35CheckinSettingsRepository) GetMultiple(context.Context, []string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range r.values {
		out[k] = v
	}
	return out, nil
}
func (r *p35CheckinSettingsRepository) CompareAndSetMultiple(_ context.Context, expected, updates map[string]string) (bool, error) {
	if r.rejectCAS || !reflect.DeepEqual(expected, r.values) {
		return false, nil
	}
	for k, v := range updates {
		r.values[k] = v
	}
	r.writes++
	return true, nil
}

func TestCheckinPolicyGetIsReadOnlyAndUnrelatedSaveUsesEffectiveClock(t *testing.T) {
	base := DefaultDailyCheckinPolicy()
	raw := DefaultDailyCheckinPolicyV2()
	raw.PendingRefresh = &DailyCheckinPendingV2{RefreshTime: "08:00", EffectiveAt: time.Date(2020, 1, 1, 0, 0, 0, 0, beijingLocation)}
	values, err := base.settingValues()
	require.NoError(t, err)
	serialized, err := raw.settingValue()
	require.NoError(t, err)
	values[SettingKeyDailyCheckinPolicyV2] = serialized
	repo := &p35CheckinSettingsRepository{values: values}
	svc := &SettingService{settingRepo: repo}
	gotBase, view, err := svc.GetDailyCheckinPolicyV2(context.Background())
	require.NoError(t, err)
	require.Equal(t, "08:00", view.RefreshTime)
	require.Zero(t, repo.writes)
	require.Equal(t, serialized, repo.values[SettingKeyDailyCheckinPolicyV2])
	view.AutoFeeBps = 700
	require.NoError(t, svc.UpdateDailyCheckinPolicyV2(context.Background(), gotBase, &view, view.Version))
	stored, err := parseDailyCheckinPolicyV2(repo.values[SettingKeyDailyCheckinPolicyV2])
	require.NoError(t, err)
	require.Equal(t, "08:00", stored.RefreshTime)
	require.Nil(t, stored.PendingRefresh)
	require.NotNil(t, stored.RefreshEffectiveAt)
	require.Equal(t, 1, repo.writes)
	_, fresh, err := svc.GetDailyCheckinPolicyV2(context.Background())
	require.NoError(t, err)
	repo.rejectCAS = true
	require.ErrorIs(t, svc.UpdateDailyCheckinPolicyV2(context.Background(), gotBase, &fresh, fresh.Version), ErrCheckinPolicyVersionStale)
	require.Equal(t, 1, repo.writes)
}
