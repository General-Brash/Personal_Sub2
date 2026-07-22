package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type mallSettingRepoStub struct {
	values  map[string]string
	updates map[string]string
}

func (r *mallSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (r *mallSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *mallSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *mallSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (r *mallSettingRepoStub) SetMultiple(_ context.Context, values map[string]string) error {
	r.updates = values
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func (r *mallSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}

func (r *mallSettingRepoStub) Delete(context.Context, string) error { return nil }

func TestMallSettingIsIndependentFromPaymentAndDefaultsClosed(t *testing.T) {
	repo := &mallSettingRepoStub{values: map[string]string{SettingPaymentEnabled: "true"}}
	svc := NewSettingService(repo, &config.Config{})

	settings, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.PaymentEnabled)
	require.False(t, settings.MallEnabled)
	require.False(t, svc.IsMallEnabled(context.Background()))

	repo.values[SettingKeyMallEnabled] = "true"
	settings, err = svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.MallEnabled)
}

func TestUpdateSettingsPersistsMallSwitch(t *testing.T) {
	repo := &mallSettingRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})

	require.NoError(t, svc.UpdateSettings(context.Background(), &SystemSettings{MallEnabled: true}))
	require.Equal(t, "true", repo.updates[SettingKeyMallEnabled])
}
