package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

var errTemporaryCreditEntTransactionExecutorUnavailable = errors.New("ent transaction does not expose a SQL executor")

// entTemporaryCreditExecutor preserves the SQL executor dynamically bound to
// an Ent transaction. It does not start, commit, or roll back a transaction.
type entTemporaryCreditExecutor struct {
	executor TemporaryCreditSQLExecutor
}

var _ TemporaryCreditSQLExecutor = (*entTemporaryCreditExecutor)(nil)

func temporaryCreditExecutorFromEntTx(tx *dbent.Tx) (TemporaryCreditSQLExecutor, error) {
	if tx == nil {
		return nil, ErrTemporaryCreditTransactionRequired
	}
	executor, ok := tx.Client().Driver().(TemporaryCreditSQLExecutor)
	if !ok {
		return nil, errTemporaryCreditEntTransactionExecutorUnavailable
	}
	return &entTemporaryCreditExecutor{executor: executor}, nil
}

func (e *entTemporaryCreditExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if e == nil || e.executor == nil {
		return nil, fmt.Errorf("query temporary credit through ent transaction: %w", errTemporaryCreditEntTransactionExecutorUnavailable)
	}
	return e.executor.QueryContext(ctx, query, args...)
}

func (e *entTemporaryCreditExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if e == nil || e.executor == nil {
		return nil, fmt.Errorf("execute temporary credit through ent transaction: %w", errTemporaryCreditEntTransactionExecutorUnavailable)
	}
	return e.executor.ExecContext(ctx, query, args...)
}
