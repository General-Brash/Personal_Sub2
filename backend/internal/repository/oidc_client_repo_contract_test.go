package repository

import (
	"context"
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
