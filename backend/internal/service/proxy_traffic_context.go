package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxytraffic"
)

func withAccountProxyTraffic(ctx context.Context, account *Account, raw string) context.Context {
	if account == nil || raw == "" {
		return ctx
	}
	ctx = proxytraffic.WithAccountID(ctx, account.ID)
	if account.ProxyID != nil && account.Proxy != nil && proxytraffic.Key(account.Proxy.URL()) == proxytraffic.Key(raw) {
		ctx = proxytraffic.WithProxyID(ctx, *account.ProxyID, raw)
	}
	return ctx
}
