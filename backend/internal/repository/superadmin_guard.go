package repository

import (
	"context"
	"errors"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const superAdminGuardLockKey int64 = 0x5355423241504931 // SUB2API1

func ensureNotLastSuperAdminWithClient(ctx context.Context, client *dbent.Client) error {
	if client == nil {
		return errors.New("super-admin guard client is nil")
	}
	// All last-super-admin checks use one transaction-scoped mutex. This avoids
	// cross-row lock inversion when two demotions/disables/deletions race and
	// makes the count-then-write sequence serial within one PostgreSQL tx.
	if _, err := client.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, superAdminGuardLockKey); err != nil {
		return err
	}
	rows, err := client.User.Query().
		Where(
			dbuser.RoleEQ(service.RoleSuperAdmin),
			dbuser.StatusEQ(service.StatusActive),
			dbuser.DeletedAtIsNil(),
		).
		Order(dbent.Asc(dbuser.FieldID)).
		ForUpdate().
		All(ctx)
	if err != nil {
		return err
	}
	if len(rows) <= 1 {
		return service.ErrLastSuperAdmin
	}
	return nil
}
