package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

type recordAllQueryMatcher struct {
	queries *[]string
}

func (m recordAllQueryMatcher) Match(_, actual string) error {
	*m.queries = append(*m.queries, actual)
	return nil
}

func TestUserRepositoryListWithFiltersTierIsAppliedToCountAndPageQuery(t *testing.T) {
	for _, tc := range []struct {
		name         string
		tier         string
		expectedExpr string
	}{
		{name: "premium", tier: service.EntitlementTierPremium, expectedExpr: "EXISTS ("},
		{name: "standard", tier: service.EntitlementTierStandard, expectedExpr: "NOT EXISTS ("},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queries := make([]string, 0, 2)
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(recordAllQueryMatcher{queries: &queries}))
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			repo := newUserRepositoryWithSQL(client, db)
			includeSubscriptions := false

			mock.ExpectQuery("count").
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))
			mock.ExpectQuery("page").
				WillReturnRows(sqlmock.NewRows([]string{}))

			users, result, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{
				Page: 1, PageSize: 10,
			}, service.UserListFilters{
				Tier:                 tc.tier,
				IncludeSubscriptions: &includeSubscriptions,
			})
			require.NoError(t, err)
			require.Empty(t, users)
			require.NotNil(t, result)
			require.Equal(t, int64(2), result.Total)
			require.Len(t, queries, 2)
			require.NoError(t, mock.ExpectationsWereMet())

			for _, query := range queries {
				normalized := strings.ToUpper(normalizeSQLWhitespace(query))
				require.Contains(t, normalized, tc.expectedExpr)
				require.Contains(t, normalized, "USER_ENTITLEMENTS")
				require.Contains(t, normalized, "USER_ENTITLEMENT_GRANTS")
				require.Contains(t, normalized, "ENTITLEMENT_TIERS")
				require.Contains(t, normalized, "E.TIER = 'PREMIUM'")
			}
		})
	}
}

func TestUserRepositoryListWithFiltersRejectsUnknownTierBeforeDatabaseAccess(t *testing.T) {
	repo := &userRepository{}
	_, _, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 1, PageSize: 10}, service.UserListFilters{Tier: "gold"})
	require.ErrorIs(t, err, service.ErrInvalidUserListTier)
}
