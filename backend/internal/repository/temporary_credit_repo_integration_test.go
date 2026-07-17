//go:build integration

package repository

import (
	"context"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTemporaryCreditRepositoryConsumeFEFOUsesCallerTransaction(t *testing.T) {
	ctx := context.Background()
	repo := NewTemporaryCreditRepository(integrationDB)
	user := newTemporaryCreditTestUser(t)

	earlier := createTemporaryCreditTestGrant(t, repo, user.ID, "1.00000000")
	later := createTemporaryCreditTestGrant(t, repo, user.ID, "2.00000000")
	reference := temporaryCreditTestReference(t, "fefo-request")

	tx := testTx(t)
	remaining, err := repo.ConsumeFEFO(ctx, tx, user.ID, 1.5, reference)
	require.NoError(t, err)
	require.InDelta(t, 0, remaining, 1e-12)

	var earlierRemaining, laterRemaining string
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT remaining_amount::text FROM temporary_credit_grants WHERE id = $1", earlier.ID).Scan(&earlierRemaining))
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT remaining_amount::text FROM temporary_credit_grants WHERE id = $1", later.ID).Scan(&laterRemaining))
	require.Equal(t, "0.00000000", earlierRemaining)
	require.Equal(t, "1.50000000", laterRemaining)
	require.NoError(t, tx.Commit())

	var consumptionCount int
	var persistedRequestID string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM temporary_credit_consumptions WHERE grant_id IN ($1, $2)", earlier.ID, later.ID).Scan(&consumptionCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT request_id FROM temporary_credit_consumptions WHERE grant_id = $1", earlier.ID).Scan(&persistedRequestID))
	require.Equal(t, 2, consumptionCount)
	require.Equal(t, reference.RequestID, persistedRequestID)
}

func TestTemporaryCreditRepositoryCreateGrantTxRollsBackWithCallerTransaction(t *testing.T) {
	ctx := context.Background()
	repo := NewTemporaryCreditRepository(integrationDB)
	user := newTemporaryCreditTestUser(t)
	adminID := user.ID

	tx := testTx(t)
	grant, err := service.NewTemporaryCreditService(repo).CreateGrantTx(ctx, tx, service.CreateTemporaryCreditGrantInput{
		UserID:    user.ID,
		Source:    service.TemporaryCreditSourceAdminGrant,
		Amount:    1,
		GrantedBy: &adminID,
	})
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM temporary_credit_grants WHERE id = $1", grant.ID).Scan(&count))
	require.Zero(t, count)
}

func TestTemporaryCreditRepositoryConsumeFEFORollsBackGrantAndConsumptionWithCallerTransaction(t *testing.T) {
	ctx := context.Background()
	repo := NewTemporaryCreditRepository(integrationDB)
	user := newTemporaryCreditTestUser(t)
	grant := createTemporaryCreditTestGrant(t, repo, user.ID, "1.00000000")

	tx := testTx(t)
	remaining, err := repo.ConsumeFEFO(ctx, tx, user.ID, 0.4, temporaryCreditTestReference(t, "rollback-request"))
	require.NoError(t, err)
	require.InDelta(t, 0, remaining, 1e-12)
	require.NoError(t, tx.Rollback())

	var remainingAmount string
	var consumptionCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT remaining_amount::text FROM temporary_credit_grants WHERE id = $1", grant.ID).Scan(&remainingAmount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM temporary_credit_consumptions WHERE grant_id = $1", grant.ID).Scan(&consumptionCount))
	require.Equal(t, "1.00000000", remainingAmount)
	require.Zero(t, consumptionCount)
}

func TestTemporaryCreditRepositoryCreateGrantRejectsAmountsThatWouldRoundIntoNumericRange(t *testing.T) {
	ctx := context.Background()
	repo := NewTemporaryCreditRepository(integrationDB)
	user := newTemporaryCreditTestUser(t)

	_, err := service.NewTemporaryCreditService(repo).CreateGrant(ctx, service.CreateTemporaryCreditGrantInput{
		UserID: user.ID,
		Source: service.TemporaryCreditSourceAdminGrant,
		Amount: 1_000_000_000_000,
	})

	require.Error(t, err)
}

func TestTemporaryCreditRepositoryMigrationConstraintsAndDuplicateReferenceRollback(t *testing.T) {
	ctx := context.Background()
	assertTemporaryCreditForeignKeyAction(t, "daily_checkins", "user_id", "r")
	assertTemporaryCreditForeignKeyAction(t, "temporary_credit_grants", "user_id", "r")
	assertTemporaryCreditForeignKeyAction(t, "temporary_credit_grants", "checkin_id", "r")
	assertTemporaryCreditForeignKeyAction(t, "temporary_credit_grants", "granted_by", "r")
	assertTemporaryCreditForeignKeyAction(t, "temporary_credit_consumptions", "grant_id", "r")
	assertTemporaryCreditForeignKeyAction(t, "temporary_credit_consumptions", "usage_log_id", "r")

	var requestIDLength int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT character_maximum_length
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = 'temporary_credit_consumptions'
  AND column_name = 'request_id'`).Scan(&requestIDLength))
	require.Equal(t, 255, requestIDLength)

	repo := NewTemporaryCreditRepository(integrationDB)
	user := newTemporaryCreditTestUser(t)
	_, err := integrationDB.ExecContext(ctx, `
INSERT INTO temporary_credit_grants
    (user_id, source, amount, remaining_amount, expires_at, notes)
VALUES ($1, 'admin_grant', 1, 1, clock_timestamp() + INTERVAL '1 hour', '')`, user.ID)
	require.Error(t, err, "admin grants must retain a non-null grantor")
	grant := createTemporaryCreditTestGrant(t, repo, user.ID, "1.00000000")

	_, err = integrationDB.ExecContext(ctx, `
INSERT INTO temporary_credit_consumptions (grant_id, amount)
VALUES ($1, $2)`, grant.ID, "0.10000000")
	require.Error(t, err, "the XOR reference constraint must reject untraceable consumption")

	reference := temporaryCreditTestReference(t, "same-external-request")
	firstTx := testTx(t)
	remaining, err := repo.ConsumeFEFO(ctx, firstTx, user.ID, 0.4, reference)
	require.NoError(t, err)
	require.InDelta(t, 0, remaining, 1e-12)
	require.NoError(t, firstTx.Commit())

	duplicateTx := testTx(t)
	_, err = repo.ConsumeFEFO(ctx, duplicateTx, user.ID, 0.4, reference)
	require.Error(t, err, "the same request may not allocate the same grant twice")
	require.NoError(t, duplicateTx.Rollback())

	var remainingAmount string
	var consumptionCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT remaining_amount::text FROM temporary_credit_grants WHERE id = $1", grant.ID).Scan(&remainingAmount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM temporary_credit_consumptions WHERE grant_id = $1", grant.ID).Scan(&consumptionCount))
	require.Equal(t, "0.60000000", remainingAmount)
	require.Equal(t, 1, consumptionCount)
}

func TestTemporaryCreditConsumptionRequestIDIsImmutable(t *testing.T) {
	ctx := context.Background()
	repo := NewTemporaryCreditRepository(integrationDB)
	user := newTemporaryCreditTestUser(t)
	grant := createTemporaryCreditTestGrant(t, repo, user.ID, "1.00000000")
	reference := temporaryCreditTestReference(t, "immutable-request")

	var consumptionID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
INSERT INTO temporary_credit_consumptions (grant_id, request_id, amount)
VALUES ($1, $2, $3)
RETURNING id`, grant.ID, reference.RequestID, "0.10000000").Scan(&consumptionID))

	_, err := integrationDB.ExecContext(ctx, `
UPDATE temporary_credit_consumptions
SET request_id = $1
WHERE id = $2`, "replacement-request", consumptionID)
	require.ErrorContains(t, err, "temporary credit consumption request_id is immutable")

	_, err = integrationDB.ExecContext(ctx, `
UPDATE temporary_credit_consumptions
SET request_id = NULL
WHERE id = $1`, consumptionID)
	require.ErrorContains(t, err, "temporary credit consumption request_id is immutable")

	var persistedRequestID string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT request_id
FROM temporary_credit_consumptions
WHERE id = $1`, consumptionID).Scan(&persistedRequestID))
	require.Equal(t, reference.RequestID, persistedRequestID)

	_, err = integrationDB.ExecContext(ctx, `
UPDATE temporary_credit_consumptions
SET amount = $1
WHERE id = $2`, "0.20000000", consumptionID)
	require.NoError(t, err)
}

func newTemporaryCreditTestUser(t *testing.T) *service.User {
	t.Helper()
	ctx := context.Background()
	user := mustCreateUser(t, testEntClient(t), &service.User{
		Email:        "temporary-credit-" + uuid.NewString() + "@example.com",
		PasswordHash: "hash",
	})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `
DELETE FROM temporary_credit_consumptions
WHERE grant_id IN (SELECT id FROM temporary_credit_grants WHERE user_id = $1)`, user.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM temporary_credit_grants WHERE user_id = $1", user.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM daily_checkins WHERE user_id = $1", user.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	return user
}

func createTemporaryCreditTestGrant(t *testing.T, repo service.TemporaryCreditRepository, userID int64, amount string) *service.TemporaryCreditGrant {
	t.Helper()
	parsedAmount, err := strconv.ParseFloat(amount, 64)
	require.NoError(t, err)
	grant, err := service.NewTemporaryCreditService(repo).CreateGrant(context.Background(), service.CreateTemporaryCreditGrantInput{
		UserID:    userID,
		Source:    service.TemporaryCreditSourceAdminGrant,
		Amount:    parsedAmount,
		GrantedBy: &userID,
	})
	require.NoError(t, err)
	return grant
}

func temporaryCreditTestReference(t *testing.T, externalRequestID string) service.TemporaryCreditConsumptionReference {
	t.Helper()
	return service.TemporaryCreditConsumptionReference{RequestID: externalRequestID}
}

func assertTemporaryCreditForeignKeyAction(t *testing.T, tableName, columnName, expectedAction string) {
	t.Helper()
	var action string
	require.NoError(t, integrationDB.QueryRow(`
SELECT constraint_row.confdeltype
FROM pg_constraint AS constraint_row
JOIN pg_attribute AS attribute_row
  ON attribute_row.attrelid = constraint_row.conrelid
 AND attribute_row.attnum = ANY(constraint_row.conkey)
WHERE constraint_row.conrelid = $1::regclass
  AND constraint_row.contype = 'f'
  AND attribute_row.attname = $2`, tableName, columnName).Scan(&action))
	require.Equal(t, expectedAction, action)
}
