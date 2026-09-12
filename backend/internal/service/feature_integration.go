package service

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"strconv"
	"strings"
	"time"
)

// The HTTP identity, including a scoped key, must survive into sensitive domain
// operations. Re-resolving only its user ID would discard key restrictions.
type adminPrincipalContextKey struct{}

func ContextWithAdminPrincipal(ctx context.Context, principal *AdminPrincipal) context.Context {
	return context.WithValue(ctx, adminPrincipalContextKey{}, principal)
}

func AdminPrincipalFromContext(ctx context.Context) (*AdminPrincipal, bool) {
	principal, ok := ctx.Value(adminPrincipalContextKey{}).(*AdminPrincipal)
	return principal, ok && principal != nil
}

func (s *AdminPermissionService) AuthorizeInvitation(ctx context.Context, actorID int64, scope string) error {
	principal, ok := AdminPrincipalFromContext(ctx)
	if !ok || principal.UserID != actorID || s == nil {
		return ErrInvitationPermissionDenied
	}
	allowed, err := s.CheckPermission(ctx, principal, scope, nil)
	if err != nil || !allowed {
		return ErrInvitationPermissionDenied
	}
	return nil
}

const (
	SettingKeyPlayerInvitationsEnabled   = "player_invitations_enabled"
	SettingKeyPlayerInvitationTTLSeconds = "player_invitation_reservation_ttl_seconds"
)

// No expiry duration is silently approved. Enabling player invitations requires
// an explicit positive duration; existing credentials retain their stored expiry.
func (s *SettingService) GetPlayerInvitationPolicy(ctx context.Context) (PlayerInvitationPolicy, error) {
	if s == nil || s.settingRepo == nil {
		return PlayerInvitationPolicy{}, ErrServiceUnavailable
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyPlayerInvitationsEnabled, SettingKeyPlayerInvitationTTLSeconds})
	if err != nil {
		return PlayerInvitationPolicy{}, err
	}
	policy := PlayerInvitationPolicy{}
	if raw := strings.TrimSpace(values[SettingKeyPlayerInvitationsEnabled]); raw != "" {
		policy.Enabled, err = strconv.ParseBool(raw)
		if err != nil {
			return policy, fmt.Errorf("invalid player invitation enabled policy: %w", err)
		}
	}
	if raw := strings.TrimSpace(values[SettingKeyPlayerInvitationTTLSeconds]); raw != "" {
		seconds, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || seconds < 0 || seconds > int64((1<<63-1)/time.Second) {
			return policy, fmt.Errorf("player invitation expiry must be a positive duration in seconds")
		}
		policy.ReservationTTL = time.Duration(seconds) * time.Second
	}
	if policy.Enabled && policy.ReservationTTL <= 0 {
		return policy, fmt.Errorf("player invitation expiry must be configured before enabling")
	}
	return policy, nil
}

func ProvideAdminPermissionService(repo AdminPermissionRepository) *AdminPermissionService {
	return NewAdminPermissionService(repo, AdminPermissionModeFromEnv())
}

func ProvidePlayerInvitationService(repo PlayerInvitationRepository, permissions *AdminPermissionService, settings *SettingService, cfg *config.Config) *PlayerInvitationService {
	svc := NewPlayerInvitationService(repo, permissions, settings)
	if cfg != nil {
		svc.SetSigningKey(cfg.JWT.Secret)
	}
	return svc
}
