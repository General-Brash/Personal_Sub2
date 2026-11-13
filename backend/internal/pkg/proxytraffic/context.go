package proxytraffic

import "context"

type routeContextKey struct{}

type routeIdentity struct {
	accountID int64
	proxyID   int64
	route     string
}

func WithAccountID(ctx context.Context, accountID int64) context.Context {
	identity, _ := ctx.Value(routeContextKey{}).(routeIdentity)
	identity.accountID = accountID
	return context.WithValue(ctx, routeContextKey{}, identity)
}

func WithProxyID(ctx context.Context, proxyID int64, raw string) context.Context {
	identity, _ := ctx.Value(routeContextKey{}).(routeIdentity)
	identity.proxyID = proxyID
	identity.route = Key(raw)
	return context.WithValue(ctx, routeContextKey{}, identity)
}

func AccountID(ctx context.Context) int64 {
	identity, _ := ctx.Value(routeContextKey{}).(routeIdentity)
	return identity.accountID
}

func ProxyID(ctx context.Context, raw string) int64 {
	identity, _ := ctx.Value(routeContextKey{}).(routeIdentity)
	if identity.route != "" && identity.route == Key(raw) {
		return identity.proxyID
	}
	return 0
}
