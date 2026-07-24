package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"
)

type cleanupReloadSequence struct {
	errs  []error
	calls int
}

func (s *cleanupReloadSequence) Reload(context.Context) error {
	s.calls++
	if len(s.errs) == 0 {
		return nil
	}
	err := s.errs[0]
	s.errs = s.errs[1:]
	return err
}

func TestValidateOpsAdvancedSettingsRejectsInvalidCleanupCron(t *testing.T) {
	cfg := defaultOpsAdvancedSettings()
	cfg.DataRetention.CleanupSchedule = "not a cron"
	require.ErrorContains(t, validateOpsAdvancedSettings(cfg), "cleanup_schedule")
}

func TestUpdateDataCleanupSettingsRollsBackBothSettingsWhenReloadFails(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	oldAdvanced := defaultOpsAdvancedSettings()
	oldAdvanced.DataRetention.CleanupSchedule = "0 2 * * *"
	oldRaw, err := json.Marshal(oldAdvanced)
	require.NoError(t, err)
	repo.values[SettingKeyOpsAdvancedSettings] = string(oldRaw)
	repo.values[SettingKeyAuditLogRetentionDays] = "180"

	reloader := &cleanupReloadSequence{errs: []error{errors.New("reload failed"), nil}}
	svc := &OpsService{settingRepo: repo, cleanupReloader: reloader}
	newRetention := oldAdvanced.DataRetention
	newRetention.CleanupSchedule = "5 3 * * *"
	newRetention.Targets["ops_error_logs"] = OpsRetentionPolicy{Enabled: true, RetentionDays: 7}

	_, err = svc.UpdateDataCleanupSettings(context.Background(), newRetention, 30)
	require.ErrorContains(t, err, "reload cleanup schedule")
	require.Equal(t, string(oldRaw), repo.values[SettingKeyOpsAdvancedSettings])
	require.Equal(t, "180", repo.values[SettingKeyAuditLogRetentionDays])
	require.Equal(t, 2, reloader.calls)
}

func TestUpdateDataCleanupSettingsValidatesBeforeWriting(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	advanced := defaultOpsAdvancedSettings()
	oldRaw, err := json.Marshal(advanced)
	require.NoError(t, err)
	repo.values[SettingKeyOpsAdvancedSettings] = string(oldRaw)
	repo.values[SettingKeyAuditLogRetentionDays] = "180"

	invalid := advanced.DataRetention
	invalid.CleanupSchedule = "bad cron"
	svc := &OpsService{settingRepo: repo}
	_, err = svc.UpdateDataCleanupSettings(context.Background(), invalid, 30)
	require.ErrorContains(t, err, "cleanup_schedule")
	require.Equal(t, string(oldRaw), repo.values[SettingKeyOpsAdvancedSettings])
	require.Equal(t, "180", repo.values[SettingKeyAuditLogRetentionDays])
}

func TestApplyScheduleLockedKeepsPreviousCronOnInvalidStoredSchedule(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	advanced := defaultOpsAdvancedSettings()
	advanced.DataRetention.CleanupEnabled = true
	advanced.DataRetention.CleanupSchedule = "invalid"
	raw, err := json.Marshal(advanced)
	require.NoError(t, err)
	repo.values[SettingKeyOpsAdvancedSettings] = string(raw)

	base := config.OpsCleanupConfig{Enabled: true, Schedule: "0 2 * * *", ErrorLogRetentionDays: 30}
	oldCron := cron.New()
	svc := &OpsCleanupService{
		cfg:              &config.Config{Ops: config.OpsConfig{Cleanup: base}},
		settingRepo:      repo,
		cron:             oldCron,
		effective:        base,
		effectiveTargets: defaultOpsRetentionPolicies(30, 30, 30),
	}

	err = svc.applyScheduleLocked(context.Background())
	require.Error(t, err)
	require.Same(t, oldCron, svc.cron)
	require.Equal(t, base.Schedule, svc.effective.Schedule)
}
