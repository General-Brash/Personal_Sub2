package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	gocache "github.com/patrickmn/go-cache"
	"golang.org/x/sync/singleflight"
)

type PremiumGroupRateResolver interface {
	ResolveGroupRate(ctx context.Context, userID, groupID int64, userRate *float64, groupRate float64) (EffectiveGroupRate, error)
}

type userGroupRateResolver struct {
	repo            UserGroupRateRepository
	premiumResolver PremiumGroupRateResolver
	cache           *gocache.Cache
	cacheTTL        time.Duration
	sf              *singleflight.Group
	logComponent    string
}

func newUserGroupRateResolver(repo UserGroupRateRepository, cache *gocache.Cache, cacheTTL time.Duration, sf *singleflight.Group, logComponent string) *userGroupRateResolver {
	if cacheTTL <= 0 {
		cacheTTL = defaultUserGroupRateCacheTTL
	}
	if cache == nil {
		cache = gocache.New(cacheTTL, time.Minute)
	}
	if logComponent == "" {
		logComponent = "service.gateway"
	}
	if sf == nil {
		sf = &singleflight.Group{}
	}
	return &userGroupRateResolver{repo: repo, cache: cache, cacheTTL: cacheTTL, sf: sf, logComponent: logComponent}
}

func (r *userGroupRateResolver) SetPremiumResolver(resolver PremiumGroupRateResolver) {
	if r != nil {
		r.premiumResolver = resolver
	}
}

func (s *GatewayService) SetPremiumRateResolver(resolver PremiumGroupRateResolver) {
	if s == nil || resolver == nil {
		return
	}
	if s.userGroupRateResolver == nil {
		s.userGroupRateResolver = newUserGroupRateResolver(s.userGroupRateRepo, s.userGroupRateCache, resolveUserGroupRateCacheTTL(s.cfg), &s.userGroupRateSF, "service.gateway")
	}
	s.userGroupRateResolver.SetPremiumResolver(resolver)
}

func (s *OpenAIGatewayService) SetPremiumRateResolver(resolver PremiumGroupRateResolver) {
	if s == nil || resolver == nil {
		return
	}
	if s.userGroupRateResolver == nil {
		s.userGroupRateResolver = newUserGroupRateResolver(nil, nil, resolveUserGroupRateCacheTTL(s.cfg), nil, "service.openai_gateway")
	}
	s.userGroupRateResolver.SetPremiumResolver(resolver)
}

func (r *userGroupRateResolver) Resolve(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
	if r == nil || userID <= 0 || groupID <= 0 {
		return groupDefaultMultiplier
	}
	if r.premiumResolver != nil {
		return r.resolvePremium(ctx, userID, groupID, groupDefaultMultiplier)
	}
	key := fmt.Sprintf("%d:%d", userID, groupID)
	if r.cache != nil {
		if cached, ok := r.cache.Get(key); ok {
			if multiplier, castOK := cached.(float64); castOK {
				userGroupRateCacheHitTotal.Add(1)
				return multiplier
			}
		}
	}
	if r.repo == nil {
		return groupDefaultMultiplier
	}
	userGroupRateCacheMissTotal.Add(1)
	value, err, shared := r.sf.Do(key, func() (any, error) {
		if r.cache != nil {
			if cached, ok := r.cache.Get(key); ok {
				if multiplier, castOK := cached.(float64); castOK {
					userGroupRateCacheHitTotal.Add(1)
					return multiplier, nil
				}
			}
		}
		userGroupRateCacheLoadTotal.Add(1)
		userRate, repoErr := r.repo.GetByUserAndGroup(ctx, userID, groupID)
		if repoErr != nil {
			return nil, repoErr
		}
		multiplier := groupDefaultMultiplier
		if userRate != nil {
			multiplier = *userRate
		}
		if r.cache != nil {
			r.cache.Set(key, multiplier, r.cacheTTL)
		}
		return multiplier, nil
	})
	if shared {
		userGroupRateCacheSFSharedTotal.Add(1)
	}
	if err != nil {
		userGroupRateCacheFallbackTotal.Add(1)
		logger.LegacyPrintf(r.logComponent, "get user group rate failed, fallback to group default: user=%d group=%d err=%v", userID, groupID, err)
		return groupDefaultMultiplier
	}
	multiplier, ok := value.(float64)
	if !ok {
		userGroupRateCacheFallbackTotal.Add(1)
		return groupDefaultMultiplier
	}
	return multiplier
}

func (r *userGroupRateResolver) resolvePremium(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
	if r.repo == nil {
		effective, err := r.premiumResolver.ResolveGroupRate(ctx, userID, groupID, nil, groupDefaultMultiplier)
		if err != nil {
			return math.NaN()
		}
		return effective.Multiplier
	}
	userRate, err := r.repo.GetByUserAndGroup(ctx, userID, groupID)
	if err != nil {
		userGroupRateCacheFallbackTotal.Add(1)
		logger.LegacyPrintf(r.logComponent, "get user group rate failed, fallback to group default: user=%d group=%d err=%v", userID, groupID, err)
		return math.NaN()
	}
	if userRate != nil {
		return *userRate
	}
	effective, err := r.premiumResolver.ResolveGroupRate(ctx, userID, groupID, nil, groupDefaultMultiplier)
	if err != nil {
		userGroupRateCacheFallbackTotal.Add(1)
		logger.LegacyPrintf(r.logComponent, "resolve premium group rate failed, fallback to group default: user=%d group=%d err=%v", userID, groupID, err)
		return math.NaN()
	}
	if effective.Multiplier < 0 {
		return math.NaN()
	}
	return effective.Multiplier
}
