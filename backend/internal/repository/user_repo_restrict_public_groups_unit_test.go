//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRepositoryUpdateRestrictPublicGroupsPersistsTrue(t *testing.T) {
	repo, _ := newUserEntRepo(t)
	ctx := context.Background()
	user := &service.User{
		Email:                "restrict-true@example.com",
		Username:             "restrict-true",
		PasswordHash:         "hash",
		Role:                 service.RoleUser,
		Status:               service.StatusActive,
		RestrictPublicGroups: false,
	}
	require.NoError(t, repo.Create(ctx, user))

	user.RestrictPublicGroups = true
	require.NoError(t, repo.Update(ctx, user, service.UserUpdateFields{RestrictPublicGroups: true}))

	got, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, got.RestrictPublicGroups)
}

func TestUserRepositoryUpdateRestrictPublicGroupsPersistsFalse(t *testing.T) {
	repo, _ := newUserEntRepo(t)
	ctx := context.Background()
	user := &service.User{
		Email:                "restrict-false@example.com",
		Username:             "restrict-false",
		PasswordHash:         "hash",
		Role:                 service.RoleUser,
		Status:               service.StatusActive,
		RestrictPublicGroups: true,
	}
	require.NoError(t, repo.Create(ctx, user))

	user.RestrictPublicGroups = false
	require.NoError(t, repo.Update(ctx, user, service.UserUpdateFields{RestrictPublicGroups: true}))

	got, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.False(t, got.RestrictPublicGroups)
}

func TestUserRepositoryUpdateWithoutRestrictPublicGroupsLeavesColumnUntouched(t *testing.T) {
	repo, _ := newUserEntRepo(t)
	ctx := context.Background()
	user := &service.User{
		Email:                "restrict-untouched@example.com",
		Username:             "before",
		PasswordHash:         "hash",
		Role:                 service.RoleUser,
		Status:               service.StatusActive,
		RestrictPublicGroups: true,
	}
	require.NoError(t, repo.Create(ctx, user))

	user.Username = "after"
	user.RestrictPublicGroups = false
	require.NoError(t, repo.Update(ctx, user, service.UserUpdateFields{Username: true}))

	got, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, "after", got.Username)
	require.True(t, got.RestrictPublicGroups)
}
