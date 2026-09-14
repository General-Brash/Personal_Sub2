package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func previewTestRepository() *entitlementRepository {
	repo, ok := NewEntitlementRepository(nil, &config.Config{JWT: config.JWTConfig{Secret: strings.Repeat("preview-test-secret-", 3)}}).(*entitlementRepository)
	if !ok {
		panic("NewEntitlementRepository returned an unexpected implementation")
	}
	return repo
}

func previewTestActor() entitlementPreviewActor {
	return entitlementPreviewActor{ID: "user:1", Kind: service.AdminPrincipalKindJWT, UserID: 1, Role: service.RoleSuperAdmin, Version: 3}
}

func previewTestState() *entitlementPreviewState {
	return &entitlementPreviewState{
		Policies: []entitlementPreviewPolicy{{Tier: "premium", Enabled: true, Version: 3, Groups: json.RawMessage(`[]`)}, {Tier: "standard", Enabled: true, Version: 1, Groups: json.RawMessage(`[]`)}},
		Users: []entitlementPreviewUserState{{entitlementPreviewTarget: entitlementPreviewTarget{UserID: 7, Role: service.RoleUser, Status: service.StatusActive},
			Assigned: json.RawMessage(`[{"tier":"standard","active":true,"version":1}]`), Grants: json.RawMessage(`[]`), Manual: json.RawMessage(`[]`), Subscriptions: json.RawMessage(`[]`)}},
	}
}

func previewTestClaims(t *testing.T, state *entitlementPreviewState, now time.Time) entitlementPreviewClaims {
	t.Helper()
	digest, err := entitlementPreviewStateHash(state)
	require.NoError(t, err)
	claims := entitlementPreviewClaims{Format: entitlementPreviewFormat, Actor: previewTestActor(), Tier: "premium", StateHash: digest,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(entitlementPreviewLifetime).Unix(), Targets: []entitlementPreviewTarget{}}
	for _, user := range state.Users {
		claims.Targets = append(claims.Targets, user.entitlementPreviewTarget)
	}
	return claims
}

func TestEntitlementPreviewTokenBindsActorTargetsTierAndExpiry(t *testing.T) {
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	repo := previewTestRepository()
	claims := previewTestClaims(t, previewTestState(), now)
	token, err := repo.signEntitlementPreview(claims)
	require.NoError(t, err)
	// Independently constructed repository instances share the configured key.
	for _, verifier := range []*entitlementRepository{repo, previewTestRepository()} {
		got, err := verifier.verifyEntitlementPreview(token, claims.Actor, []int64{7}, "premium", now.Add(9*time.Minute))
		require.NoError(t, err)
		require.Equal(t, claims, *got)
	}
	checks := []struct {
		name  string
		token string
		actor entitlementPreviewActor
		ids   []int64
		tier  string
		at    time.Time
	}{
		{"missing", "", claims.Actor, []int64{7}, "premium", now},
		{"tampered", token + "x", claims.Actor, []int64{7}, "premium", now},
		{"target", token, claims.Actor, []int64{8}, "premium", now},
		{"tier", token, claims.Actor, []int64{7}, "standard", now},
		{"expired_at_boundary", token, claims.Actor, []int64{7}, "premium", now.Add(10 * time.Minute)},
		{"before_issue", token, claims.Actor, []int64{7}, "premium", now.Add(-time.Second)},
	}
	for _, change := range []string{"kind", "id", "user", "role", "version"} {
		actor := claims.Actor
		switch change {
		case "kind":
			actor.Kind = service.AdminPrincipalKindAPIKey
		case "id":
			actor.ID = "admin-key:other"
		case "user":
			actor.UserID = 2
		case "role":
			actor.Role = service.RoleAdmin
		case "version":
			actor.Version++
		}
		checks = append(checks, struct {
			name  string
			token string
			actor entitlementPreviewActor
			ids   []int64
			tier  string
			at    time.Time
		}{change, token, actor, []int64{7}, "premium", now})
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			_, err := repo.verifyEntitlementPreview(check.token, check.actor, check.ids, check.tier, check.at)
			require.ErrorIs(t, err, service.ErrEntitlementPreviewConflict)
		})
	}
	other, ok := NewEntitlementRepository(nil, &config.Config{JWT: config.JWTConfig{Secret: "different-key"}}).(*entitlementRepository)
	require.True(t, ok)
	_, err = other.verifyEntitlementPreview(token, claims.Actor, []int64{7}, "premium", now)
	require.ErrorIs(t, err, service.ErrEntitlementPreviewConflict)
	_, err = (&entitlementRepository{}).signEntitlementPreview(claims)
	require.Error(t, err)
}

func TestEntitlementPreviewStateIncludesPolicyGrantsManualAndSubscriptions(t *testing.T) {
	original := previewTestState()
	before, err := entitlementPreviewStateHash(original)
	require.NoError(t, err)
	changes := map[string]func(*entitlementPreviewState){
		"policy_version": func(s *entitlementPreviewState) { s.Policies[0].Version++ },
		"policy_enabled": func(s *entitlementPreviewState) { s.Policies[0].Enabled = false },
		"policy_groups":  func(s *entitlementPreviewState) { s.Policies[0].Groups = json.RawMessage(`[{"group_id":2}]`) },
		"assigned": func(s *entitlementPreviewState) {
			s.Users[0].Assigned = json.RawMessage(`[{"tier":"premium","active":true,"version":2}]`)
		},
		"grant": func(s *entitlementPreviewState) {
			s.Users[0].Grants = json.RawMessage(`[{"tier":"premium","active":true,"version":1}]`)
		},
		"manual_group": func(s *entitlementPreviewState) {
			s.Users[0].Manual = json.RawMessage(`[{"group_id":2,"row_version":"2"}]`)
		},
		"effective_subscription": func(s *entitlementPreviewState) {
			s.Users[0].Subscriptions = json.RawMessage(`[{"id":3,"group_id":2,"status":"active"}]`)
		},
		"role":   func(s *entitlementPreviewState) { s.Users[0].Role = service.RoleAdmin },
		"status": func(s *entitlementPreviewState) { s.Users[0].Status = "disabled" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			state := previewTestState()
			change(state)
			after, err := entitlementPreviewStateHash(state)
			require.NoError(t, err)
			require.NotEqual(t, before, after)
		})
	}
}

func TestEntitlementPreviewTargetChecksProtectSuperAdminAndAllowDisabledUsers(t *testing.T) {
	targets := []entitlementPreviewTarget{{UserID: 7, Role: service.RoleUser, Status: "disabled"}}
	rows := map[int64]adminMutationUserState{7: {UserID: 7, Role: service.RoleUser, Status: "disabled"}}
	actor := &AdminMutationAuthorization{ActorKind: service.AdminPrincipalKindJWT, ActorRole: service.RoleAdmin}
	require.NoError(t, validateEntitlementPreviewTargets(rows, targets, actor))
	require.ErrorIs(t, validateEntitlementPreviewTargets(map[int64]adminMutationUserState{}, targets, actor), service.ErrEntitlementPreviewConflict)
	rows[7] = adminMutationUserState{UserID: 7, Role: service.RoleSuperAdmin, Status: "disabled"}
	require.ErrorIs(t, validateEntitlementPreviewTargets(rows, targets, actor), service.ErrEntitlementPreviewConflict)
	targets[0].Role = service.RoleSuperAdmin
	require.ErrorIs(t, validateEntitlementPreviewTargets(rows, targets, actor), service.ErrAdminCannotModifySuperAdmin)
	actor.ActorRole = service.RoleSuperAdmin
	require.NoError(t, validateEntitlementPreviewTargets(rows, targets, actor))
	actor.ActorKind = service.AdminPrincipalKindAPIKey
	require.ErrorIs(t, validateEntitlementPreviewTargets(rows, targets, actor), service.ErrAdminCannotModifySuperAdmin)
}

func TestEntitlementPreviewKeepsIndependentPremiumGrantAndCountsAssignedTier(t *testing.T) {
	state := previewTestState()
	effective, err := projectedEntitlementTier(state, json.RawMessage(`[{"tier":"premium","active":true}]`), "standard")
	require.NoError(t, err)
	require.Equal(t, "premium", effective)
	effective, err = projectedEntitlementTier(state, json.RawMessage(`[{"tier":"premium","active":false}]`), "standard")
	require.NoError(t, err)
	require.Equal(t, "standard", effective)
	needed, err := entitlementAssignmentNeeded(json.RawMessage(`[]`), "standard")
	require.NoError(t, err)
	require.False(t, needed)
	needed, err = entitlementAssignmentNeeded(json.RawMessage(`[{"tier":"premium","active":false}]`), "premium")
	require.NoError(t, err)
	require.True(t, needed)
	require.Equal(t, []int64{}, mergeInt64Groups(nil, nil))
	require.Equal(t, []int64{2, 5}, mapKeys(map[int64]struct{}{5: {}, 2: {}}))
}

func TestEntitlementPreviewActorRequiresExplicitIdentity(t *testing.T) {
	_, err := currentEntitlementPreviewActor(context.Background())
	require.ErrorIs(t, err, service.ErrAdminPermissionDenied)
	_, err = currentEntitlementPreviewActor(service.ContextWithAdminPrincipal(context.Background(), &service.AdminPrincipal{UserID: 1, Kind: service.AdminPrincipalKindJWT}))
	require.ErrorIs(t, err, service.ErrAdminPermissionDenied)
}
