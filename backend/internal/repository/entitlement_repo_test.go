package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func newEntitlementMock(t *testing.T) (*entitlementRepository, sqlmock.Sqlmock, context.Context) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); _ = db.Close() })
	repo := previewTestRepository()
	repo.sqlDB = db
	actor := previewTestActor()
	ctx := enforcedAdminMutationContext(&service.AdminPrincipal{ID: actor.ID, Kind: actor.Kind, UserID: actor.UserID, Role: actor.Role, Version: actor.Version, Explicit: true})
	return repo, mock, ctx
}

func expectEntitlementApplyStart(mock sqlmock.Sqlmock, targets []entitlementPreviewUserState, targetIDs ...int64) {
	if len(targetIDs) == 0 {
		targetIDs = []int64{7}
	}
	mock.ExpectBegin()
	rows := sqlmock.NewRows([]string{"id", "role", "status", "version"}).AddRow(int64(1), service.RoleSuperAdmin, service.StatusActive, int64(3))
	for _, target := range targets {
		rows.AddRow(target.UserID, target.Role, target.Status, int64(0))
	}
	expectAdminMutationLockUsers(mock, rows, append([]int64{1}, targetIDs...)...)
	expectAdminMutationLockUsers(mock, sqlmock.NewRows([]string{"id", "role", "status", "version"}).AddRow(int64(1), service.RoleSuperAdmin, service.StatusActive, int64(3)), 1)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT permission, effect, scope")).WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"permission", "effect", "scope"}))
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtextextended($1,0))")).WithArgs("entitlement-change:1:req-premium").WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectEntitlementStateRead(mock sqlmock.Sqlmock, state *entitlementPreviewState, ids []int64) {
	policies := sqlmock.NewRows([]string{"tier", "enabled", "version", "groups"})
	for _, policy := range state.Policies {
		policies.AddRow(policy.Tier, policy.Enabled, policy.Version, []byte(policy.Groups))
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT t.tier, t.enabled, t.version,")).WillReturnRows(policies)
	users := sqlmock.NewRows([]string{"id", "role", "status", "assigned", "grants", "manual", "subscriptions"})
	for _, user := range state.Users {
		users.AddRow(user.UserID, user.Role, user.Status, []byte(user.Assigned), []byte(user.Grants), []byte(user.Manual), []byte(user.Subscriptions))
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT u.id, u.role, u.status, COALESCE((SELECT jsonb_agg")).WithArgs(pq.Array(ids), sqlmock.AnyArg()).WillReturnRows(users)
}

func expectEntitlementStateLocks(mock sqlmock.Sqlmock) {
	for _, query := range []string{"SELECT tier FROM entitlement_tiers", "SELECT tier FROM entitlement_tier_groups"} {
		mock.ExpectQuery(regexp.QuoteMeta(query)).WillReturnRows(sqlmock.NewRows([]string{"tier"}))
	}
	for _, table := range []string{"user_entitlements", "user_entitlement_grants", "user_allowed_groups", "user_subscriptions"} {
		mock.ExpectQuery(regexp.QuoteMeta("SELECT user_id FROM " + table + " WHERE user_id=ANY($1)")).WithArgs(pq.Array([]int64{7})).WillReturnRows(sqlmock.NewRows([]string{"user_id"}))
	}
}

func entitlementRequestToken(t *testing.T, repo *entitlementRepository, state *entitlementPreviewState, at time.Time) string {
	t.Helper()
	token, err := repo.signEntitlementPreview(previewTestClaims(t, state, at))
	require.NoError(t, err)
	return token
}

func TestApplyEntitlementChangeCommitsTierVersionAndAuditAtomically(t *testing.T) {
	repo, mock, ctx := newEntitlementMock(t)
	state := previewTestState()
	token := entitlementRequestToken(t, repo, state, time.Now().UTC())
	expectEntitlementApplyStart(mock, state.Users)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT fingerprint,result FROM entitlement_change_requests")).WithArgs(int64(1), "req-premium").WillReturnError(sql.ErrNoRows)
	expectEntitlementStateLocks(mock)
	expectEntitlementStateRead(mock, state, []int64{7})
	mock.ExpectQuery(regexp.QuoteMeta("SELECT tier, version, expires_at FROM user_entitlements")).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"tier", "version", "expires_at"}).AddRow("standard", int64(1), nil))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO user_entitlements")).WithArgs(int64(7), "premium", int64(2), int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO entitlement_audit_logs")).WithArgs(int64(1), int64(7), "premium", int64(1), int64(2), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO entitlement_change_requests")).WithArgs(int64(1), "req-premium", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	result, err := repo.ApplyEntitlementChange(ctx, []int64{7}, "premium", 1, "campaign", "req-premium", token)
	require.NoError(t, err)
	require.Equal(t, 1, result.Changed)
	require.Equal(t, int64(3), result.Version)
}

func TestApplyEntitlementChangeRejectsChangedPreviewBeforeAnyWrite(t *testing.T) {
	cases := map[string]func(*entitlementPreviewState){
		"policy": func(s *entitlementPreviewState) { s.Policies[0].Version++ },
		"assigned": func(s *entitlementPreviewState) {
			s.Users[0].Assigned = json.RawMessage(`[{"tier":"standard","active":true,"version":2}]`)
		},
		"grant": func(s *entitlementPreviewState) {
			s.Users[0].Grants = json.RawMessage(`[{"tier":"premium","active":true}]`)
		},
		"manual": func(s *entitlementPreviewState) {
			s.Users[0].Manual = json.RawMessage(`[{"group_id":4,"row_version":"new"}]`)
		},
		"subscription": func(s *entitlementPreviewState) {
			s.Users[0].Subscriptions = json.RawMessage(`[{"id":2,"group_id":4}]`)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			repo, mock, ctx := newEntitlementMock(t)
			state := previewTestState()
			token := entitlementRequestToken(t, repo, state, time.Now().UTC())
			change(state)
			expectEntitlementApplyStart(mock, state.Users)
			mock.ExpectQuery(regexp.QuoteMeta("SELECT fingerprint,result FROM entitlement_change_requests")).WillReturnError(sql.ErrNoRows)
			expectEntitlementStateLocks(mock)
			expectEntitlementStateRead(mock, state, []int64{7})
			mock.ExpectRollback()
			_, err := repo.ApplyEntitlementChange(ctx, []int64{7}, "premium", 1, "campaign", "req-premium", token)
			require.ErrorIs(t, err, service.ErrEntitlementPreviewConflict)
		})
	}
}

func TestApplyEntitlementChangeMissingExpiredOrDeletedPreviewWritesNothing(t *testing.T) {
	for _, name := range []string{"missing", "expired", "deleted", "role_changed"} {
		t.Run(name, func(t *testing.T) {
			repo, mock, ctx := newEntitlementMock(t)
			state := previewTestState()
			when := time.Now().UTC()
			if name == "expired" {
				when = when.Add(-11 * time.Minute)
			}
			token := entitlementRequestToken(t, repo, state, when)
			if name == "missing" {
				token = ""
			}
			if name == "deleted" {
				state.Users = nil
			}
			if name == "role_changed" {
				state.Users[0].Role = service.RoleSuperAdmin
			}
			expectEntitlementApplyStart(mock, state.Users)
			mock.ExpectQuery(regexp.QuoteMeta("SELECT fingerprint,result FROM entitlement_change_requests")).WillReturnError(sql.ErrNoRows)
			mock.ExpectRollback()
			_, err := repo.ApplyEntitlementChange(ctx, []int64{7}, "premium", 1, "campaign", "req-premium", token)
			require.ErrorIs(t, err, service.ErrEntitlementPreviewConflict)
		})
	}
}

func TestApplyEntitlementChangeReplaysCompletedRequestBeforeExpiredPreview(t *testing.T) {
	repo, mock, ctx := newEntitlementMock(t)
	state := previewTestState()
	token := entitlementRequestToken(t, repo, state, time.Now().Add(-time.Hour))
	expectEntitlementApplyStart(mock, nil) // The target may have been deleted since completion.
	body, _ := json.Marshal(service.EntitlementChangeResult{Tier: "premium", Requested: 1, Changed: 1, Version: 3})
	mock.ExpectQuery(regexp.QuoteMeta("SELECT fingerprint,result FROM entitlement_change_requests")).WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "result"}).AddRow(entitlementChangeFingerprint([]int64{7}, "premium", "campaign", token), body))
	mock.ExpectCommit()
	result, err := repo.ApplyEntitlementChange(ctx, []int64{7}, "premium", 1, "campaign", "req-premium", token)
	require.NoError(t, err)
	require.True(t, result.Idempotent)
	require.Equal(t, 1, result.Changed)
}

func TestApplyEntitlementChangeRejectsDifferentInputOnCompletedKey(t *testing.T) {
	repo, mock, ctx := newEntitlementMock(t)
	expectEntitlementApplyStart(mock, previewTestState().Users)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT fingerprint,result FROM entitlement_change_requests")).WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "result"}).AddRow("different", []byte(`{}`)))
	mock.ExpectRollback()
	_, err := repo.ApplyEntitlementChange(ctx, []int64{7}, "premium", 1, "campaign", "req-premium", "")
	require.ErrorIs(t, err, service.ErrEntitlementIdempotencyConflict)
}

func TestPreviewEntitlementChangeReadOnlyDisabledPolicyAndMissingTargets(t *testing.T) {
	t.Run("disabled_readonly", func(t *testing.T) {
		repo, mock, ctx := newEntitlementMock(t)
		state := previewTestState()
		state.Policies[0].Enabled = false
		state.Users[0].Assigned = json.RawMessage(`[{"tier":"premium","active":true}]`)
		mock.ExpectBegin()
		expectEntitlementStateRead(mock, state, []int64{7})
		mock.ExpectCommit()
		result, err := repo.PreviewEntitlementChange(ctx, []int64{7}, "premium")
		require.NoError(t, err)
		require.False(t, result.PolicyEnabled)
		require.NotEmpty(t, result.PreviewToken)
		require.Empty(t, result.AffectedUserIDs)
		require.NotNil(t, result.AffectedUserIDs)
		require.Equal(t, []int64{7}, result.AlreadyAtTier)
		require.NotNil(t, result.GrantedGroupIDs)
		require.NotNil(t, result.RevokedGroupIDs)
	})
	t.Run("mixed_missing", func(t *testing.T) {
		repo, mock, ctx := newEntitlementMock(t)
		mock.ExpectBegin()
		expectEntitlementStateRead(mock, previewTestState(), []int64{7, 8})
		mock.ExpectRollback()
		_, err := repo.PreviewEntitlementChange(ctx, []int64{7, 8}, "standard")
		require.ErrorIs(t, err, service.ErrUserNotFound)
	})
	t.Run("disabled_apply", func(t *testing.T) {
		repo, mock, ctx := newEntitlementMock(t)
		state := previewTestState()
		state.Policies[0].Enabled = false
		token := entitlementRequestToken(t, repo, state, time.Now().UTC())
		expectEntitlementApplyStart(mock, state.Users)
		mock.ExpectQuery(regexp.QuoteMeta("SELECT fingerprint,result FROM entitlement_change_requests")).WillReturnError(sql.ErrNoRows)
		expectEntitlementStateLocks(mock)
		expectEntitlementStateRead(mock, state, []int64{7})
		mock.ExpectRollback()
		_, err := repo.ApplyEntitlementChange(ctx, []int64{7}, "premium", 1, "campaign", "req-premium", token)
		require.ErrorIs(t, err, service.ErrEntitlementDisabled)
	})
}

func TestEntitlementChangeFingerprintIncludesTargetsTierAndReason(t *testing.T) {
	original := entitlementChangeFingerprint([]int64{1, 2}, "premium", "reason")
	if original != entitlementChangeFingerprint([]int64{2, 1}, "premium", "reason") {
		t.Fatal("ID order must not change the request")
	}
	for _, value := range []string{entitlementChangeFingerprint([]int64{1, 3}, "premium", "reason"), entitlementChangeFingerprint([]int64{1, 2}, "standard", "reason"), entitlementChangeFingerprint([]int64{1, 2}, "premium", "other")} {
		if value == original {
			t.Fatal("different entitlement request shares a fingerprint")
		}
	}
}

func TestEntitlementFingerprintBindsGuardAndPreservesLegacyReplay(t *testing.T) {
	legacy := entitlementChangeFingerprint([]int64{7}, "premium", "reason")
	require.Equal(t, legacy, entitlementChangeFingerprint([]int64{7}, "premium", "reason", ""))
	require.NotEqual(t, legacy, entitlementChangeFingerprint([]int64{7}, "premium", "reason", "first-token"))
	require.NotEqual(t, entitlementChangeFingerprint([]int64{7}, "premium", "reason", "first-token"), entitlementChangeFingerprint([]int64{7}, "premium", "reason", "second-token"))
}

func TestApplyEntitlementChangeReplaysCompletedLegacyRequestWithoutNewGuard(t *testing.T) {
	repo, mock, ctx := newEntitlementMock(t)
	expectEntitlementApplyStart(mock, nil)
	body, _ := json.Marshal(service.EntitlementChangeResult{Tier: "premium", Requested: 1, Changed: 1, Version: 3})
	mock.ExpectQuery(regexp.QuoteMeta("SELECT fingerprint,result FROM entitlement_change_requests")).WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "result"}).AddRow(entitlementChangeFingerprint([]int64{7}, "premium", "campaign"), body))
	mock.ExpectCommit()
	result, err := repo.ApplyEntitlementChange(ctx, []int64{7}, "premium", 1, "campaign", "req-premium", "")
	require.NoError(t, err)
	require.True(t, result.Idempotent)
}

func TestApplyEntitlementBatchRejectsMissingLastTargetBeforeAnyWrite(t *testing.T) {
	repo, mock, ctx := newEntitlementMock(t)
	state := previewTestState()
	second := state.Users[0]
	second.UserID = 8
	state.Users = append(state.Users, second)
	token := entitlementRequestToken(t, repo, state, time.Now().UTC())
	expectEntitlementApplyStart(mock, state.Users[:1], 7, 8)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT fingerprint,result FROM entitlement_change_requests")).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	_, err := repo.ApplyEntitlementChange(ctx, []int64{7, 8}, "premium", 1, "campaign", "req-premium", token)
	require.ErrorIs(t, err, service.ErrEntitlementPreviewConflict)
}
