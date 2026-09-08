package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAIAutoResetCreditExtra(t *testing.T) {
	t.Run("历史账号默认关闭", func(t *testing.T) {
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
		config := ResolveOpenAIAutoResetCreditConfig(account)
		require.False(t, config.Enabled)
		require.Equal(t, 1.0, config.Threshold5h)
		require.Equal(t, 1.0, config.Threshold7d)
	})

	t.Run("开启时补齐两个百分百阈值并剥离运行态", func(t *testing.T) {
		extra, err := normalizeOpenAIAutoResetCreditExtra(PlatformOpenAI, AccountTypeOAuth, false, map[string]any{
			OpenAIAutoResetCreditEnabledExtraKey: true,
			OpenAIAutoResetCreditStateExtraKey:   map[string]any{"status": "success"},
		})
		require.NoError(t, err)
		require.Equal(t, 1.0, extra[OpenAIAutoResetCredit5hThresholdExtraKey])
		require.Equal(t, 1.0, extra[OpenAIAutoResetCredit7dThresholdExtraKey])
		require.NotContains(t, extra, OpenAIAutoResetCreditStateExtraKey)
	})

	t.Run("阈值和账号类型严格校验", func(t *testing.T) {
		_, err := normalizeOpenAIAutoResetCreditExtra(PlatformOpenAI, AccountTypeOAuth, false, map[string]any{
			OpenAIAutoResetCreditEnabledExtraKey:     true,
			OpenAIAutoResetCredit5hThresholdExtraKey: 0.0009,
		})
		require.Error(t, err)

		_, err = normalizeOpenAIAutoResetCreditExtra(PlatformOpenAI, AccountTypeOAuth, true, map[string]any{
			OpenAIAutoResetCreditEnabledExtraKey: true,
		})
		require.Error(t, err)
	})
}

func TestShouldAutoPauseOpenAIAccountByQuota_AutoResetCreditStates(t *testing.T) {
	now := time.Now().UTC()
	baseExtra := map[string]any{
		OpenAIAutoResetCreditEnabledExtraKey:     true,
		OpenAIAutoResetCredit5hThresholdExtraKey: 1.0,
		OpenAIAutoResetCredit7dThresholdExtraKey: 1.0,
		"auto_pause_5h_threshold":                0.8,
		"auto_pause_7d_disabled":                 true,
		"codex_5h_used_percent":                  90.0,
		"codex_usage_updated_at":                 now.Format(time.RFC3339),
		"codex_5h_reset_at":                      now.Add(time.Hour).Format(time.RFC3339),
	}

	t.Run("卡状态未知时暂停并触发异步查询", func(t *testing.T) {
		account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: cloneOpenAIAutoResetExtra(baseExtra)}
		paused, decision := shouldAutoPauseOpenAIAccountByQuota(context.Background(), account)
		require.True(t, paused)
		require.Equal(t, "quota_auto_reset_credit_check_5h", decision.reason)
	})

	t.Run("明确有卡时允许继续到用卡阈值", func(t *testing.T) {
		extra := cloneOpenAIAutoResetExtra(baseExtra)
		extra[OpenAIAutoResetCreditStateExtraKey] = OpenAIAutoResetCreditState{
			Status: OpenAIAutoResetStatusAvailable, AvailableCount: 1, CheckedAt: now.Format(time.RFC3339),
		}
		account := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: extra}
		paused, _ := shouldAutoPauseOpenAIAccountByQuota(context.Background(), account)
		require.False(t, paused)
	})

	t.Run("达到用卡阈值后即使有卡也退出调度", func(t *testing.T) {
		extra := cloneOpenAIAutoResetExtra(baseExtra)
		extra["codex_5h_used_percent"] = 100.0
		extra[OpenAIAutoResetCreditStateExtraKey] = OpenAIAutoResetCreditState{
			Status: OpenAIAutoResetStatusAvailable, AvailableCount: 1, CheckedAt: now.Format(time.RFC3339),
		}
		account := &Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: extra}
		paused, decision := shouldAutoPauseOpenAIAccountByQuota(context.Background(), account)
		require.True(t, paused)
		require.Equal(t, "quota_auto_reset_pending_5h", decision.reason)
	})

	t.Run("自然窗口重置后清除动态阻塞", func(t *testing.T) {
		extra := cloneOpenAIAutoResetExtra(baseExtra)
		extra["codex_5h_used_percent"] = 100.0
		extra["codex_5h_reset_at"] = now.Add(-time.Second).Format(time.RFC3339)
		extra[OpenAIAutoResetCreditStateExtraKey] = OpenAIAutoResetCreditState{
			Status: OpenAIAutoResetStatusFailed, TriggerWindow: "5h", ErrorCode: "RESET_FAILED",
		}
		account := &Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: extra}
		paused, _ := shouldAutoPauseOpenAIAccountByQuota(context.Background(), account)
		require.False(t, paused)
	})
}

func TestSelectOpenAIAutoResetCandidate_FailsClosed(t *testing.T) {
	candidates := []openAIAutoResetCreditCandidate{
		{ID: "later", ExpiresAt: "2026-09-02T00:00:00Z"},
		{ID: "earlier", ExpiresAt: "2026-09-01T00:00:00Z"},
	}
	selected, err := selectOpenAIAutoResetCandidate(candidates, 2, nil, "cycle-a")
	require.NoError(t, err)
	require.Equal(t, "earlier", selected.ID)

	_, err = selectOpenAIAutoResetCandidate([]openAIAutoResetCreditCandidate{
		{ExpiresAt: "2026-09-01T00:00:00Z"},
	}, 1, nil, "cycle-a")
	require.Error(t, err)

	_, err = selectOpenAIAutoResetCandidate(candidates, 2, &OpenAIAutoResetCreditState{
		AttemptCycleHash: "cycle-a", AttemptCreditHash: shortOpenAIAutoResetHash("missing"),
	}, "cycle-a")
	require.Error(t, err, "模糊结果后原卡消失时不得切换下一张卡")
}

func TestOpenAIQuotaAutoResetService_AssessesIndependentWindows(t *testing.T) {
	service := &OpenAIQuotaAutoResetService{}
	account := &Account{Extra: map[string]any{
		"auto_pause_5h_disabled": true,
		"auto_pause_7d_disabled": true,
	}}
	config := OpenAIAutoResetCreditConfig{Enabled: true, Threshold5h: 0.8, Threshold7d: 0.9}
	tests := []struct {
		name       string
		fiveHour   float64
		sevenDay   float64
		wantWindow string
	}{
		{name: "5h", fiveHour: 0.8, sevenDay: 0.2, wantWindow: "5h"},
		{name: "7d", fiveHour: 0.2, sevenDay: 0.9, wantWindow: "7d"},
		{name: "同时触发", fiveHour: 0.95, sevenDay: 0.95, wantWindow: "5h+7d"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assessment := service.buildAssessment(account, config, test.fiveHour, test.sevenDay)
			require.True(t, assessment.resetReached)
			require.Equal(t, test.wantWindow, assessment.triggerWindow)
		})
	}
}

type autoResetTestAccountRepo struct {
	AccountRepository
	mu        sync.Mutex
	account   *Account
	accounts  []Account
	listCalls int
	listErr   error
}

func (r *autoResetTestAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *r.account
	copy.Extra = cloneOpenAIAutoResetExtra(r.account.Extra)
	return &copy, nil
}

func (r *autoResetTestAccountRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account.Extra == nil {
		r.account.Extra = make(map[string]any)
	}
	for key, value := range updates {
		r.account.Extra[key] = value
	}
	return nil
}

func (r *autoResetTestAccountRepo) ListWithFilters(_ context.Context, params pagination.PaginationParams, _, _, _, _ string, _ int64, _ string) ([]Account, *pagination.PaginationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCalls++
	if r.listErr != nil {
		return nil, nil, r.listErr
	}
	accounts := make([]Account, len(r.accounts))
	for i := range r.accounts {
		accounts[i] = r.accounts[i]
		accounts[i].Extra = cloneOpenAIAutoResetExtra(r.accounts[i].Extra)
	}
	return accounts, &pagination.PaginationResult{
		Total: int64(len(accounts)), Page: params.Page, PageSize: params.PageSize, Pages: 1,
	}, nil
}

type autoResetTestQuota struct {
	usage           *OpenAIQuotaUsage
	resetCalls      atomic.Int32
	queryCalls      atomic.Int32
	cacheResetCalls atomic.Int32
	cachePostCalls  atomic.Int32
	resetEntered    chan struct{}
	releaseReset    chan struct{}
	enterOnce       sync.Once
	mu              sync.Mutex
	resetArgs       [][2]string
	failFirst       bool
	queryErr        error
	cacheResetErr   error
	cachePostErr    error
	resetErr        error
}

func (q *autoResetTestQuota) QueryUsage(context.Context, int64) (*OpenAIQuotaUsage, error) {
	q.queryCalls.Add(1)
	if q.queryErr != nil {
		return nil, q.queryErr
	}
	if q.usage == nil {
		return nil, nil
	}
	copy := *q.usage
	return &copy, nil
}

func (q *autoResetTestQuota) CacheResetCreditsSnapshot(context.Context, int64, *OpenAIRateLimitResetCredits) error {
	q.cacheResetCalls.Add(1)
	return q.cacheResetErr
}

func (q *autoResetTestQuota) CachePostResetSnapshot(context.Context, int64, *OpenAIQuotaUsage) error {
	q.cachePostCalls.Add(1)
	return q.cachePostErr
}

func (q *autoResetTestQuota) ResetCreditTargeted(_ context.Context, _ int64, creditID, redeemRequestID string) (*OpenAIQuotaResetResult, error) {
	if creditID == "" || redeemRequestID == "" {
		panic("targeted reset identifiers must be present")
	}
	call := q.resetCalls.Add(1)
	q.mu.Lock()
	q.resetArgs = append(q.resetArgs, [2]string{creditID, redeemRequestID})
	q.mu.Unlock()
	if q.failFirst && call == 1 {
		return nil, context.DeadlineExceeded
	}
	if q.resetErr != nil {
		return nil, q.resetErr
	}
	if q.resetEntered != nil {
		q.enterOnce.Do(func() { close(q.resetEntered) })
	}
	if q.releaseReset != nil {
		<-q.releaseReset
	}
	return &OpenAIQuotaResetResult{Code: "ok", WindowsReset: 2}, nil
}

type autoResetTestRecoverer struct{}

func (autoResetTestRecoverer) RecoverAccountState(context.Context, int64, AccountRecoveryOptions) (*SuccessfulTestRecoveryResult, error) {
	return &SuccessfulTestRecoveryResult{ClearedRateLimit: true}, nil
}

func TestOpenAIQuotaAutoResetService_ConcurrentInstancesConsumeOnce(t *testing.T) {
	now := time.Now().UTC()
	account := &Account{
		ID: 99, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Extra: map[string]any{
			OpenAIAutoResetCreditEnabledExtraKey:     true,
			OpenAIAutoResetCredit5hThresholdExtraKey: 1.0,
			OpenAIAutoResetCredit7dThresholdExtraKey: 1.0,
			"codex_5h_used_percent":                  100.0,
			"codex_7d_used_percent":                  10.0,
			"codex_usage_updated_at":                 now.Format(time.RFC3339),
			"codex_5h_reset_at":                      now.Add(time.Hour).Format(time.RFC3339),
			"codex_7d_reset_at":                      now.Add(24 * time.Hour).Format(time.RFC3339),
		},
	}
	repo := &autoResetTestAccountRepo{account: account}
	usage := &OpenAIQuotaUsage{
		FetchedAt: now.Unix(),
		RateLimit: &OpenAIRateLimit{
			PrimaryWindow:   &OpenAIRateLimitWindow{UsedPercent: 100, LimitWindowSeconds: 5 * 60 * 60, ResetAfterSeconds: 3600, ResetAt: now.Add(time.Hour).Unix()},
			SecondaryWindow: &OpenAIRateLimitWindow{UsedPercent: 10, LimitWindowSeconds: 7 * 24 * 60 * 60, ResetAfterSeconds: 86400, ResetAt: now.Add(24 * time.Hour).Unix()},
		},
		RateLimitResetCredits: &OpenAIRateLimitResetCredits{
			AvailableCount: 1,
			Credits:        []OpenAIRateLimitResetCreditDetail{{ExpiresAt: now.Add(48 * time.Hour).Format(time.RFC3339)}},
		},
		autoResetCandidates: []openAIAutoResetCreditCandidate{{ID: "credit-sensitive-id", ExpiresAt: now.Add(48 * time.Hour).Format(time.RFC3339)}},
	}
	quota := &autoResetTestQuota{usage: usage, resetEntered: make(chan struct{}), releaseReset: make(chan struct{})}
	idempotencyRepo := newInMemoryIdempotencyRepo()
	config := DefaultIdempotencyConfig()
	config.ObserveOnly = false
	config.ProcessingTimeout = time.Second
	serviceA := NewOpenAIQuotaAutoResetService(repo, quota, autoResetTestRecoverer{}, NewIdempotencyCoordinator(idempotencyRepo, config), nil, nil, nil)
	serviceB := NewOpenAIQuotaAutoResetService(repo, quota, autoResetTestRecoverer{}, NewIdempotencyCoordinator(idempotencyRepo, config), nil, nil, nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = serviceA.evaluateAccount(context.Background(), account.ID)
	}()
	<-quota.resetEntered
	go func() {
		defer wg.Done()
		_ = serviceB.evaluateAccount(context.Background(), account.ID)
	}()
	time.Sleep(50 * time.Millisecond)
	close(quota.releaseReset)
	wg.Wait()

	require.Equal(t, int32(1), quota.resetCalls.Load())
	repo.mu.Lock()
	state := openAIAutoResetStateFromExtra(repo.account.Extra)
	repo.mu.Unlock()
	require.NotNil(t, state)
	require.Equal(t, OpenAIAutoResetStatusSuccess, state.Status)
	encodedState, err := json.Marshal(state)
	require.NoError(t, err)
	require.NotContains(t, string(encodedState), "credit-sensitive-id")
}

func TestOpenAIQuotaAutoResetService_TimeoutRetryReusesRequestBody(t *testing.T) {
	now := time.Now().UTC()
	account := &Account{
		ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true,
		Extra: map[string]any{
			OpenAIAutoResetCreditEnabledExtraKey:     true,
			OpenAIAutoResetCredit5hThresholdExtraKey: 1.0,
			OpenAIAutoResetCredit7dThresholdExtraKey: 1.0,
			"codex_5h_used_percent":                  100.0,
			"codex_usage_updated_at":                 now.Format(time.RFC3339),
			"codex_5h_reset_at":                      now.Add(time.Hour).Format(time.RFC3339),
		},
	}
	repo := &autoResetTestAccountRepo{account: account}
	expiresAt := now.Add(48 * time.Hour).Format(time.RFC3339)
	quota := &autoResetTestQuota{
		failFirst: true,
		usage: &OpenAIQuotaUsage{
			FetchedAt: now.Unix(),
			RateLimit: &OpenAIRateLimit{
				PrimaryWindow: &OpenAIRateLimitWindow{UsedPercent: 100, LimitWindowSeconds: 5 * 60 * 60, ResetAfterSeconds: 3600, ResetAt: now.Add(time.Hour).Unix()},
			},
			RateLimitResetCredits: &OpenAIRateLimitResetCredits{
				AvailableCount: 1,
				Credits:        []OpenAIRateLimitResetCreditDetail{{ExpiresAt: expiresAt}},
			},
			autoResetCandidates: []openAIAutoResetCreditCandidate{{ID: "retry-credit", ExpiresAt: expiresAt}},
		},
	}
	idempotencyConfig := DefaultIdempotencyConfig()
	idempotencyConfig.ObserveOnly = false
	idempotencyConfig.FailedRetryBackoff = 0
	service := NewOpenAIQuotaAutoResetService(
		repo,
		quota,
		autoResetTestRecoverer{},
		NewIdempotencyCoordinator(newInMemoryIdempotencyRepo(), idempotencyConfig),
		nil, nil, nil,
	)

	require.Error(t, service.evaluateAccount(context.Background(), account.ID))
	require.NoError(t, service.evaluateAccount(context.Background(), account.ID))
	quota.mu.Lock()
	args := append([][2]string(nil), quota.resetArgs...)
	quota.mu.Unlock()
	require.Len(t, args, 2)
	require.Equal(t, args[0], args[1], "超时重试必须复用相同 credit_id 与 redeem_request_id")
}

func TestResolveOpenAIAutoResetCreditConfigOnlyAllowsEnabledParentAccount(t *testing.T) {
	parent := &Account{
		ID: 501, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
			OpenAIAutoResetCreditEnabledExtraKey:     true,
			OpenAIAutoResetCredit5hThresholdExtraKey: 0.75,
			OpenAIAutoResetCredit7dThresholdExtraKey: 0.9,
		},
	}
	parentConfig := ResolveOpenAIAutoResetCreditConfig(parent)
	require.True(t, parentConfig.Enabled)
	require.Equal(t, 0.75, parentConfig.Threshold5h)
	require.Equal(t, 0.9, parentConfig.Threshold7d)

	disabled := &Account{
		ID: 502, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
			OpenAIAutoResetCreditEnabledExtraKey:     false,
			OpenAIAutoResetCredit5hThresholdExtraKey: 0.75,
			OpenAIAutoResetCredit7dThresholdExtraKey: 0.9,
		},
	}
	require.False(t, ResolveOpenAIAutoResetCreditConfig(disabled).Enabled)

	parentID := parent.ID
	shadow := &Account{
		ID: 503, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID, Extra: map[string]any{
			OpenAIAutoResetCreditEnabledExtraKey: true,
		},
	}
	require.False(t, ResolveOpenAIAutoResetCreditConfig(shadow).Enabled)
	_, err := normalizeOpenAIAutoResetCreditExtra(PlatformOpenAI, AccountTypeOAuth, true, map[string]any{
		OpenAIAutoResetCreditEnabledExtraKey: true,
	})
	require.Error(t, err)
}

func openAIQuotaAutoResetTestAccount(id int64, now time.Time) *Account {
	return &Account{
		ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Extra: map[string]any{
			OpenAIAutoResetCreditEnabledExtraKey:     true,
			OpenAIAutoResetCredit5hThresholdExtraKey: 1.0,
			OpenAIAutoResetCredit7dThresholdExtraKey: 1.0,
			"codex_5h_used_percent":                  100.0,
			"codex_7d_used_percent":                  10.0,
			"codex_usage_updated_at":                 now.Format(time.RFC3339),
			"codex_5h_reset_at":                      now.Add(time.Hour).Format(time.RFC3339),
			"codex_7d_reset_at":                      now.Add(24 * time.Hour).Format(time.RFC3339),
		},
	}

}

func openAIQuotaAutoResetTestUsage(now time.Time, creditID string) *OpenAIQuotaUsage {
	expiresAt := now.Add(48 * time.Hour).Format(time.RFC3339)
	return &OpenAIQuotaUsage{
		FetchedAt: now.Unix(),
		RateLimit: &OpenAIRateLimit{
			PrimaryWindow:   &OpenAIRateLimitWindow{UsedPercent: 100, LimitWindowSeconds: 5 * 60 * 60, ResetAfterSeconds: 3600, ResetAt: now.Add(time.Hour).Unix()},
			SecondaryWindow: &OpenAIRateLimitWindow{UsedPercent: 10, LimitWindowSeconds: 7 * 24 * 60 * 60, ResetAfterSeconds: 86400, ResetAt: now.Add(24 * time.Hour).Unix()},
		},
		RateLimitResetCredits: &OpenAIRateLimitResetCredits{
			AvailableCount: 1,
			Credits:        []OpenAIRateLimitResetCreditDetail{{ExpiresAt: expiresAt}},
		},
		autoResetCandidates: []openAIAutoResetCreditCandidate{{ID: creditID, ExpiresAt: expiresAt}},
	}

}

func TestParseOpenAIRateLimitResetCreditDetailsKeepsStableCandidateIDs(t *testing.T) {
	details, err := parseOpenAIRateLimitResetCreditDetails([]byte(`{"available_count":3,"credits":[{"id":"snake-id","expires_at":"2026-10-01T00:00:00Z"},{"creditId":"camel-id","expires_at":"2026-10-02T00:00:00Z"},{"credit_id":"fallback-id","expires_at":"2026-10-03T00:00:00Z"}]}`))
	require.NoError(t, err)
	require.Equal(t, []openAIAutoResetCreditCandidate{
		{ID: "snake-id", ExpiresAt: "2026-10-01T00:00:00Z"},
		{ID: "camel-id", ExpiresAt: "2026-10-02T00:00:00Z"},
		{ID: "fallback-id", ExpiresAt: "2026-10-03T00:00:00Z"},
	}, details.AutoResetCandidates)
}

func TestOpenAIQuotaAutoResetService_CycleIdempotencyKeepsOneStableConsume(t *testing.T) {
	now := time.Now().UTC()
	account := openAIQuotaAutoResetTestAccount(601, now)
	repo := &autoResetTestAccountRepo{account: account}
	quota := &autoResetTestQuota{usage: openAIQuotaAutoResetTestUsage(now, "cycle-credit")}
	idempotencyConfig := DefaultIdempotencyConfig()
	idempotencyConfig.ObserveOnly = false
	idempotencyConfig.FailedRetryBackoff = 0
	autoResetService := NewOpenAIQuotaAutoResetService(repo, quota, autoResetTestRecoverer{}, NewIdempotencyCoordinator(newInMemoryIdempotencyRepo(), idempotencyConfig), nil, nil, nil)

	require.NoError(t, autoResetService.evaluateAccount(context.Background(), account.ID))
	require.NoError(t, autoResetService.evaluateAccount(context.Background(), account.ID))
	require.Equal(t, int32(1), quota.resetCalls.Load(), "same quota cycle must consume only once")
	quota.mu.Lock()
	args := append([][2]string(nil), quota.resetArgs...)
	quota.mu.Unlock()
	require.Len(t, args, 1)
	require.NotEmpty(t, args[0][0])
	require.NotEmpty(t, args[0][1])
	repo.mu.Lock()
	state := openAIAutoResetStateFromExtra(repo.account.Extra)
	repo.mu.Unlock()
	require.NotNil(t, state)
	require.Equal(t, OpenAIAutoResetStatusSuccess, state.Status)
}

func TestOpenAIQuotaAutoResetService_StartupScanQueuesOnlyEnabledParents(t *testing.T) {
	parentID := int64(701)
	accounts := []Account{
		{ID: 701, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Extra: map[string]any{OpenAIAutoResetCreditEnabledExtraKey: true}},
		{ID: 702, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Extra: map[string]any{OpenAIAutoResetCreditEnabledExtraKey: false}},
		{ID: 703, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: false, Extra: map[string]any{OpenAIAutoResetCreditEnabledExtraKey: true}},
		{ID: 704, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID, Status: StatusActive, Schedulable: true, Extra: map[string]any{OpenAIAutoResetCreditEnabledExtraKey: true}},
		{ID: 705, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Extra: map[string]any{OpenAIAutoResetCreditEnabledExtraKey: true}},
	}
	repo := &autoResetTestAccountRepo{accounts: accounts}
	autoResetService := NewOpenAIQuotaAutoResetService(repo, nil, nil, nil, nil, nil, nil)

	autoResetService.scanEnabledAccounts(context.Background())
	repo.mu.Lock()
	listCalls := repo.listCalls
	repo.mu.Unlock()
	require.Equal(t, 1, listCalls)

	var queued []int64
	for {
		select {
		case id := <-autoResetService.queue:
			queued = append(queued, id)
		default:
			require.Equal(t, []int64{701}, queued)
			return
		}
	}
}

func TestOpenAIQuotaAutoResetService_FailureStateIsCompensatedOnNextEvaluation(t *testing.T) {
	now := time.Now().UTC()
	account := openAIQuotaAutoResetTestAccount(801, now)
	repo := &autoResetTestAccountRepo{account: account}
	quotaErr := errors.New("mock quota query unavailable")
	quota := &autoResetTestQuota{usage: openAIQuotaAutoResetTestUsage(now, "compensation-credit"), queryErr: quotaErr}
	idempotencyConfig := DefaultIdempotencyConfig()
	idempotencyConfig.ObserveOnly = false
	resetService := NewOpenAIQuotaAutoResetService(repo, quota, autoResetTestRecoverer{}, NewIdempotencyCoordinator(newInMemoryIdempotencyRepo(), idempotencyConfig), nil, nil, nil)

	require.ErrorIs(t, resetService.evaluateAccount(context.Background(), account.ID), quotaErr)
	repo.mu.Lock()
	failed := openAIAutoResetStateFromExtra(repo.account.Extra)
	repo.mu.Unlock()
	require.NotNil(t, failed)
	require.Equal(t, OpenAIAutoResetStatusFailed, failed.Status)
	require.Equal(t, "RESET_CREDIT_QUERY_FAILED", failed.ErrorCode)

	quota.queryErr = nil
	require.NoError(t, resetService.evaluateAccount(context.Background(), account.ID))
	repo.mu.Lock()
	recovered := openAIAutoResetStateFromExtra(repo.account.Extra)
	repo.mu.Unlock()
	require.NotNil(t, recovered)
	require.Equal(t, OpenAIAutoResetStatusSuccess, recovered.Status)
	require.Equal(t, int32(1), quota.resetCalls.Load())
}

type trackingAutoResetRecoverer struct {
	calls   int
	options []AccountRecoveryOptions
	err     error
}

func (r *trackingAutoResetRecoverer) RecoverAccountState(_ context.Context, _ int64, options AccountRecoveryOptions) (*SuccessfulTestRecoveryResult, error) {
	r.calls++
	r.options = append(r.options, options)
	if r.err != nil {
		return nil, r.err
	}
	return &SuccessfulTestRecoveryResult{ClearedRateLimit: true}, nil
}

func TestRunOpenAIQuotaResetPostProcessRecoversSchedulingAndRefreshesQuota(t *testing.T) {
	now := time.Now().UTC()
	quota := &autoResetTestQuota{usage: openAIQuotaAutoResetTestUsage(now, "post-credit")}
	recoverer := &trackingAutoResetRecoverer{}
	loaded := &Account{ID: 901, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	loadCalls := 0
	result := RunOpenAIQuotaResetPostProcess(context.Background(), loaded.ID, quota, recoverer, func(context.Context, int64) (*Account, error) {
		loadCalls++
		return loaded, nil
	})

	require.True(t, result.AccountStateRecovered)
	require.True(t, result.CacheRefreshed)
	require.Empty(t, result.WarningCode)
	require.Same(t, loaded, result.Account)
	require.Equal(t, 1, recoverer.calls)
	require.Len(t, recoverer.options, 1)
	require.True(t, recoverer.options[0].InvalidateToken)
	require.Equal(t, int32(1), quota.queryCalls.Load())
	require.Equal(t, int32(1), quota.cachePostCalls.Load())
	require.Equal(t, 1, loadCalls)
}

func TestRunOpenAIQuotaResetPostProcessReportsRecoveryFailure(t *testing.T) {
	quota := &autoResetTestQuota{usage: &OpenAIQuotaUsage{}}
	recoverErr := errors.New("mock recovery failed")
	recoverer := &trackingAutoResetRecoverer{err: recoverErr}
	result := RunOpenAIQuotaResetPostProcess(context.Background(), 902, quota, recoverer, nil)
	require.False(t, result.AccountStateRecovered)
	require.Equal(t, OpenAIQuotaResetWarningAccountRecoveryFailed, result.WarningCode)
	require.Zero(t, quota.queryCalls.Load(), "quota refresh must not run after recovery failure")
}
