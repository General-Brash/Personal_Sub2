//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type pageVisibilityInitRepoStub struct {
	updates map[string]string
}

func (s *pageVisibilityInitRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *pageVisibilityInitRepoStub) GetValue(context.Context, string) (string, error) {
	return "", ErrSettingNotFound
}

func (s *pageVisibilityInitRepoStub) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (s *pageVisibilityInitRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (s *pageVisibilityInitRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	s.updates = make(map[string]string, len(settings))
	for key, value := range settings {
		s.updates[key] = value
	}
	return nil
}

func (s *pageVisibilityInitRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *pageVisibilityInitRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func TestSettingService_InitializeDefaultSettings_PageVisibilityDefaultsToEnabled(t *testing.T) {
	repo := &pageVisibilityInitRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	require.NoError(t, svc.InitializeDefaultSettings(context.Background()))
	require.Equal(t, "true", repo.updates[SettingKeyUserChannelStatusEnabled])
	require.Equal(t, "true", repo.updates[SettingKeyUserSubscriptionsEnabled])
	require.Equal(t, "true", repo.updates[SettingKeyAdminSubscriptionsEnabled])
	require.Equal(t, "true", repo.updates[SettingKeyAdminPromoCodesEnabled])
	require.Equal(t, "true", repo.updates[SettingKeyAdminChannelManagementEnabled])
}

func TestSettingService_GetAllSettings_PageVisibilityDefaultsToEnabledWhenMissing(t *testing.T) {
	svc := NewSettingService(&settingGetAllRepoStub{values: map[string]string{}}, &config.Config{})

	settings, err := svc.GetAllSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.UserChannelStatusEnabled)
	require.True(t, settings.UserSubscriptionsEnabled)
	require.True(t, settings.AdminSubscriptionsEnabled)
	require.True(t, settings.AdminPromoCodesEnabled)
	require.True(t, settings.AdminChannelManagementEnabled)
}

func TestSettingService_GetAllSettings_PageVisibilityHonorsExplicitFalse(t *testing.T) {
	svc := NewSettingService(&settingGetAllRepoStub{values: map[string]string{
		SettingKeyUserChannelStatusEnabled:      "false",
		SettingKeyUserSubscriptionsEnabled:      "false",
		SettingKeyAdminPromoCodesEnabled:        "false",
		SettingKeyAdminChannelManagementEnabled: "false",
	}}, &config.Config{})

	settings, err := svc.GetAllSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.UserChannelStatusEnabled)
	require.False(t, settings.UserSubscriptionsEnabled)
	require.False(t, settings.AdminSubscriptionsEnabled)
	require.False(t, settings.AdminPromoCodesEnabled)
	require.False(t, settings.AdminChannelManagementEnabled)
}

func TestSettingService_UpdateSettings_PersistsPageVisibility(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		UserChannelStatusEnabled:      false,
		UserSubscriptionsEnabled:      true,
		AdminSubscriptionsEnabled:     true,
		AdminPromoCodesEnabled:        false,
		AdminChannelManagementEnabled: true,
	})
	require.NoError(t, err)
	require.Equal(t, "false", repo.updates[SettingKeyUserChannelStatusEnabled])
	require.Equal(t, "true", repo.updates[SettingKeyUserSubscriptionsEnabled])
	require.Equal(t, "true", repo.updates[SettingKeyAdminSubscriptionsEnabled])
	require.Equal(t, "false", repo.updates[SettingKeyAdminPromoCodesEnabled])
	require.Equal(t, "true", repo.updates[SettingKeyAdminChannelManagementEnabled])
}

func TestSettingService_GetPublicSettings_PageVisibilityDefaultsAndExplicitFalse(t *testing.T) {
	t.Run("missing keys default to enabled", func(t *testing.T) {
		svc := NewSettingService(&settingPublicRepoStub{values: map[string]string{}}, &config.Config{})

		settings, err := svc.GetPublicSettings(context.Background())
		require.NoError(t, err)
		require.True(t, settings.UserChannelStatusEnabled)
		require.True(t, settings.UserSubscriptionsEnabled)
		require.True(t, settings.AdminSubscriptionsEnabled)
		require.True(t, settings.AdminPromoCodesEnabled)
		require.True(t, settings.AdminChannelManagementEnabled)
	})

	t.Run("explicit false disables each page", func(t *testing.T) {
		svc := NewSettingService(&settingPublicRepoStub{values: map[string]string{
			SettingKeyUserChannelStatusEnabled:      "false",
			SettingKeyUserSubscriptionsEnabled:      "false",
			SettingKeyAdminSubscriptionsEnabled:     "false",
			SettingKeyAdminPromoCodesEnabled:        "false",
			SettingKeyAdminChannelManagementEnabled: "false",
		}}, &config.Config{})

		settings, err := svc.GetPublicSettings(context.Background())
		require.NoError(t, err)
		require.False(t, settings.UserChannelStatusEnabled)
		require.False(t, settings.UserSubscriptionsEnabled)
		require.False(t, settings.AdminSubscriptionsEnabled)
		require.False(t, settings.AdminPromoCodesEnabled)
		require.False(t, settings.AdminChannelManagementEnabled)
	})
}

func TestSettingService_GetPublicSettingsForInjection_IncludesPageVisibility(t *testing.T) {
	svc := NewSettingService(&settingPublicRepoStub{values: map[string]string{
		SettingKeyUserChannelStatusEnabled:      "false",
		SettingKeyUserSubscriptionsEnabled:      "false",
		SettingKeyAdminSubscriptionsEnabled:     "false",
		SettingKeyAdminPromoCodesEnabled:        "false",
		SettingKeyAdminChannelManagementEnabled: "false",
	}}, &config.Config{})

	raw, err := svc.GetPublicSettingsForInjection(context.Background())
	require.NoError(t, err)
	payload, ok := raw.(*PublicSettingsInjectionPayload)
	require.True(t, ok)
	require.False(t, payload.UserChannelStatusEnabled)
	require.False(t, payload.UserSubscriptionsEnabled)
	require.False(t, payload.AdminSubscriptionsEnabled)
	require.False(t, payload.AdminPromoCodesEnabled)
	require.False(t, payload.AdminChannelManagementEnabled)
}
