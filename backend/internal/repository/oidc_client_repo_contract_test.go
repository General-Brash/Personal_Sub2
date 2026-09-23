package repository

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOIDCAuthenticateClientQualifiesJoinedClientColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	clientRows := sqlmock.NewRows([]string{
		"id", "client_id", "name", "owner", "client_type", "enabled", "trusted_skip_consent",
		"policy_version", "version", "created_at", "updated_at", "disabled_at", "disabled_reason",
	}).AddRow(42, "sub2-client", "client", "owner", "confidential", true, false, 1, 1, now, now, nil, "")
	mock.ExpectQuery(`(?s)SELECT c\.id,c\.client_id,c\.name,c\.owner,c\.client_type,c\.enabled,c\.trusted_skip_consent,c\.policy_version,c\.version,c\.created_at,c\.updated_at,c\.disabled_at,c\.disabled_reason.*FROM oidc_clients c JOIN oidc_client_secrets s`).
		WithArgs("sub2-client", "digest", now).
		WillReturnRows(clientRows)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT redirect_uri FROM oidc_client_redirect_uris WHERE client_pk=$1 AND enabled=TRUE ORDER BY id")).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"redirect_uri"}).AddRow("https://client.example/callback"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT scope FROM oidc_client_scopes WHERE client_pk=$1 AND enabled=TRUE ORDER BY id")).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"scope"}).AddRow("openid"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id,client_pk,fingerprint,status,not_before,expires_at,created_at,revoked_at FROM oidc_client_secrets WHERE client_pk=$1 ORDER BY id DESC")).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "client_pk", "fingerprint", "status", "not_before", "expires_at", "created_at", "revoked_at"}).
			AddRow(7, 42, "fingerprint", "active", now, now.Add(time.Hour), now, nil))

	client, err := (&oidcRepository{db: db}).AuthenticateClient(context.TODO(), "sub2-client", "digest", now)
	require.NoError(t, err)
	require.Equal(t, int64(42), client.ID)
	require.Equal(t, "sub2-client", client.ClientID)
	require.NoError(t, mock.ExpectationsWereMet())
}

// The client row lock serializes rotations; the UPDATE must never extend an
// earlier expiry or revive an already expired active secret.
func TestOIDCSecretRotationClampsOldExpiryAndRollsBackAtCapacity(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprintf("remaining_%d", count), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
			until := now.Add(90 * 24 * time.Hour)
			overlap := now.Add(time.Hour)
			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM oidc_clients WHERE id=$1 FOR UPDATE")).WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
			mock.ExpectExec(regexp.QuoteMeta("UPDATE oidc_client_secrets SET status='expired' WHERE client_pk=$1 AND status IN ('active','retiring') AND expires_at <= $2")).WithArgs(int64(42), now).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(regexp.QuoteMeta("UPDATE oidc_client_secrets SET status='retiring', expires_at=LEAST(expires_at,$3) WHERE client_pk=$1 AND status='active' AND expires_at > $2")).WithArgs(int64(42), now, overlap).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM oidc_client_secrets WHERE client_pk=$1 AND status IN ('active','retiring') AND expires_at > $2")).WithArgs(int64(42), now).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
			if count == 1 {
				mock.ExpectQuery(`INSERT INTO oidc_client_secrets`).WithArgs(int64(42), "digest", "fingerprint", now, until, int64(7), "rotation").WillReturnRows(sqlmock.NewRows([]string{"id", "client_pk", "fingerprint", "status", "not_before", "expires_at", "created_at", "revoked_at"}).AddRow(2, 42, "fingerprint", "active", now, until, now, nil))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			secret, err := (&oidcRepository{db: db}).CreateClientSecretWithOverlap(context.Background(), 42, 7, "digest", "fingerprint", now, until, overlap, "rotation")
			if count == 1 {
				require.NoError(t, err)
				require.Equal(t, until, secret.ExpiresAt)
			} else {
				require.ErrorContains(t, err, "too many active secrets")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestOIDCSecretRotationRejectsOverlapBeyond24Hours(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	_, err := (&oidcRepository{}).CreateClientSecretWithOverlap(context.Background(), 42, 7, "digest", "fingerprint", now, now.Add(90*24*time.Hour), now.Add(24*time.Hour+time.Second), "rotation")
	require.ErrorContains(t, err, "invalid oidc secret rotation window")
}

func TestOIDCAuthenticateClientRejectsExpiredBasicSecret(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM oidc_clients c JOIN oidc_client_secrets s`).WithArgs("sub2-client", "digest", now).WillReturnError(sql.ErrNoRows)
	_, err = (&oidcRepository{db: db}).AuthenticateClient(context.Background(), "sub2-client", "digest", now)
	require.ErrorIs(t, err, sql.ErrNoRows)
	require.NoError(t, mock.ExpectationsWereMet())
}
