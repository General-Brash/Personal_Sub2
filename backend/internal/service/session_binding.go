package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ErrSessionBindingMismatch 会话绑定的 User-Agent 发生变化，会话已失效。
var ErrSessionBindingMismatch = infraerrors.Unauthorized("SESSION_BINDING_MISMATCH", "session network fingerprint changed, please login again")

// SessionBinding 会话指纹：客户端 IP 与 User-Agent。
// 会话绑定开启时，仅 User-Agent 变化会导致会话失效；IP 变化被忽略
// （避免移动网络 / 多出口 IP 频繁切换导致登录后立即掉线）。
// IP 字段仍保留，供审计等其它用途使用，不参与指纹哈希。
type SessionBinding struct {
	IP        string
	UserAgent string
}

// Hash 计算绑定指纹哈希（仅基于 User-Agent；IP 不参与，故 IP 变化不影响哈希）。
func (b *SessionBinding) Hash() string {
	if b == nil {
		return ""
	}
	ua := strings.TrimSpace(b.UserAgent)
	if ua == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(ua))
	return hex.EncodeToString(sum[:16])
}

type sessionBindingCtxKey struct{}

// WithSessionBinding 将会话指纹注入 context（由 HTTP 入口中间件调用）。
func WithSessionBinding(ctx context.Context, binding *SessionBinding) context.Context {
	if binding == nil {
		return ctx
	}
	return context.WithValue(ctx, sessionBindingCtxKey{}, binding)
}

// SessionBindingFromContext 从 context 提取会话指纹；不存在时返回 nil。
func SessionBindingFromContext(ctx context.Context) *SessionBinding {
	if ctx == nil {
		return nil
	}
	binding, _ := ctx.Value(sessionBindingCtxKey{}).(*SessionBinding)
	return binding
}

// sessionBindingHashFromContext 提取指纹哈希，缺失时返回空串。
func sessionBindingHashFromContext(ctx context.Context) string {
	return SessionBindingFromContext(ctx).Hash()
}
