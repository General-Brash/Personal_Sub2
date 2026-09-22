package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const oidcSSOCodeConsumedPrefix = "oidc:sso:consumed:"

type oidcSSOCodeCache struct {
	rdb *redis.Client
}

// NewOIDCSSOCodeCache 提供跨域 SSO 一次性凭证的消费标记（Redis 实现）。
func NewOIDCSSOCodeCache(rdb *redis.Client) service.OIDCSSOCodeCache {
	return &oidcSSOCodeCache{rdb: rdb}
}

// ConsumeOnce 用 Redis SETNX 标记 fingerprint 已消费：首次返回 true，重复返回 false。
func (c *oidcSSOCodeCache) ConsumeOnce(ctx context.Context, fingerprint string, ttl time.Duration) (bool, error) {
	return c.rdb.SetNX(ctx, oidcSSOCodeConsumedPrefix+fingerprint, "1", ttl).Result()
}
