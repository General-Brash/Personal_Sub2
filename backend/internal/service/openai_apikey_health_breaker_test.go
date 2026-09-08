package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type openAIAPIKeyHealthSettingRepo struct {
	SettingRepository
	value    string
	getCalls int
	setCalls int
	setKey   string
}

func (r *openAIAPIKeyHealthSettingRepo) GetValue(context.Context, string) (string, error) {
	r.getCalls++
	return r.value, nil
}

func (r *openAIAPIKeyHealthSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (r *openAIAPIKeyHealthSettingRepo) Set(_ context.Context, key, value string) error {
	r.setCalls++
	r.setKey = key
	r.value = value
	return nil
}

type openAIAPIKeyHealthAccountRepo struct {
	AccountRepository
	setCalls  int
	accountID int64
	until     time.Time
	reason    string
}

func (r *openAIAPIKeyHealthAccountRepo) SetTempUnschedulable(_ context.Context, accountID int64, until time.Time, reason string) error {
	r.setCalls++
	r.accountID = accountID
	r.until = until
	r.reason = reason
	return nil
}

type openAIAPIKeyHealthCacheStub struct {
	TempUnschedCache
	recordCalls   int
	setCalls      int
	lastAccountID int64
	lastWindow    int
	lastThreshold int
	tripAt        int64
	recordErr     error
}

func (c *openAIAPIKeyHealthCacheStub) RecordOpenAIAPIKeyHealthFailure(_ context.Context, accountID int64, windowMinutes, threshold int) (int64, bool, error) {
	c.recordCalls++
	c.lastAccountID = accountID
	c.lastWindow = windowMinutes
	c.lastThreshold = threshold
	count := int64(c.recordCalls)
	return count, c.tripAt > 0 && count == c.tripAt, c.recordErr
}

func (c *openAIAPIKeyHealthCacheStub) SetTempUnsched(context.Context, int64, *TempUnschedState) error {
	c.setCalls++
	return nil
}

type openAIAPIKeyHealthRuntimeBlocker struct{ calls int }

func (b *openAIAPIKeyHealthRuntimeBlocker) BlockAccountScheduling(*Account, time.Time, string) {
	b.calls++
}
func (*openAIAPIKeyHealthRuntimeBlocker) ClearAccountSchedulingBlock(int64) {}

func openAIHealthPoolAccount() *Account {
	return &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}
}

func openAIHealthEnabledSettings(t *testing.T, windowMinutes, threshold, cooldownMinutes int) *SettingService {
	t.Helper()
	encoded, err := json.Marshal(OpenAIAPIKeyHealthBreakerSettings{
		Enabled:          true,
		WindowMinutes:    windowMinutes,
		FailureThreshold: threshold,
		CooldownMinutes:  cooldownMinutes,
	})
	require.NoError(t, err)
	return NewSettingService(&openAIAPIKeyHealthSettingRepo{value: string(encoded)}, &config.Config{})
}

func TestClassifyOpenAIAPIKeyHealthFailureExclusions(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		eligible bool
	}{
		{name: "account attributed 429", err: &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests, Scope: GatewayFailureScopeAccount}, eligible: true},
		{name: "legacy attributed 502", err: &UpstreamFailoverError{StatusCode: http.StatusBadGateway}, eligible: true},
		{name: "image attributed 503", err: &OpenAIImagesUpstreamError{StatusCode: http.StatusServiceUnavailable, Message: "upstream unavailable"}, eligible: true},
		{name: "request scoped capacity", err: &UpstreamFailoverError{StatusCode: 529, RequestScopedTransient: true}},
		{name: "request scoped failure", err: &UpstreamFailoverError{StatusCode: http.StatusBadGateway, Scope: GatewayFailureScopeRequest}},
		{name: "provider scoped overload", err: &UpstreamFailoverError{StatusCode: 529, Scope: GatewayFailureScopeProvider}},
		{name: "dedicated same account retry", err: &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests, RetryableOnSameAccount: true}},
		{name: "credential disable path", err: &UpstreamFailoverError{StatusCode: http.StatusUnauthorized, Stage: GatewayFailureStageAccountAuth, Scope: GatewayFailureScopeAccount}},
		{name: "client request", err: &UpstreamFailoverError{StatusCode: http.StatusBadRequest}},
		{name: "canceled", err: context.Canceled},
		{name: "wrapped deadline", err: errors.Join(errors.New("upstream stopped"), context.DeadlineExceeded)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, eligible := classifyOpenAIAPIKeyHealthFailure(tt.err)
			require.Equal(t, tt.eligible, eligible)
		})
	}
}

func TestOpenAIAPIKeyHealthBreakerAccountScopeExclusions(t *testing.T) {
	settings := openAIHealthEnabledSettings(t, 2, 3, 5)
	tests := []struct {
		name    string
		account *Account
	}{
		{name: "nil account"},
		{name: "oauth account", account: &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"pool_mode": true}}},
		{name: "non pool api key", account: &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}},
		{name: "other platform pool", account: &Account{ID: 3, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"pool_mode": true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &openAIAPIKeyHealthCacheStub{tripAt: 1}
			svc := NewRateLimitService(&openAIAPIKeyHealthAccountRepo{}, nil, &config.Config{}, nil, cache)
			svc.SetSettingService(settings)
			svc.SetOpenAIAPIKeyHealthCache(cache)

			require.False(t, svc.ObserveOpenAIAPIKeyHealthFailure(context.Background(), tt.account, &UpstreamFailoverError{StatusCode: http.StatusBadGateway}))
			require.Zero(t, cache.recordCalls)
		})
	}
}

func TestOpenAIAPIKeyHealthBreakerDefaultDisabled(t *testing.T) {
	settingRepo := &openAIAPIKeyHealthSettingRepo{}
	settings := NewSettingService(settingRepo, &config.Config{})
	cache := &openAIAPIKeyHealthCacheStub{tripAt: 1}
	svc := NewRateLimitService(&openAIAPIKeyHealthAccountRepo{}, nil, &config.Config{}, nil, cache)
	svc.SetSettingService(settings)
	svc.SetOpenAIAPIKeyHealthCache(cache)

	require.False(t, svc.ObserveOpenAIAPIKeyHealthFailure(context.Background(), openAIHealthPoolAccount(), &UpstreamFailoverError{StatusCode: http.StatusBadGateway}))
	require.Zero(t, cache.recordCalls)
	require.Equal(t, 1, settingRepo.getCalls)
}

func TestOpenAIAPIKeyHealthBreakerTripsAtConfiguredThreshold(t *testing.T) {
	settings := openAIHealthEnabledSettings(t, 2, 3, 5)
	cache := &openAIAPIKeyHealthCacheStub{tripAt: 3}
	repo := &openAIAPIKeyHealthAccountRepo{}
	blocker := &openAIAPIKeyHealthRuntimeBlocker{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)
	svc.SetSettingService(settings)
	svc.SetOpenAIAPIKeyHealthCache(cache)
	svc.SetAccountRuntimeBlocker(blocker)
	account := openAIHealthPoolAccount()
	upstreamErr := &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: []byte(`{"error":"upstream"}`)}
	startedAt := time.Now()

	require.False(t, svc.ObserveOpenAIAPIKeyHealthFailure(context.Background(), account, upstreamErr))
	require.False(t, svc.ObserveOpenAIAPIKeyHealthFailure(context.Background(), account, upstreamErr))
	require.Zero(t, repo.setCalls)
	require.True(t, svc.ObserveOpenAIAPIKeyHealthFailure(context.Background(), account, upstreamErr))

	require.Equal(t, 3, cache.recordCalls)
	require.Equal(t, account.ID, cache.lastAccountID)
	require.Equal(t, 2, cache.lastWindow)
	require.Equal(t, 3, cache.lastThreshold)
	require.Equal(t, 1, cache.setCalls)
	require.Equal(t, 1, repo.setCalls)
	require.Equal(t, account.ID, repo.accountID)
	require.WithinDuration(t, startedAt.Add(5*time.Minute), repo.until, 2*time.Second)
	require.Equal(t, 1, blocker.calls)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Contains(t, repo.reason, openAIAPIKeyHealthBreakerReason)
	require.Contains(t, repo.reason, `"trigger_count":3`)
	require.Contains(t, repo.reason, `"trigger_threshold":3`)
}

func TestOpenAIAPIKeyHealthAccountAwareEntryPointCountsOnceAndPreservesLegacyPath(t *testing.T) {
	settings := openAIHealthEnabledSettings(t, 2, 10, 5)
	cache := &openAIAPIKeyHealthCacheStub{}
	rateLimit := NewRateLimitService(&openAIAPIKeyHealthAccountRepo{}, nil, &config.Config{}, nil, cache)
	rateLimit.SetSettingService(settings)
	rateLimit.SetOpenAIAPIKeyHealthCache(cache)
	gateway := &OpenAIGatewayService{rateLimitService: rateLimit}
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	account := openAIHealthPoolAccount()
	observedErr := &UpstreamFailoverError{StatusCode: http.StatusBadGateway}

	require.False(t, gateway.ReportOpenAIAccountScheduleResultForAccount(account, "gpt-test", false, nil, observedErr))
	require.Equal(t, 1, cache.recordCalls)

	gateway.ReportOpenAIAccountScheduleResult(account.ID, "gpt-test", false, nil)
	require.Equal(t, 1, cache.recordCalls, "legacy ID-only scheduler reporting must not count breaker failures")

	require.False(t, gateway.ReportOpenAIAccountScheduleResultForAccount(account, "gpt-test", false, nil, nil))
	require.Equal(t, 1, cache.recordCalls, "an account without an attributable error must not count")

	require.False(t, gateway.ReportOpenAIAccountScheduleResultForAccount(account, "gpt-test", true, nil, nil))
	require.Equal(t, 1, cache.recordCalls, "success notification must not count as a failure")
}

func TestProvideRateLimitServiceWiresOpenAIAPIKeyHealthCacheExtension(t *testing.T) {
	cache := &openAIAPIKeyHealthCacheStub{}
	settings := NewSettingService(&openAIAPIKeyHealthSettingRepo{}, &config.Config{})

	svc := ProvideRateLimitService(
		&openAIAPIKeyHealthAccountRepo{},
		nil,
		&config.Config{},
		nil,
		cache,
		nil,
		nil,
		settings,
		nil,
	)

	require.Same(t, cache, svc.openAIAPIKeyHealth)
}

func TestNormalizeOpenAIAPIKeyHealthBreakerSettingsKeepsDefaultOffAndBounds(t *testing.T) {
	defaults := normalizeOpenAIAPIKeyHealthBreakerSettings(nil)
	require.False(t, defaults.Enabled)
	require.Equal(t, 2, defaults.WindowMinutes)
	require.Equal(t, 10, defaults.FailureThreshold)
	require.Equal(t, 5, defaults.CooldownMinutes)

	normalized := normalizeOpenAIAPIKeyHealthBreakerSettings(&OpenAIAPIKeyHealthBreakerSettings{
		Enabled:          true,
		WindowMinutes:    0,
		FailureThreshold: 10001,
		CooldownMinutes:  61,
	})
	require.True(t, normalized.Enabled)
	require.Equal(t, 1, normalized.WindowMinutes)
	require.Equal(t, 10000, normalized.FailureThreshold)
	require.Equal(t, 60, normalized.CooldownMinutes)
}
func TestSetOpenAIAPIKeyHealthBreakerSettingsRefreshesCachedRuntimeConfig(t *testing.T) {
	repo := &openAIAPIKeyHealthSettingRepo{}
	svc := NewSettingService(repo, &config.Config{})

	initial, err := svc.GetOpenAIAPIKeyHealthBreakerSettings(context.Background())
	require.NoError(t, err)
	require.False(t, initial.Enabled)
	require.Equal(t, 1, repo.getCalls)

	err = svc.SetOpenAIAPIKeyHealthBreakerSettings(context.Background(), &OpenAIAPIKeyHealthBreakerSettings{
		Enabled:          true,
		WindowMinutes:    3,
		FailureThreshold: 7,
		CooldownMinutes:  11,
	})
	require.NoError(t, err)
	require.Equal(t, 1, repo.setCalls)
	require.Equal(t, SettingKeyOpenAIAPIKeyHealthBreakerSettings, repo.setKey)

	updated, err := svc.GetOpenAIAPIKeyHealthBreakerSettings(context.Background())
	require.NoError(t, err)
	require.True(t, updated.Enabled)
	require.Equal(t, 3, updated.WindowMinutes)
	require.Equal(t, 7, updated.FailureThreshold)
	require.Equal(t, 11, updated.CooldownMinutes)
	require.Equal(t, 1, repo.getCalls, "setter must refresh the process cache without a stale repository read")
}

func TestOpenAIAPIKeyHealthAccountAwareEntryPointAttributionBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		success     bool
		observedErr error
		wantCalls   int
	}{
		{name: "success", success: true, wantCalls: 0},
		{name: "account-attributable 429", observedErr: &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests, Scope: GatewayFailureScopeAccount}, wantCalls: 1},
		{name: "account-attributable 5xx", observedErr: &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, Scope: GatewayFailureScopeAccount}, wantCalls: 1},
		{name: "canceled", observedErr: context.Canceled, wantCalls: 0},
		{name: "request-scoped error", observedErr: &UpstreamFailoverError{StatusCode: http.StatusBadGateway, Scope: GatewayFailureScopeRequest}, wantCalls: 0},
		{name: "client request error", observedErr: &UpstreamFailoverError{StatusCode: http.StatusBadRequest, Scope: GatewayFailureScopeAccount}, wantCalls: 0},
		{name: "local transport error", observedErr: errors.New("mock local transport failure"), wantCalls: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := openAIHealthEnabledSettings(t, 2, 10, 5)
			cache := &openAIAPIKeyHealthCacheStub{}
			rateLimit := NewRateLimitService(&openAIAPIKeyHealthAccountRepo{}, nil, &config.Config{}, nil, cache)
			rateLimit.SetSettingService(settings)
			rateLimit.SetOpenAIAPIKeyHealthCache(cache)
			gateway := &OpenAIGatewayService{rateLimitService: rateLimit}

			require.False(t, gateway.ReportOpenAIAccountScheduleResultForAccount(openAIHealthPoolAccount(), "gpt-test", tt.success, nil, tt.observedErr))
			require.Equal(t, tt.wantCalls, cache.recordCalls, "the account-aware public entry point used by OpenAI handlers must attribute one eligible upstream failure at most once")
		})
	}
}
