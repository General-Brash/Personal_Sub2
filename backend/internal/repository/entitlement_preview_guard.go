package repository

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const entitlementPreviewLifetime = 10 * time.Minute
const entitlementPreviewFormat = 1

// Preview reads and apply's locked re-read share this SQL surface. In
// particular, a preview must not escape its repeatable-read transaction.
type entitlementQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type entitlementPreviewActor struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	UserID  int64  `json:"user_id"`
	Role    string `json:"role"`
	Version int64  `json:"version"`
}

type entitlementPreviewTarget struct {
	UserID int64  `json:"user_id"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

type entitlementPreviewClaims struct {
	Format    int                        `json:"v"`
	Actor     entitlementPreviewActor    `json:"actor"`
	Tier      string                     `json:"tier"`
	Targets   []entitlementPreviewTarget `json:"targets"`
	StateHash string                     `json:"state_hash"`
	IssuedAt  int64                      `json:"issued_at"`
	ExpiresAt int64                      `json:"expires_at"`
}

type entitlementPreviewPolicy struct {
	Tier    string          `json:"tier"`
	Enabled bool            `json:"enabled"`
	Version int64           `json:"version"`
	Groups  json.RawMessage `json:"groups"`
}

type entitlementPreviewUserState struct {
	entitlementPreviewTarget
	Assigned      json.RawMessage `json:"assigned"`
	Grants        json.RawMessage `json:"grants"`
	Manual        json.RawMessage `json:"manual"`
	Subscriptions json.RawMessage `json:"subscriptions"`
}

type entitlementPreviewState struct {
	Policies []entitlementPreviewPolicy    `json:"policies"`
	Users    []entitlementPreviewUserState `json:"users"`
}

func currentEntitlementPreviewActor(ctx context.Context) (entitlementPreviewActor, error) {
	principal, ok := service.AdminPrincipalFromContext(ctx)
	if !ok || principal == nil || !principal.Explicit || principal.UserID <= 0 ||
		(principal.Kind != service.AdminPrincipalKindJWT && principal.Kind != service.AdminPrincipalKindAPIKey) {
		return entitlementPreviewActor{}, service.ErrAdminPermissionDenied
	}
	return entitlementPreviewActor{ID: principal.ID, Kind: principal.Kind, UserID: principal.UserID, Role: principal.Role, Version: principal.Version}, nil
}

func (r *entitlementRepository) signEntitlementPreview(claims entitlementPreviewClaims) (string, error) {
	if len(r.previewSigningKey) == 0 {
		return "", errors.New("entitlement preview signing key unavailable")
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, r.previewSigningKey)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (r *entitlementRepository) verifyEntitlementPreview(token string, actor entitlementPreviewActor, ids []int64, tier string, now time.Time) (*entitlementPreviewClaims, error) {
	if len(r.previewSigningKey) == 0 {
		return nil, errors.New("entitlement preview signing key unavailable")
	}
	if len(token) == 0 || len(token) > 256*1024 {
		return nil, service.ErrEntitlementPreviewConflict
	}
	encoded, signature, found := strings.Cut(token, ".")
	if !found {
		return nil, service.ErrEntitlementPreviewConflict
	}
	digest, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return nil, service.ErrEntitlementPreviewConflict
	}
	mac := hmac.New(sha256.New, r.previewSigningKey)
	_, _ = mac.Write([]byte(encoded))
	if !hmac.Equal(digest, mac.Sum(nil)) {
		return nil, service.ErrEntitlementPreviewConflict
	}
	body, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, service.ErrEntitlementPreviewConflict
	}
	var claims entitlementPreviewClaims
	if json.Unmarshal(body, &claims) != nil || claims.Format != entitlementPreviewFormat || claims.Actor != actor || claims.Tier != tier ||
		claims.ExpiresAt <= now.Unix() || claims.IssuedAt > now.Unix() ||
		claims.ExpiresAt-claims.IssuedAt != int64(entitlementPreviewLifetime/time.Second) || len(claims.Targets) != len(ids) {
		return nil, service.ErrEntitlementPreviewConflict
	}
	for i, id := range ids {
		if claims.Targets[i].UserID != id {
			return nil, service.ErrEntitlementPreviewConflict
		}
	}
	return &claims, nil
}

func entitlementPreviewStateHash(state *entitlementPreviewState) (string, error) {
	body, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// Every state component used by the preview is read from the same snapshot.
// Subscription counters are deliberately excluded: consuming a subscription is
// not a change to its entitlement, whereas its identity/group/validity is.
func loadEntitlementPreviewState(ctx context.Context, db entitlementQueryer, ids []int64, now time.Time) (*entitlementPreviewState, error) {
	state := &entitlementPreviewState{Policies: []entitlementPreviewPolicy{}, Users: []entitlementPreviewUserState{}}
	rows, err := db.QueryContext(ctx, `
SELECT t.tier, t.enabled, t.version,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('group_id', g.group_id, 'rate', g.rate_multiplier, 'source', g.source, 'row_version', g.xmin::text) ORDER BY g.group_id)
 FROM entitlement_tier_groups g WHERE g.tier=t.tier), '[]'::jsonb)
FROM entitlement_tiers t WHERE t.tier IN ('standard','premium') ORDER BY t.tier`)
	if err != nil {
		return nil, fmt.Errorf("read entitlement preview policies: %w", err)
	}
	for rows.Next() {
		var policy entitlementPreviewPolicy
		if err := rows.Scan(&policy.Tier, &policy.Enabled, &policy.Version, &policy.Groups); err != nil {
			return nil, joinRowsCloseError(rows, err)
		}
		state.Policies = append(state.Policies, policy)
	}
	if err := joinRowsCloseError(rows, rows.Err()); err != nil {
		return nil, err
	}
	rows, err = db.QueryContext(ctx, `
SELECT u.id, u.role, u.status,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('tier', e.tier, 'source', e.source, 'version', e.version, 'expires_at', e.expires_at,
  'active', e.expires_at IS NULL OR e.expires_at > $2, 'row_version', e.xmin::text) ORDER BY e.tier)
  FROM user_entitlements e WHERE e.user_id=u.id), '[]'::jsonb),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('tier', g.tier, 'source', g.source, 'version', g.version, 'expires_at', g.expires_at,
  'active', g.expires_at IS NULL OR g.expires_at > $2, 'row_version', g.xmin::text) ORDER BY g.tier,g.source)
  FROM user_entitlement_grants g WHERE g.user_id=u.id), '[]'::jsonb),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('group_id', m.group_id, 'row_version', m.xmin::text) ORDER BY m.group_id)
  FROM user_allowed_groups m WHERE m.user_id=u.id), '[]'::jsonb),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id', s.id, 'group_id', s.group_id, 'starts_at', s.starts_at, 'expires_at', s.expires_at, 'status', s.status) ORDER BY s.id)
  FROM user_subscriptions s WHERE s.user_id=u.id AND s.status='active' AND s.deleted_at IS NULL AND (s.expires_at IS NULL OR s.expires_at > $2)), '[]'::jsonb)
FROM users u WHERE u.id=ANY($1) AND u.deleted_at IS NULL ORDER BY u.id`, pq.Array(ids), now)
	if err != nil {
		return nil, fmt.Errorf("read entitlement preview users: %w", err)
	}
	for rows.Next() {
		var user entitlementPreviewUserState
		if err := rows.Scan(&user.UserID, &user.Role, &user.Status, &user.Assigned, &user.Grants, &user.Manual, &user.Subscriptions); err != nil {
			return nil, joinRowsCloseError(rows, err)
		}
		state.Users = append(state.Users, user)
	}
	if err := joinRowsCloseError(rows, rows.Err()); err != nil {
		return nil, err
	}
	if len(state.Users) != len(ids) {
		return nil, service.ErrUserNotFound
	}
	return state, nil
}

func entitlementPreviewTargetPolicy(state *entitlementPreviewState, tier string) (entitlementPreviewPolicy, error) {
	for _, policy := range state.Policies {
		if policy.Tier == tier {
			return policy, nil
		}
	}
	return entitlementPreviewPolicy{}, service.ErrEntitlementTierUnknown
}

func normalizeEntitlementMutationIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 || len(ids) > 1000 {
		return nil, service.ErrEntitlementTargetsInvalid
	}
	set := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, service.ErrEntitlementTargetsInvalid
		}
		set[id] = struct{}{}
	}
	result := make([]int64, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

// Parent user/policy FOR UPDATE locks also block new FK children. Existing
// children are locked explicitly, since deleting a child need not lock its
// parent. Thus manual groups and subscriptions cannot change after validation.
func lockEntitlementPreviewState(ctx context.Context, tx *sql.Tx, ids []int64) error {
	queries := []struct {
		sql  string
		args []any
	}{
		{`SELECT tier FROM entitlement_tiers WHERE tier IN ('standard','premium') ORDER BY tier FOR UPDATE`, nil},
		{`SELECT tier FROM entitlement_tier_groups WHERE tier IN ('standard','premium') ORDER BY tier,group_id FOR UPDATE`, nil},
		{`SELECT user_id FROM user_entitlements WHERE user_id=ANY($1) ORDER BY user_id FOR UPDATE`, []any{pq.Array(ids)}},
		{`SELECT user_id FROM user_entitlement_grants WHERE user_id=ANY($1) ORDER BY user_id,tier,source FOR UPDATE`, []any{pq.Array(ids)}},
		{`SELECT user_id FROM user_allowed_groups WHERE user_id=ANY($1) ORDER BY user_id,group_id FOR UPDATE`, []any{pq.Array(ids)}},
		{`SELECT user_id FROM user_subscriptions WHERE user_id=ANY($1) ORDER BY user_id,id FOR UPDATE`, []any{pq.Array(ids)}},
	}
	for _, query := range queries {
		rows, err := tx.QueryContext(ctx, query.sql, query.args...)
		if err != nil {
			return fmt.Errorf("lock entitlement preview state: %w", err)
		}
		for rows.Next() {
		}
		if err := joinRowsCloseError(rows, rows.Err()); err != nil {
			return err
		}
	}
	return nil
}

// Lock lists include one extra actor in addition to the 1000 public targets.
func normalizeEntitlementMutationIDsForLocks(ids []int64) ([]int64, error) {
	set := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, service.ErrAdminPermissionDenied
		}
		set[id] = struct{}{}
	}
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func validateEntitlementPreviewTargets(users map[int64]adminMutationUserState, targets []entitlementPreviewTarget, actor *AdminMutationAuthorization) error {
	for _, target := range targets {
		current, exists := users[target.UserID]
		if !exists || current.Role != target.Role || current.Status != target.Status {
			return service.ErrEntitlementPreviewConflict
		}
		if current.Role == service.RoleSuperAdmin && (actor.ActorKind != service.AdminPrincipalKindJWT || actor.ActorRole != service.RoleSuperAdmin) {
			return service.ErrAdminCannotModifySuperAdmin
		}
	}
	return nil
}

type entitlementPreviewAssignment struct {
	Tier   string `json:"tier"`
	Active bool   `json:"active"`
}

func entitlementAssignmentNeeded(assigned json.RawMessage, tier string) (bool, error) {
	var rows []entitlementPreviewAssignment
	if err := json.Unmarshal(assigned, &rows); err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return tier != service.EntitlementTierStandard, nil
	}
	return rows[0].Tier != tier || !rows[0].Active, nil
}

func projectedEntitlementTier(state *entitlementPreviewState, grants json.RawMessage, tier string) (string, error) {
	var rows []entitlementPreviewAssignment
	if err := json.Unmarshal(grants, &rows); err != nil {
		return "", err
	}
	for _, grant := range rows {
		if !grant.Active || rankEntitlementTier(grant.Tier) <= rankEntitlementTier(tier) {
			continue
		}
		policy, err := entitlementPreviewTargetPolicy(state, grant.Tier)
		if err == nil && policy.Enabled {
			tier = grant.Tier
		}
	}
	return tier, nil
}
