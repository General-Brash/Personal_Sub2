package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// errConsentIssuePath 是"已经走到签发授权码路径"的哨兵错误。
//
// 本测试只关心 finishAuthorization 是否要求用户交互确认，不复算真实授权码签发，
// 因此 stub 的 GetAuthorizationTransaction 直接返回该错误：拿到它说明跳过了授权页，
// 拿到 NeedsConsent=true 说明要求了交互确认。
var errConsentIssuePath = errors.New("reached authorization code issue path")

type consentModeRepo struct {
	OIDCProviderRepository
	consent         *OIDCConsentRecord
	setConsentCalls int
	createdConsents []OIDCConsentCreateInput
}

func (r *consentModeRepo) GetConsent(_ context.Context, _, _ int64, _ string, _ int64) (*OIDCConsentRecord, error) {
	if r.consent == nil {
		return nil, sql.ErrNoRows
	}
	return r.consent, nil
}

func (r *consentModeRepo) SetTransactionConsent(_ context.Context, _, _ int64) error {
	r.setConsentCalls++
	return nil
}

func (r *consentModeRepo) CreateConsent(_ context.Context, input OIDCConsentCreateInput) (int64, error) {
	r.createdConsents = append(r.createdConsents, input)
	return 900, nil
}

func (r *consentModeRepo) GetAuthorizationTransaction(_ context.Context, _ string, _ time.Time) (*OIDCTransactionRecord, error) {
	return nil, errConsentIssuePath
}

// consentModeSettings 是最小 SettingRepository：只回答授权页策略这一个键。
type consentModeSettings struct {
	SettingRepository
	value string
	err   error
}

func (s *consentModeSettings) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeyOIDCConsentPromptMode {
		return "", sql.ErrNoRows
	}
	if s.err != nil {
		return "", s.err
	}
	return s.value, nil
}

func newConsentModeFixture(repo OIDCProviderRepository, settings SettingRepository) (*OIDCProviderService, *OIDCClientRecord, *OIDCTransactionRecord, *OIDCUserRecord) {
	svc := &OIDCProviderService{repo: repo, settings: settings}
	client := &OIDCClientRecord{ID: 11, ClientID: "sub2-test-client", Enabled: true, PolicyVersion: 3}
	transaction := &OIDCTransactionRecord{ID: 21, ClientPK: 11, RedirectURI: "https://rp.example.com/cb", ScopeSnapshot: "openid profile"}
	user := &OIDCUserRecord{ID: 31, Subject: "subject-31", Status: StatusActive}
	return svc, client, transaction, user
}

func activeConsent() *OIDCConsentRecord {
	return &OIDCConsentRecord{ID: 41, Status: "active"}
}

// TestFinishAuthorization_AlwaysModeForcesConsentPage 覆盖站点策略 always 的核心保证：
// 既不复用历史同意，也不接受客户端的 trusted_skip_consent 预授权。
func TestFinishAuthorization_AlwaysModeForcesConsentPage(t *testing.T) {
	t.Run("已有 active consent 仍要求交互确认", func(t *testing.T) {
		repo := &consentModeRepo{consent: activeConsent()}
		svc, client, transaction, user := newConsentModeFixture(repo, &consentModeSettings{value: OIDCConsentPromptModeAlways})

		result, err := svc.finishAuthorization(context.Background(), "handle", client, transaction, user, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.NeedsConsent {
			t.Fatal("always 模式下已有 consent 也必须渲染授权页")
		}
		if repo.setConsentCalls != 0 {
			t.Fatalf("不应复用历史同意，SetTransactionConsent 调用了 %d 次", repo.setConsentCalls)
		}
	})

	t.Run("trusted_skip_consent 失效", func(t *testing.T) {
		repo := &consentModeRepo{}
		svc, client, transaction, user := newConsentModeFixture(repo, &consentModeSettings{value: OIDCConsentPromptModeAlways})
		client.TrustedSkipConsent = true

		result, err := svc.finishAuthorization(context.Background(), "handle", client, transaction, user, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.NeedsConsent {
			t.Fatal("always 模式下 trusted_skip_consent 必须让位给授权页")
		}
		if len(repo.createdConsents) != 0 {
			t.Fatalf("不应写入 admin_pre_authorized 同意，实际写入 %d 条", len(repo.createdConsents))
		}
	})
}

// TestFinishAuthorization_AlwaysModeKeepsSilentCheck 守护 prompt=none 的静默能力：
// prompt=none 是 RP 显式要求"绝不展示界面"，always 模式仍允许复用历史同意，
// 但不接受管理员预授权。
func TestFinishAuthorization_AlwaysModeKeepsSilentCheck(t *testing.T) {
	t.Run("prompt=none 且已有 consent 时静默放行", func(t *testing.T) {
		repo := &consentModeRepo{consent: activeConsent()}
		svc, client, transaction, user := newConsentModeFixture(repo, &consentModeSettings{value: OIDCConsentPromptModeAlways})

		_, err := svc.finishAuthorization(context.Background(), "handle", client, transaction, user, "none")
		if !errors.Is(err, errConsentIssuePath) {
			t.Fatalf("prompt=none + 已有 consent 应走签发路径，got err=%v", err)
		}
		if repo.setConsentCalls != 1 {
			t.Fatalf("应复用历史同意一次，实际 %d 次", repo.setConsentCalls)
		}
	})

	t.Run("prompt=none 且仅有 trusted 预授权时不静默放行", func(t *testing.T) {
		repo := &consentModeRepo{}
		svc, client, transaction, user := newConsentModeFixture(repo, &consentModeSettings{value: OIDCConsentPromptModeAlways})
		client.TrustedSkipConsent = true

		_, err := svc.finishAuthorization(context.Background(), "handle", client, transaction, user, "none")
		if errors.Is(err, errConsentIssuePath) {
			t.Fatal("always 模式下 trusted 预授权不得签发授权码")
		}
		if len(repo.createdConsents) != 0 {
			t.Fatalf("不应写入 admin_pre_authorized 同意，实际写入 %d 条", len(repo.createdConsents))
		}
	})
}

// TestFinishAuthorization_RememberModeKeepsLegacyBehaviour 保证 remember 模式完整回归历史行为。
func TestFinishAuthorization_RememberModeKeepsLegacyBehaviour(t *testing.T) {
	t.Run("复用已有 consent", func(t *testing.T) {
		repo := &consentModeRepo{consent: activeConsent()}
		svc, client, transaction, user := newConsentModeFixture(repo, &consentModeSettings{value: OIDCConsentPromptModeRemember})

		_, err := svc.finishAuthorization(context.Background(), "handle", client, transaction, user, "")
		if !errors.Is(err, errConsentIssuePath) {
			t.Fatalf("remember 模式应复用同意并走签发路径，got err=%v", err)
		}
		if repo.setConsentCalls != 1 {
			t.Fatalf("应复用历史同意一次，实际 %d 次", repo.setConsentCalls)
		}
	})

	t.Run("trusted_skip_consent 生效并记录管理员预授权", func(t *testing.T) {
		repo := &consentModeRepo{}
		svc, client, transaction, user := newConsentModeFixture(repo, &consentModeSettings{value: OIDCConsentPromptModeRemember})
		client.TrustedSkipConsent = true

		_, err := svc.finishAuthorization(context.Background(), "handle", client, transaction, user, "")
		if !errors.Is(err, errConsentIssuePath) {
			t.Fatalf("remember + trusted 应走签发路径，got err=%v", err)
		}
		if len(repo.createdConsents) != 1 {
			t.Fatalf("应写入 1 条同意，实际 %d 条", len(repo.createdConsents))
		}
		if got := repo.createdConsents[0].Source; got != "admin_pre_authorized" {
			t.Fatalf("同意来源应为 admin_pre_authorized，实际 %q", got)
		}
	})

	t.Run("prompt=consent 仍强制渲染授权页", func(t *testing.T) {
		repo := &consentModeRepo{consent: activeConsent()}
		svc, client, transaction, user := newConsentModeFixture(repo, &consentModeSettings{value: OIDCConsentPromptModeRemember})
		client.TrustedSkipConsent = true

		result, err := svc.finishAuthorization(context.Background(), "handle", client, transaction, user, "consent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.NeedsConsent {
			t.Fatal("prompt=consent 必须渲染授权页")
		}
		if repo.setConsentCalls != 0 || len(repo.createdConsents) != 0 {
			t.Fatal("prompt=consent 不应复用同意或写入预授权")
		}
	})
}

// TestConsentPromptMode_FailsClosed 守护"读不到配置时更严格"：授权页是安全面，
// 配置缺失、取值非法或读取报错都必须按 always 处理，不得退回静默授权。
func TestConsentPromptMode_FailsClosed(t *testing.T) {
	cases := []struct {
		name     string
		settings SettingRepository
		want     string
	}{
		{"未注入设置仓库", nil, OIDCConsentPromptModeAlways},
		{"读取报错", &consentModeSettings{err: errors.New("boom")}, OIDCConsentPromptModeAlways},
		{"空值", &consentModeSettings{value: ""}, OIDCConsentPromptModeAlways},
		{"非法值", &consentModeSettings{value: "whatever"}, OIDCConsentPromptModeAlways},
		{"合法 remember", &consentModeSettings{value: OIDCConsentPromptModeRemember}, OIDCConsentPromptModeRemember},
		{"大小写与空格容错", &consentModeSettings{value: "  Remember "}, OIDCConsentPromptModeRemember},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &OIDCProviderService{settings: tc.settings}
			if got := svc.consentPromptMode(context.Background()); got != tc.want {
				t.Fatalf("consentPromptMode() = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("读取失败时仍强制授权页", func(t *testing.T) {
		repo := &consentModeRepo{consent: activeConsent()}
		svc, client, transaction, user := newConsentModeFixture(repo, &consentModeSettings{err: errors.New("boom")})

		result, err := svc.finishAuthorization(context.Background(), "handle", client, transaction, user, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.NeedsConsent {
			t.Fatal("读不到策略时必须 fail closed 到 always")
		}
	})
}

func TestNormalizeOIDCConsentPromptMode(t *testing.T) {
	cases := map[string]string{
		"":          OIDCConsentPromptModeAlways,
		"always":    OIDCConsentPromptModeAlways,
		"ALWAYS":    OIDCConsentPromptModeAlways,
		"remember":  OIDCConsentPromptModeRemember,
		"REMEMBER ": OIDCConsentPromptModeRemember,
		"skip":      OIDCConsentPromptModeAlways,
		"true":      OIDCConsentPromptModeAlways,
	}
	for raw, want := range cases {
		if got := normalizeOIDCConsentPromptMode(raw); got != want {
			t.Fatalf("normalizeOIDCConsentPromptMode(%q) = %q, want %q", raw, got, want)
		}
	}
}
