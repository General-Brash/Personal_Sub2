//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeUserListTierUsesCanonicalEffectiveTierValues(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty means no filter", in: "", want: ""},
		{name: "standard", in: " STANDARD ", want: EntitlementTierStandard},
		{name: "premium", in: "premium", want: EntitlementTierPremium},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeUserListTier(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	_, err := NormalizeUserListTier("entitlement_tier=premium")
	require.ErrorIs(t, err, ErrInvalidUserListTier)
}

func TestAdminServiceListUsersPassesCanonicalTierToRepository(t *testing.T) {
	repo := &userRepoStubForListUsers{users: []User{{ID: 7, Email: "premium@example.com"}}}
	svc := &adminServiceImpl{userRepo: repo}

	_, _, err := svc.ListUsers(context.Background(), 1, 20, UserListFilters{Tier: " PREMIUM "}, "", "")
	require.NoError(t, err)
	require.Equal(t, EntitlementTierPremium, repo.listWithFiltersFilter.Tier)
}
