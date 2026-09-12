package service

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/stretchr/testify/require"
)

func TestLockRegistrationInvitationSourcesDialectBehavior(t *testing.T) {
	ctx := context.Background()
	calls := 0
	var query string
	var args []any
	exec := func(_ context.Context, gotQuery string, gotArgs ...any) (sql.Result, error) {
		calls++
		query = gotQuery
		args = gotArgs
		return nil, nil
	}

	require.NoError(t, lockRegistrationInvitationSources(ctx, dialect.SQLite, exec))
	require.Zero(t, calls, "SQLite unit tests have no PostgreSQL advisory transaction lock")

	require.NoError(t, lockRegistrationInvitationSources(ctx, dialect.Postgres, exec))
	require.Equal(t, 1, calls)
	require.Equal(t, `SELECT pg_advisory_xact_lock($1)`, query)
	require.Equal(t, []any{registrationInvitationAdvisoryLockID}, args)

	require.Error(t, lockRegistrationInvitationSources(ctx, "unsupported", exec))
	require.Error(t, lockRegistrationInvitationSources(ctx, dialect.Postgres, nil))
	require.Equal(t, 1, calls, "unsupported dialects and missing executors must fail closed")
}
