package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type affiliateOutboxSettingRepoStub struct {
	values map[string]string
	err    error
}

func (r *affiliateOutboxSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}
func (r *affiliateOutboxSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}
func (r *affiliateOutboxSettingRepoStub) Set(context.Context, string, string) error { return nil }
func (r *affiliateOutboxSettingRepoStub) GetMultiple(_ context.Context, _ []string) (map[string]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.values, nil
}
func (r *affiliateOutboxSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}
func (r *affiliateOutboxSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return nil, nil
}
func (r *affiliateOutboxSettingRepoStub) Delete(context.Context, string) error { return nil }

func TestLoadAffiliateRebateRuntimeConfig_DistinguishesInfrastructureErrors(t *testing.T) {
	wantErr := errors.New("database unavailable")
	svc := NewSettingService(&affiliateOutboxSettingRepoStub{err: wantErr}, nil)
	_, err := svc.loadAffiliateRebateRuntimeConfig(context.Background(), AffiliateRebateSourceRedeem)
	require.ErrorIs(t, err, wantErr)
}

func TestLoadAffiliateRebateRuntimeConfig_InvalidValueIsRetriableError(t *testing.T) {
	svc := NewSettingService(&affiliateOutboxSettingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:           "true",
		SettingKeyAffiliateRebateRate:        "not-a-number",
		SettingKeyAffiliateRebateFreezeHours: "0",
	}}, nil)
	_, err := svc.loadAffiliateRebateRuntimeConfig(context.Background(), AffiliateRebateSourceRedeem)
	require.Error(t, err)
	require.Contains(t, err.Error(), SettingKeyAffiliateRebateRate)
}

func TestLoadAffiliateRebateRuntimeConfig_ExplicitDisabledDoesNotNeedOtherSettings(t *testing.T) {
	svc := NewSettingService(&affiliateOutboxSettingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled: "false",
	}}, nil)
	cfg, err := svc.loadAffiliateRebateRuntimeConfig(context.Background(), AffiliateRebateSourceRedeem)
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
}

func TestLoadAffiliateRebateRuntimeConfig_AdminRechargeRequiresItsOwnSwitch(t *testing.T) {
	svc := NewSettingService(&affiliateOutboxSettingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:              "true",
		SettingKeyAffiliateAdminRechargeEnabled: "false",
	}}, nil)
	cfg, err := svc.loadAffiliateRebateRuntimeConfig(context.Background(), AffiliateRebateSourceAdminRecharge)
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
}

func TestLoadAffiliateRebateRuntimeConfig_MissingValuesUseDocumentedDefaults(t *testing.T) {
	svc := NewSettingService(&affiliateOutboxSettingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled: "true",
	}}, nil)
	cfg, err := svc.loadAffiliateRebateRuntimeConfig(context.Background(), AffiliateRebateSourceRedeem)
	require.NoError(t, err)
	require.True(t, cfg.Enabled)
	require.Equal(t, AffiliateRebateRateDefault, cfg.RatePercent)
	require.Equal(t, AffiliateRebateFreezeHoursDefault, cfg.FreezeHours)
	require.Equal(t, AffiliateRebateDurationDaysDefault, cfg.DurationDays)
	require.Equal(t, AffiliateRebatePerInviteeCapDefault, cfg.PerInviteeCap)
}
