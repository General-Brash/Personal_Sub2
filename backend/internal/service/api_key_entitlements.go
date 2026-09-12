package service

import (
	"context"
	"fmt"
)

type APIKeyEntitlementResolver interface {
	Resolve(context.Context, int64) (*EntitlementSnapshot, error)
}

func (s *APIKeyService) SetEntitlementResolver(resolver APIKeyEntitlementResolver) {
	s.entitlements = resolver
}

// Resolve after the credential cache, never into it. A tier revocation applies
// to the next request using an old key without waiting for the auth-cache TTL.
func (s *APIKeyService) applyLiveEntitlements(ctx context.Context, key *APIKey) (*APIKey, error) {
	if key == nil || key.User == nil || s.entitlements == nil {
		return key, nil
	}
	snapshot, err := s.entitlements.Resolve(ctx, key.UserID)
	if err != nil {
		return nil, fmt.Errorf("resolve live API key entitlements: %w", err)
	}
	if snapshot == nil {
		return nil, fmt.Errorf("live API key entitlements unavailable")
	}
	copyKey := *key
	copyUser := *key.User
	// This is the authoritative merged list, not a union with a stale cache.
	copyUser.AllowedGroups = append([]int64(nil), snapshot.AllowedGroups...)
	copyUser.EntitlementTier = snapshot.Tier
	copyUser.EntitlementVersion = snapshot.Version
	copyKey.User = &copyUser
	return &copyKey, nil
}

func (s *APIKeyService) liveEntitlementUser(ctx context.Context, user *User) (*User, error) {
	if user == nil || s.entitlements == nil {
		return user, nil
	}
	key, err := s.applyLiveEntitlements(ctx, &APIKey{UserID: user.ID, User: user})
	if err != nil {
		return nil, err
	}
	return key.User, nil
}

func (s *APIKeyService) RevalidateForTurn(ctx context.Context, previous *APIKey) (*APIKey, error) {
	if s == nil || s.apiKeyRepo == nil || previous == nil {
		return nil, ErrAPIKeyNotFound
	}
	current, err := s.apiKeyRepo.GetByID(ctx, previous.ID)
	if err != nil {
		return nil, err
	}
	if current == nil || current.UserID != previous.UserID || !current.IsActive() || current.IsExpired() || current.IsQuotaExhausted() {
		return nil, ErrAPIKeyNotFound
	}
	if (current.GroupID == nil) != (previous.GroupID == nil) || (current.GroupID != nil && *current.GroupID != *previous.GroupID) {
		return nil, ErrAPIKeyNotFound
	}
	current, err = s.applyLiveEntitlements(ctx, current)
	if err != nil {
		return nil, err
	}
	if current.User == nil || !current.User.IsActive() {
		return nil, ErrUserNotActive
	}
	if current.GroupID != nil && (current.Group == nil || !current.Group.IsActive() || !s.canUserBindGroup(ctx, current.User, current.Group)) {
		return nil, ErrAPIKeyNotFound
	}
	return current, nil
}
