package service

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"strings"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

type registrationInvitationSources struct {
	AdminCode          *RedeemCode
	PlayerToken        string
	AffiliateCode      string
	AffiliateInviterID *int64
}

// resolveRegistrationInvitationSources accepts either the legacy administrator
// invitation code, a player one-time token, or both only when they resolve to
// the same inviter. It performs no consumption.
func (s *AuthService) resolveRegistrationInvitationSources(ctx context.Context, invitationCode, affiliateCode string) (*registrationInvitationSources, error) {
	if s == nil {
		return nil, ErrServiceUnavailable
	}
	rawInvitation := strings.TrimSpace(invitationCode)
	out := &registrationInvitationSources{}
	adminGate := false
	if s.settingService != nil && s.settingService.IsInvitationCodeEnabled(ctx) {
		adminGate = true
		if rawInvitation != "" && s.redeemRepo != nil {
			if code, err := s.redeemRepo.GetByCode(ctx, rawInvitation); err == nil && code.Type == RedeemTypeInvitation && code.CanUse() {
				out.AdminCode = code
			} else if err != nil && !errors.Is(err, ErrRedeemCodeNotFound) {
				return nil, ErrServiceUnavailable
			}
		}
	}

	if out.AdminCode == nil && rawInvitation != "" && s.invitationService != nil {
		player, err := s.invitationService.Validate(ctx, rawInvitation)
		if err != nil {
			if !adminGate || !errors.Is(err, ErrPlayerInvitationInvalid) {
				return nil, ErrInvitationCodeInvalid
			}
		} else {
			out.PlayerToken = rawInvitation
			inviterID := player.InviterUserID
			out.AffiliateInviterID = &inviterID
		}
	}
	if out.AdminCode == nil && out.PlayerToken == "" && adminGate {
		if rawInvitation == "" {
			return nil, ErrInvitationCodeRequired
		}
		return nil, ErrInvitationCodeInvalid
	}

	code := strings.TrimSpace(affiliateCode)
	if code != "" && s.affiliateService != nil && s.affiliateService.IsEnabled(ctx) {
		inviterID, err := s.affiliateService.ResolveInviterByCode(ctx, code)
		if err != nil {
			return nil, err
		}
		if inviterID > 0 {
			out.AffiliateInviterID = &inviterID
		}
	}
	if out.PlayerToken != "" && out.AffiliateInviterID != nil {
		player, err := s.invitationService.Validate(ctx, out.PlayerToken)
		if err != nil {
			return nil, ErrPlayerInvitationInvalid
		}
		if player.InviterUserID != *out.AffiliateInviterID {
			return nil, ErrPlayerInvitationSourceConflict
		}
	}
	out.AffiliateCode = code
	return out, nil
}

const registrationInvitationAdvisoryLockID int64 = 78231701

func lockRegistrationInvitationSources(
	ctx context.Context,
	dialectName string,
	exec func(context.Context, string, ...any) (stdsql.Result, error),
) error {
	switch dialectName {
	case dialect.Postgres:
		if exec == nil {
			return errors.New("registration invitation SQL executor is unavailable")
		}
		_, err := exec(ctx, `SELECT pg_advisory_xact_lock($1)`, registrationInvitationAdvisoryLockID)
		return err
	case dialect.SQLite:
		return nil
	default:
		return fmt.Errorf("registration invitation locking is unsupported for dialect %q", dialectName)
	}
}

// createUserAndClaimInvitationSources is the single registration-completion
// boundary. When an Ent transaction is available, user creation, legacy code
// consumption, player reservation consumption, relationship creation, and
// affiliate binding all run in it.
func (s *AuthService) createUserAndClaimInvitationSources(ctx context.Context, user *User, sources *registrationInvitationSources) error {
	if s == nil || user == nil {
		return ErrServiceUnavailable
	}
	if sources == nil {
		sources = &registrationInvitationSources{}
	}
	commit := func(execCtx context.Context) error {
		if tx := dbent.TxFromContext(execCtx); tx != nil && (sources.PlayerToken != "" || sources.AffiliateCode != "") {
			txClient := tx.Client()
			if txClient == nil || txClient.Driver() == nil {
				return errors.New("registration invitation transaction client is unavailable")
			}
			if err := lockRegistrationInvitationSources(execCtx, txClient.Driver().Dialect(), txClient.ExecContext); err != nil {
				return err
			}
		}

		if err := s.createUserWithRegistrationEmailGuard(execCtx, user); err != nil {
			return err
		}
		if sources.AdminCode != nil {
			if err := s.redeemRepo.Use(execCtx, sources.AdminCode.ID, user.ID); err != nil {
				return ErrInvitationCodeInvalid
			}
		}
		if sources.PlayerToken != "" {
			if s.invitationService == nil {
				return ErrServiceUnavailable
			}
			relationship, err := s.invitationService.ClaimRegistration(execCtx, sources.PlayerToken, user.ID, sources.AffiliateInviterID)
			if err != nil {
				return err
			}
			if sources.AffiliateInviterID == nil {
				inviterID := relationship.InviterUserID
				sources.AffiliateInviterID = &inviterID
			}
		}
		if sources.AffiliateCode != "" && s.affiliateService != nil {
			if err := s.affiliateService.BindInviterByCode(execCtx, user.ID, sources.AffiliateCode); err != nil {
				return err
			}
		}
		return nil
	}

	needsAtomic := sources.AdminCode != nil || sources.PlayerToken != "" || sources.AffiliateCode != ""
	if !needsAtomic || s.entClient == nil {
		return commit(ctx)
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to start registration transaction: %v", err)
		return ErrServiceUnavailable
	}
	defer func() { _ = tx.Rollback() }()
	if err := commit(dbent.NewTxContext(ctx, tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to commit registration transaction: %v", err)
		return ErrServiceUnavailable
	}
	return nil
}

// RegisterOAuthEmailAccountWithPlayerInvitation is the W08 replacement for the
// legacy OAuth email creation call. Core account creation and invitation claim
// are atomic; the pending-identity binding remains in the caller's transaction
// and compensation releases the player reservation if it fails.
func (s *AuthService) RegisterOAuthEmailAccountWithPlayerInvitation(
	ctx context.Context,
	email, password, verifyCode, invitationCode, affiliateCode, signupSource string,
) (*TokenPair, *User, error) {
	if s == nil {
		return nil, nil, ErrServiceUnavailable
	}
	if s.settingService == nil || (!s.settingService.IsRegistrationEnabled(ctx) && !s.canBypassRegistrationDisabledForOAuth(ctx, signupSource)) {
		return nil, nil, ErrRegDisabled
	}
	email = strings.TrimSpace(strings.ToLower(email))
	if isReservedEmail(email) {
		return nil, nil, ErrEmailReserved
	}
	if err := s.VerifyOAuthEmailCode(ctx, email, verifyCode); err != nil {
		return nil, nil, err
	}
	sources, err := s.resolveRegistrationInvitationSources(ctx, invitationCode, affiliateCode)
	if err != nil {
		return nil, nil, err
	}
	existsEmail, err := s.existsByEmailOrAlias(ctx, email)
	if err != nil {
		return nil, nil, ErrServiceUnavailable
	}
	if existsEmail {
		return nil, nil, ErrEmailExists
	}
	if err := s.validateRegistrationEmailQuota(ctx, email); err != nil {
		return nil, nil, err
	}
	hashedPassword, err := s.HashPassword(password)
	if err != nil {
		return nil, nil, fmt.Errorf("hash password: %w", err)
	}
	signupSource = normalizeOAuthSignupSource(signupSource)
	grantPlan := s.resolveSignupGrantPlan(ctx, signupSource)
	var defaultRPMLimit int
	if s.settingService != nil {
		defaultRPMLimit = s.settingService.GetDefaultUserRPMLimit(ctx)
	}
	user := &User{
		Email:        email,
		PasswordHash: hashedPassword,
		Role:         RoleUser,
		Balance:      grantPlan.Balance,
		Concurrency:  grantPlan.Concurrency,
		RPMLimit:     defaultRPMLimit,
		Status:       StatusActive,
		SignupSource: signupSource,
	}
	if err := s.createUserAndClaimInvitationSources(ctx, user, sources); err != nil {
		switch {
		case errors.Is(err, ErrEmailExists):
			return nil, nil, ErrEmailExists
		case errors.Is(err, ErrEmailDomainRegistrationLimit):
			return nil, nil, ErrEmailDomainRegistrationLimit
		case errors.Is(err, ErrInvitationCodeInvalid), errors.Is(err, ErrPlayerInvitationInvalid), errors.Is(err, ErrPlayerInvitationSourceConflict):
			return nil, nil, err
		default:
			return nil, nil, ErrServiceUnavailable
		}
	}
	s.postAuthUserBootstrap(ctx, user, signupSource, false)
	s.assignSubscriptions(ctx, user.ID, grantPlan.Subscriptions, "auto assigned by signup defaults")
	_ = s.snapshotPlatformQuotaDefaults(ctx, user.ID, &grantPlan)
	tokenPair, err := s.GenerateTokenPair(ctx, user, "")
	if err != nil {
		_ = s.RollbackOAuthEmailAccountCreationWithSources(ctx, user.ID, invitationCode, affiliateCode)
		return nil, nil, fmt.Errorf("generate token pair: %w", err)
	}
	return tokenPair, user, nil
}

func (s *AuthService) RollbackOAuthEmailAccountCreationWithSources(ctx context.Context, userID int64, invitationCode string, _ ...string) error {
	if s == nil || s.userRepo == nil || userID <= 0 {
		return ErrServiceUnavailable
	}
	if s.invitationService != nil && strings.TrimSpace(invitationCode) != "" {
		if err := s.invitationService.ReleaseClaimedRegistration(ctx, userID, strings.TrimSpace(invitationCode)); err != nil {
			return err
		}
	}
	return s.RollbackOAuthEmailAccountCreation(ctx, userID, strings.TrimSpace(invitationCode))
}

// FinalizeOAuthEmailAccountWithSources skips the legacy invitation-code
// consumption when a player token already claimed the reservation atomically.
func (s *AuthService) FinalizeOAuthEmailAccountWithSources(ctx context.Context, user *User, invitationCode, signupSource, affiliateCode string) error {
	if s == nil || user == nil || user.ID <= 0 {
		return ErrServiceUnavailable
	}
	// RegisterOAuthEmailAccountWithPlayerInvitation already consumed the
	// legacy code or player reservation and performed bootstrap in the same
	// transaction as user creation. Reusing the legacy finalizer would consume
	// the credential a second time.
	s.updateOAuthSignupSource(ctx, user.ID, normalizeOAuthSignupSource(signupSource))
	_ = invitationCode
	_ = affiliateCode
	return nil
}

// RegisterVerifiedOAuthEmailAccountWithPlayerInvitation handles providers that
// already returned a verified email and keeps account/invitation completion in
// one transaction.
func (s *AuthService) RegisterVerifiedOAuthEmailAccountWithPlayerInvitation(
	ctx context.Context,
	email, password, invitationCode, affiliateCode, signupSource string,
) (*TokenPair, *User, error) {
	if s == nil {
		return nil, nil, ErrServiceUnavailable
	}
	if s.settingService == nil || (!s.settingService.IsRegistrationEnabled(ctx) && !s.canBypassRegistrationDisabledForOAuth(ctx, signupSource)) {
		return nil, nil, ErrRegDisabled
	}
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || isReservedEmail(email) {
		return nil, nil, ErrEmailVerifyRequired
	}
	sources, err := s.resolveRegistrationInvitationSources(ctx, invitationCode, affiliateCode)
	if err != nil {
		return nil, nil, err
	}
	existsEmail, err := s.existsByEmailOrAlias(ctx, email)
	if err != nil {
		return nil, nil, ErrServiceUnavailable
	}
	if existsEmail {
		return nil, nil, ErrEmailExists
	}
	if err := s.validateRegistrationEmailQuota(ctx, email); err != nil {
		return nil, nil, err
	}
	hashedPassword, err := s.HashPassword(password)
	if err != nil {
		return nil, nil, fmt.Errorf("hash password: %w", err)
	}
	signupSource = normalizeOAuthSignupSource(signupSource)
	grantPlan := s.resolveSignupGrantPlan(ctx, signupSource)
	var defaultRPMLimit int
	if s.settingService != nil {
		defaultRPMLimit = s.settingService.GetDefaultUserRPMLimit(ctx)
	}
	user := &User{
		Email:        email,
		PasswordHash: hashedPassword,
		Role:         RoleUser,
		Balance:      grantPlan.Balance,
		Concurrency:  grantPlan.Concurrency,
		RPMLimit:     defaultRPMLimit,
		Status:       StatusActive,
		SignupSource: signupSource,
	}
	if err := s.createUserAndClaimInvitationSources(ctx, user, sources); err != nil {
		switch {
		case errors.Is(err, ErrEmailExists):
			return nil, nil, ErrEmailExists
		case errors.Is(err, ErrEmailDomainRegistrationLimit):
			return nil, nil, ErrEmailDomainRegistrationLimit
		case errors.Is(err, ErrInvitationCodeInvalid), errors.Is(err, ErrPlayerInvitationInvalid), errors.Is(err, ErrPlayerInvitationSourceConflict):
			return nil, nil, err
		default:
			return nil, nil, ErrServiceUnavailable
		}
	}
	s.postAuthUserBootstrap(ctx, user, signupSource, false)
	s.assignSubscriptions(ctx, user.ID, grantPlan.Subscriptions, "auto assigned by signup defaults")
	_ = s.snapshotPlatformQuotaDefaults(ctx, user.ID, &grantPlan)
	tokenPair, err := s.GenerateTokenPair(ctx, user, "")
	if err != nil {
		_ = s.RollbackOAuthEmailAccountCreationWithSources(ctx, user.ID, invitationCode, affiliateCode)
		return nil, nil, fmt.Errorf("generate token pair: %w", err)
	}
	return tokenPair, user, nil
}
