package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const ssoTestEncryptionKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// fakeSSOCodeCache 是 OIDCSSOCodeCache 的内存实现，用于隔离测试一次性消费语义，
// 避免 service 层测试直接依赖 redis。
type fakeSSOCodeCache struct {
	consumed map[string]bool
}

func newFakeSSOCodeCache() *fakeSSOCodeCache {
	return &fakeSSOCodeCache{consumed: map[string]bool{}}
}

func (c *fakeSSOCodeCache) ConsumeOnce(_ context.Context, fingerprint string, _ time.Duration) (bool, error) {
	if c.consumed[fingerprint] {
		return false, nil
	}
	c.consumed[fingerprint] = true
	return true, nil
}

// precheckUserRepo 是 LoginPrecheck 测试用的最小 UserRepository：按邮箱返回预置用户。
type precheckUserRepo struct {
	UserRepository
	byEmail map[string]*User
}

func (r *precheckUserRepo) GetByEmail(_ context.Context, email string) (*User, error) {
	if u, ok := r.byEmail[email]; ok {
		return u, nil
	}
	return nil, ErrUserNotFound
}

func TestLoginPrecheck_ConsistentResponses(t *testing.T) {
	repo := &precheckUserRepo{byEmail: map[string]*User{
		"no2fa@example.com":     {ID: 1, Email: "no2fa@example.com", Status: StatusActive, TotpEnabled: false},
		"has2fa@example.com":    {ID: 2, Email: "has2fa@example.com", Status: StatusActive, TotpEnabled: true},
		"banned2fa@example.com": {ID: 3, Email: "banned2fa@example.com", Status: "banned", TotpEnabled: true},
	}}
	svc := &OIDCProviderService{users: repo, loginGuard: newOIDCLoginAttemptGuard()}

	cases := []struct {
		email string
		want  bool
	}{
		{"notfound@example.com", false},  // 不存在 → 与"未开 2FA"一致，不泄露账号存在性
		{"no2fa@example.com", false},     // 存在但未开 2FA
		{"has2fa@example.com", true},     // 存在且开了 2FA
		{"banned2fa@example.com", false}, // 非激活用户即便开了 2FA 也不暴露
	}
	for _, tc := range cases {
		if got := svc.LoginPrecheck(context.Background(), tc.email, "ip|"+tc.email); got != tc.want {
			t.Fatalf("LoginPrecheck(%q) = %v, want %v", tc.email, got, tc.want)
		}
	}
}

func newSSOTestService(t *testing.T, cache OIDCSSOCodeCache) *OIDCProviderService {
	t.Helper()
	cfg := &config.Config{OIDCProvider: config.OIDCProviderConfig{
		Enabled:       true,
		EncryptionKey: ssoTestEncryptionKey,
		SecretPepper:  "runtime-secret-pepper-abcdefghijklmnopqrstuvwxyz",
	}}
	svc := NewOIDCProviderService(nil, nil, nil, nil, cfg, cache, nil)
	if svc.protector == nil {
		t.Fatal("protector must be initialised for SSO tests")
	}
	return svc
}

// craftSSOCode 复刻 IssueSSOCode 的载荷格式，直接用 protector 封装，便于隔离测试 RedeemSSOCode。
func craftSSOCode(t *testing.T, svc *OIDCProviderService, userID int64, handle string, expUnix int64) string {
	t.Helper()
	payload := strconv.FormatInt(userID, 10) + "|" + oidcDigest(handle) + "|" + strconv.FormatInt(expUnix, 10)
	code, _, err := svc.protector.seal(payload, "sso-code", oidcDigest(handle))
	if err != nil {
		t.Fatalf("seal sso code: %v", err)
	}
	return code
}

func TestRedeemSSOCode_ValidRoundTrip(t *testing.T) {
	svc := newSSOTestService(t, nil)
	const handle = "tx-handle-abc"
	code := craftSSOCode(t, svc, 77, handle, time.Now().UTC().Add(30*time.Second).Unix())

	userID, err := svc.RedeemSSOCode(context.Background(), code, handle)
	if err != nil {
		t.Fatalf("valid code must redeem: %v", err)
	}
	if userID != 77 {
		t.Fatalf("redeemed user id = %d, want 77", userID)
	}
}

func TestRedeemSSOCode_WrongTransactionRejected(t *testing.T) {
	svc := newSSOTestService(t, nil)
	code := craftSSOCode(t, svc, 77, "tx-handle-abc", time.Now().UTC().Add(30*time.Second).Unix())

	// 用不同的事务句柄核销：AAD 不匹配，open 直接失败。
	if _, err := svc.RedeemSSOCode(context.Background(), code, "tx-handle-different"); err == nil {
		t.Fatal("code bound to another transaction must be rejected")
	}
}

func TestRedeemSSOCode_ExpiredRejected(t *testing.T) {
	svc := newSSOTestService(t, nil)
	const handle = "tx-handle-abc"
	code := craftSSOCode(t, svc, 77, handle, time.Now().UTC().Add(-time.Second).Unix())

	if _, err := svc.RedeemSSOCode(context.Background(), code, handle); err == nil {
		t.Fatal("expired code must be rejected")
	}
}

func TestRedeemSSOCode_TamperedRejected(t *testing.T) {
	svc := newSSOTestService(t, nil)
	const handle = "tx-handle-abc"
	code := craftSSOCode(t, svc, 77, handle, time.Now().UTC().Add(30*time.Second).Unix())

	tampered := code + "x"
	if _, err := svc.RedeemSSOCode(context.Background(), tampered, handle); err == nil {
		t.Fatal("tampered ciphertext must be rejected")
	}
}

func TestRedeemSSOCode_OneTimeConsumption(t *testing.T) {
	svc := newSSOTestService(t, newFakeSSOCodeCache())
	const handle = "tx-handle-abc"
	code := craftSSOCode(t, svc, 77, handle, time.Now().UTC().Add(30*time.Second).Unix())

	if _, err := svc.RedeemSSOCode(context.Background(), code, handle); err != nil {
		t.Fatalf("first redemption must succeed: %v", err)
	}
	// 二次核销同一 code：缓存已标记消费，必须被拒绝（防重放）。
	if _, err := svc.RedeemSSOCode(context.Background(), code, handle); err == nil {
		t.Fatal("second redemption of the same code must be rejected")
	}
}
