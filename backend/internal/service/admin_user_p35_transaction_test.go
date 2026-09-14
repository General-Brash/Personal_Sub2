package service

import (
	"context"
	"errors"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

type p35AtomicUserRepo struct {
	UserRepository
	tx        *dbent.Tx
	emptyMask bool
}

func (r *p35AtomicUserRepo) GetByID(context.Context, int64) (*User, error) {
	return &User{ID: 7, Role: RoleUser, Status: StatusActive}, nil
}
func (r *p35AtomicUserRepo) Update(ctx context.Context, _ *User, fields UserUpdateFields) error {
	r.tx = dbent.TxFromContext(ctx)
	r.emptyMask = fields.IsEmpty()
	return nil
}

type p35AtomicRateRepo struct {
	UserGroupRateRepository
	tx  *dbent.Tx
	err error
}

func (r *p35AtomicRateRepo) SyncUserGroupRates(ctx context.Context, _ int64, _ map[int64]*float64) error {
	r.tx = dbent.TxFromContext(ctx)
	return r.err
}

func TestAdminUserRateOnlyUpdateSharesAuthorizationTransaction(t *testing.T) {
	t.Setenv("ADMIN_PERMISSIONS_MODE", AdminPermissionModeDisabled)
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[fail], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			users := &p35AtomicUserRepo{}
			rates := &p35AtomicRateRepo{}
			if fail {
				rates.err = errors.New("rate update failed")
			}
			svc := &adminServiceImpl{userRepo: users, userGroupRateRepo: rates, entClient: client}
			mock.ExpectBegin()
			if fail {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			rate := 1.5
			_, err = svc.UpdateUser(context.Background(), 7, &UpdateUserInput{ActorAdminID: 1, GroupRates: map[int64]*float64{2: &rate}})
			if fail {
				require.ErrorIs(t, err, rates.err)
			} else {
				require.NoError(t, err)
			}
			require.True(t, users.emptyMask)
			require.NotNil(t, users.tx)
			require.Same(t, users.tx, rates.tx)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
