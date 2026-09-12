package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	InvitationScopeQuotaAdjust          = "invites.quota.adjust"
	InvitationScopeRelationshipCreate   = "affiliates.relationship.create"
	InvitationScopeRelationshipReassign = "affiliates.relationship.reassign"
)

var (
	ErrPlayerInvitationDisabled       = infraerrors.Forbidden("PLAYER_INVITATION_DISABLED", "player invitation is not enabled")
	ErrPlayerInvitationRequired       = infraerrors.BadRequest("PLAYER_INVITATION_REQUIRED", "a valid player invitation is required")
	ErrPlayerInvitationInvalid        = infraerrors.BadRequest("PLAYER_INVITATION_INVALID", "player invitation is invalid, expired, cancelled, or already claimed")
	ErrPlayerInvitationSourceConflict = infraerrors.Conflict("INVITATION_SOURCE_CONFLICT", "invitation source conflicts with affiliate code")
	ErrInvitationQuotaExhausted       = infraerrors.Conflict("INVITATION_QUOTA_EXHAUSTED", "no invitation quota is available")
	ErrInvitationRelationshipExists   = infraerrors.Conflict("INVITATION_RELATIONSHIP_EXISTS", "invitee already has an inviter")
	ErrInvitationSelfReferral         = infraerrors.BadRequest("INVITATION_SELF_REFERRAL", "self invitation is not allowed")
	ErrInvitationRelationshipCycle    = infraerrors.Conflict("INVITATION_RELATIONSHIP_CYCLE", "invitation relationship would create a cycle")
	ErrInvitationPermissionDenied     = infraerrors.Forbidden("INVITATION_PERMISSION_DENIED", "invitation administration permission denied")
)

// InvitationAuthorizer is the explicit permission callback boundary. Wiring the
// service without one is safe: all administrative actions are denied.
type InvitationAuthorizer interface {
	AuthorizeInvitation(ctx context.Context, actorUserID int64, scope string) error
}

type InvitationAuthorizerFunc func(context.Context, int64, string) error

func (f InvitationAuthorizerFunc) AuthorizeInvitation(ctx context.Context, actorUserID int64, scope string) error {
	if f == nil {
		return ErrInvitationPermissionDenied
	}
	return f(ctx, actorUserID, scope)
}

type PlayerInvitationPolicy struct {
	Enabled        bool
	ReservationTTL time.Duration
}

type PlayerInvitationPolicyProvider interface {
	GetPlayerInvitationPolicy(ctx context.Context) (PlayerInvitationPolicy, error)
}

type PlayerInvitationPolicyFunc func(context.Context) (PlayerInvitationPolicy, error)

func (f PlayerInvitationPolicyFunc) GetPlayerInvitationPolicy(ctx context.Context) (PlayerInvitationPolicy, error) {
	if f == nil {
		return PlayerInvitationPolicy{}, nil
	}
	return f(ctx)
}

type PlayerInvitationSummary struct {
	UserID       int64     `json:"user_id"`
	Available    int64     `json:"available"`
	Reserved     int64     `json:"reserved"`
	Consumed     int64     `json:"consumed"`
	TotalGranted int64     `json:"total_granted"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type PlayerInvitationReservation struct {
	ID            int64      `json:"id"`
	InviterUserID int64      `json:"inviter_user_id"`
	Status        string     `json:"status"`
	ExpiresAt     time.Time  `json:"expires_at"`
	ClaimedUserID *int64     `json:"claimed_user_id,omitempty"`
	ClaimedAt     *time.Time `json:"claimed_at,omitempty"`
	CancelledAt   *time.Time `json:"cancelled_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type PlayerInvitationCredential struct {
	Token       string                      `json:"token"`
	Reservation PlayerInvitationReservation `json:"reservation"`
}

type PlayerInvitationContext struct {
	ReservationID int64
	InviterUserID int64
	ExpiresAt     time.Time
}

type InvitationRelationship struct {
	InviteeUserID int64     `json:"invitee_user_id"`
	InviterUserID int64     `json:"inviter_user_id"`
	Source        string    `json:"source"`
	EffectiveAt   time.Time `json:"effective_at"`
}

type InvitationQuotaAdjustment struct {
	ActorUserID    int64
	TargetUserID   int64
	Delta          int
	Reason         string
	IdempotencyKey string
	RequestID      string
}

type InvitationRelationshipCreateInput struct {
	ActorUserID   int64
	InviterUserID int64
	InviteeUserID int64
	EffectiveAt   time.Time
	Reason        string
	RequestID     string
}

// PlayerInvitationRepository is intentionally independent from AffiliateRepository.
// Implementations must participate in a transaction carried by ctx when present.
type PlayerInvitationRepository interface {
	EnsureInitialQuota(ctx context.Context, userID int64) error
	GetSummary(ctx context.Context, userID int64) (*PlayerInvitationSummary, error)
	ListReservations(ctx context.Context, userID int64, limit int) ([]PlayerInvitationReservation, error)
	Reserve(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time, requestHash ...string) (*PlayerInvitationReservation, error)
	CancelReservation(ctx context.Context, userID, reservationID int64, now time.Time) (bool, error)
	ExpireReservations(ctx context.Context, userID int64, now time.Time) (int64, error)
	GetReservationContextByHash(ctx context.Context, tokenHash string, now time.Time) (*PlayerInvitationContext, error)
	ClaimReservation(ctx context.Context, tokenHash string, inviteeUserID int64, expectedInviterID *int64, now time.Time) (*InvitationRelationship, error)
	ReleaseClaimedReservation(ctx context.Context, inviteeUserID int64, tokenHash string, now time.Time) error
	AdjustQuota(ctx context.Context, input InvitationQuotaAdjustment, before, after int64) error
	CreateAdminRelationship(ctx context.Context, input InvitationRelationshipCreateInput) (*InvitationRelationship, error)
}

type PlayerInvitationService struct {
	signingKey []byte
	repo       PlayerInvitationRepository
	authz      InvitationAuthorizer
	policy     PlayerInvitationPolicyProvider
}

func NewPlayerInvitationService(repo PlayerInvitationRepository, authz InvitationAuthorizer, policy PlayerInvitationPolicyProvider) *PlayerInvitationService {
	return &PlayerInvitationService{repo: repo, authz: authz, policy: policy}
}

func (s *PlayerInvitationService) SetPolicyProvider(policy PlayerInvitationPolicyProvider) {
	if s != nil {
		s.policy = policy
	}
}

func (s *PlayerInvitationService) policyNow(ctx context.Context) (PlayerInvitationPolicy, error) {
	if s == nil || s.repo == nil || s.policy == nil {
		return PlayerInvitationPolicy{}, nil
	}
	p, err := s.policy.GetPlayerInvitationPolicy(ctx)
	if err != nil {
		return PlayerInvitationPolicy{}, err
	}
	if p.Enabled && p.ReservationTTL <= 0 {
		return PlayerInvitationPolicy{}, fmt.Errorf("player invitation expiry must be explicitly configured")
	}
	return p, nil
}

func (s *PlayerInvitationService) IsEnabled(ctx context.Context) bool {
	p, err := s.policyNow(ctx)
	return err == nil && p.Enabled
}

func (s *PlayerInvitationService) GetSummary(ctx context.Context, userID int64) (*PlayerInvitationSummary, error) {
	if s == nil || s.repo == nil || userID <= 0 {
		return nil, ErrServiceUnavailable
	}
	policy, err := s.policyNow(ctx)
	if err != nil {
		return nil, err
	}
	if !policy.Enabled {
		return nil, ErrPlayerInvitationDisabled
	}
	if err := s.repo.EnsureInitialQuota(ctx, userID); err != nil {
		return nil, err
	}
	_, _ = s.repo.ExpireReservations(ctx, userID, time.Now().UTC())
	return s.repo.GetSummary(ctx, userID)
}

func (s *PlayerInvitationService) ListReservations(ctx context.Context, userID int64, limit int) ([]PlayerInvitationReservation, error) {
	if s == nil || s.repo == nil || userID <= 0 {
		return nil, ErrServiceUnavailable
	}
	policy, err := s.policyNow(ctx)
	if err != nil {
		return nil, err
	}
	if !policy.Enabled {
		return nil, ErrPlayerInvitationDisabled
	}
	if err := s.repo.EnsureInitialQuota(ctx, userID); err != nil {
		return nil, err
	}
	_, _ = s.repo.ExpireReservations(ctx, userID, time.Now().UTC())
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.ListReservations(ctx, userID, limit)
}

func (s *PlayerInvitationService) Reserve(ctx context.Context, userID int64, requestKey string) (*PlayerInvitationCredential, error) {
	p, err := s.policyNow(ctx)
	if err != nil {
		return nil, err
	}
	if !p.Enabled {
		return nil, ErrPlayerInvitationDisabled
	}
	if s == nil || s.repo == nil || userID <= 0 {
		return nil, ErrServiceUnavailable
	}
	token, requestHash, err := s.reservationToken(userID, requestKey)
	if err != nil {
		return nil, err
	}
	hash := hashPlayerInvitationToken(token)
	reservation, err := s.repo.Reserve(ctx, userID, hash, time.Now().UTC().Add(p.ReservationTTL), requestHash)
	if err != nil {
		return nil, err
	}
	return &PlayerInvitationCredential{Token: token, Reservation: *reservation}, nil
}

func (s *PlayerInvitationService) Cancel(ctx context.Context, userID, reservationID int64) error {
	if s == nil || s.repo == nil || userID <= 0 || reservationID <= 0 {
		return ErrServiceUnavailable
	}
	ok, err := s.repo.CancelReservation(ctx, userID, reservationID, time.Now().UTC())
	if err != nil {
		return err
	}
	if !ok {
		return ErrPlayerInvitationInvalid
	}
	return nil
}

// Validate checks a player token without consuming it. Public preflight calls
// may use this method; registration claim must use ClaimRegistration.
func (s *PlayerInvitationService) Validate(ctx context.Context, rawToken string) (*PlayerInvitationContext, error) {
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrPlayerInvitationRequired
	}
	return s.repo.GetReservationContextByHash(ctx, hashPlayerInvitationToken(rawToken), time.Now().UTC())
}

// ClaimRegistration must be called with the same transaction context as user
// creation. It consumes the reservation and creates the relationship atomically.
func (s *PlayerInvitationService) ClaimRegistration(ctx context.Context, rawToken string, inviteeUserID int64, expectedInviterID *int64) (*InvitationRelationship, error) {
	if s == nil || s.repo == nil || inviteeUserID <= 0 {
		return nil, ErrServiceUnavailable
	}
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrPlayerInvitationRequired
	}
	return s.repo.ClaimReservation(ctx, hashPlayerInvitationToken(rawToken), inviteeUserID, expectedInviterID, time.Now().UTC())
}

func (s *PlayerInvitationService) ReleaseClaimedRegistration(ctx context.Context, inviteeUserID int64, rawToken string) error {
	if s == nil || s.repo == nil || inviteeUserID <= 0 || strings.TrimSpace(rawToken) == "" {
		return nil
	}
	return s.repo.ReleaseClaimedReservation(ctx, inviteeUserID, hashPlayerInvitationToken(rawToken), time.Now().UTC())
}

func (s *PlayerInvitationService) AdminAdjustQuota(ctx context.Context, input InvitationQuotaAdjustment) error {
	if err := s.authorize(ctx, input.ActorUserID, InvitationScopeQuotaAdjust); err != nil {
		return err
	}
	if input.TargetUserID <= 0 || input.Delta == 0 || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.IdempotencyKey) == "" {
		return infraerrors.BadRequest("INVALID_INVITATION_ADJUSTMENT", "target, delta, reason, and idempotency key are required")
	}
	if s == nil || s.repo == nil {
		return ErrServiceUnavailable
	}
	if err := s.repo.EnsureInitialQuota(ctx, input.TargetUserID); err != nil {
		return err
	}
	before, err := s.repo.GetSummary(ctx, input.TargetUserID)
	if err != nil {
		return err
	}
	if before.TotalGranted+int64(input.Delta) < before.Reserved+before.Consumed {
		return ErrInvitationQuotaExhausted
	}
	afterTotal := before.TotalGranted + int64(input.Delta)
	return s.repo.AdjustQuota(ctx, input, before.TotalGranted, afterTotal)
}

func (s *PlayerInvitationService) AdminCreateRelationship(ctx context.Context, input InvitationRelationshipCreateInput) (*InvitationRelationship, error) {
	if err := s.authorize(ctx, input.ActorUserID, InvitationScopeRelationshipCreate); err != nil {
		return nil, err
	}
	if input.InviterUserID <= 0 || input.InviteeUserID <= 0 || input.InviterUserID == input.InviteeUserID || strings.TrimSpace(input.Reason) == "" {
		return nil, ErrInvitationSelfReferral
	}
	input.EffectiveAt = time.Now().UTC() // never permit historical relationship backdating
	if s == nil || s.repo == nil {
		return nil, ErrServiceUnavailable
	}
	return s.repo.CreateAdminRelationship(ctx, input)
}

func (s *PlayerInvitationService) authorize(ctx context.Context, actorUserID int64, scope string) error {
	if s == nil || s.authz == nil || actorUserID <= 0 {
		return ErrInvitationPermissionDenied
	}
	if err := s.authz.AuthorizeInvitation(ctx, actorUserID, scope); err != nil {
		if errors.Is(err, ErrInvitationPermissionDenied) {
			return err
		}
		return fmt.Errorf("authorize invitation scope %s: %w", scope, err)
	}
	return nil
}

func hashPlayerInvitationToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func (s *PlayerInvitationService) SetSigningKey(key string) { s.signingKey = []byte(key) }

// The stable request hash is public dedup metadata. The credential is HMACed
// with a server secret, so neither DB disclosure nor a predictable request key
// reveals the invitation token. Rotation mismatches fail closed, not re-reserve.
func (s *PlayerInvitationService) reservationToken(userID int64, requestKey string) (string, string, error) {
	requestKey = strings.TrimSpace(requestKey)
	if len(requestKey) < 16 || len(requestKey) > 128 {
		return "", "", ErrIdempotencyKeyRequired
	}
	if s == nil || len(s.signingKey) < 16 {
		return "", "", ErrServiceUnavailable
	}
	message := fmt.Sprintf("player-invitation-v1:%d:%s", userID, requestKey)
	digest := sha256.Sum256([]byte(message))
	mac := hmac.New(sha256.New, s.signingKey)
	_, _ = mac.Write([]byte(message))
	return "pi_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), hex.EncodeToString(digest[:]), nil
}

type InvitationAdminPreview struct {
	InviterUserID     int64  `json:"inviter_user_id"`
	InviteeUserID     int64  `json:"invitee_user_id"`
	ExistingInviterID *int64 `json:"existing_inviter_id"`
	Cycle             bool   `json:"cycle"`
	CanCreate         bool   `json:"can_create"`
	HistoricalRebate  bool   `json:"historical_rebate"`
	ConsumesQuota     bool   `json:"consumes_quota"`
}
type invitationPreviewReader interface {
	PreviewInvitationRelationship(context.Context, int64, int64) (*InvitationAdminPreview, error)
}

func (s *PlayerInvitationService) AdminPreview(ctx context.Context, actor, inviter, invitee int64) (*InvitationAdminPreview, error) {
	if err := s.authorize(ctx, actor, InvitationScopeRelationshipCreate); err != nil {
		return nil, err
	}
	if inviter <= 0 || invitee <= 0 || inviter == invitee {
		return nil, ErrInvitationSelfReferral
	}
	repo, ok := s.repo.(invitationPreviewReader)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	return repo.PreviewInvitationRelationship(ctx, inviter, invitee)
}
