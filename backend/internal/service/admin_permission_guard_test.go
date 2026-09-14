//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type adminMutationStateReaderTestRepo struct {
	*userRepoStub
	states map[int64]*AdminPermissionState
	errors map[int64]error
}

func (r *adminMutationStateReaderTestRepo) ReadAdminPermissionState(_ context.Context, userID int64) (*AdminPermissionState, error) {
	if err, ok := r.errors[userID]; ok {
		return nil, err
	}
	return r.states[userID], nil
}

func TestAuthorizeAdminPrincipalMutationReturnsConflictForChangedPreflightTarget(t *testing.T) {
	t.Setenv("ADMIN_PERMISSIONS_MODE", AdminPermissionModeDisabled)
	repo := &adminMutationStateReaderTestRepo{
		userRepoStub: &userRepoStub{},
		states: map[int64]*AdminPermissionState{
			1: {UserID: 1, Role: RoleAdmin, Status: StatusActive},
			2: {UserID: 2, Role: RoleSuperAdmin, Status: StatusActive},
		},
	}
	svc := &adminServiceImpl{userRepo: repo}
	ctx := ContextWithAdminMutationTargetSnapshot(context.Background(), RoleUser, StatusActive)

	err := authorizeAdminPrincipalMutation(ctx, svc, 1, 2, RoleAdmin, "")
	require.ErrorIs(t, err, ErrAdminMutationConflict)
}

type p35MutationSQLState string

func (e p35MutationSQLState) Error() string    { return string(e) }
func (e p35MutationSQLState) SQLState() string { return string(e) }
func TestAdminMutationConcurrentAbortIsConflictWithoutRetry(t *testing.T) {
	for _, code := range []p35MutationSQLState{"40001", "40P01"} {
		require.ErrorIs(t, normalizeAdminMutationError(code), ErrAdminMutationConflict)
	}
	original := p35MutationSQLState("08006")
	require.Equal(t, original, normalizeAdminMutationError(original))
}
