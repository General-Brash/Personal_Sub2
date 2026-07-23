package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

const adminBankLedgerPageSize = 20

// BankAdminLedgerItem is the administrator-facing bank transaction row. The
// balance snapshots are nullable for rows written before migration 190.
type BankAdminLedgerItem struct {
	ID                     int64          `json:"id"`
	RowID                  string         `json:"row_id"`
	Source                 string         `json:"source"`
	Currency               string         `json:"currency"`
	Unit                   string         `json:"unit"`
	UserID                 int64          `json:"user_id"`
	Username               string         `json:"username"`
	Email                  string         `json:"email"`
	Operation              string         `json:"operation"`
	TransactionAmount      string         `json:"transaction_amount"`
	LoanID                 *int64         `json:"loan_id,omitempty"`
	GrantID                *int64         `json:"grant_id,omitempty"`
	PermanentDelta         string         `json:"permanent_delta"`
	TemporaryDelta         string         `json:"temporary_delta"`
	DebtDelta              string         `json:"debt_delta"`
	DebtBefore             string         `json:"debt_before"`
	DebtAfter              string         `json:"debt_after"`
	PermanentBalanceBefore *string        `json:"permanent_balance_before,omitempty"`
	PermanentBalanceAfter  *string        `json:"permanent_balance_after,omitempty"`
	TemporaryBalanceBefore *string        `json:"temporary_balance_before,omitempty"`
	TemporaryBalanceAfter  *string        `json:"temporary_balance_after,omitempty"`
	Metadata               map[string]any `json:"metadata,omitempty"`
	CreatedAt              time.Time      `json:"created_at"`
}

func (s *BankService) ListAdminLedger(ctx context.Context, userID int64, page int) ([]BankAdminLedgerItem, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("bank service database is nil")
	}
	if page < 1 {
		page = 1
	}
	var total int64
	countQuery := `SELECT COUNT(*) FROM bank_ledger`
	countArgs := []any{}
	if userID > 0 {
		countQuery += ` WHERE user_id = $1`
		countArgs = append(countArgs, userID)
	}
	if err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count admin bank ledger: %w", err)
	}
	query := `
SELECT ledger.id, ledger.user_id,
       COALESCE(NULLIF(users.username, ''), users.email), users.email,
       ledger.operation, ledger.loan_id, ledger.grant_id,
       ledger.permanent_delta::text, ledger.temporary_delta::text,
       ledger.debt_delta::text, ledger.debt_before::text, ledger.debt_after::text,
       ledger.permanent_balance_before::text, ledger.permanent_balance_after::text,
       ledger.temporary_balance_before::text, ledger.temporary_balance_after::text,
       ledger.metadata, ledger.created_at
FROM bank_ledger AS ledger
JOIN users ON users.id = ledger.user_id
`
	args := make([]any, 0, 3)
	if userID > 0 {
		query += "WHERE ledger.user_id = $1\n"
		args = append(args, userID)
	}
	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	query += fmt.Sprintf("ORDER BY ledger.created_at DESC, ledger.id DESC LIMIT $%d OFFSET $%d", limitArg, offsetArg)
	args = append(args, adminBankLedgerPageSize, (page-1)*adminBankLedgerPageSize)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list admin bank ledger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]BankAdminLedgerItem, 0, adminBankLedgerPageSize)
	for rows.Next() {
		var item BankAdminLedgerItem
		var loanID, grantID sql.NullInt64
		var permanent, temporary, debtDelta, debtBefore, debtAfter string
		var permanentBefore, permanentAfter, temporaryBefore, temporaryAfter sql.NullString
		var metadataRaw []byte
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.Email, &item.Operation,
			&loanID, &grantID, &permanent, &temporary, &debtDelta, &debtBefore, &debtAfter,
			&permanentBefore, &permanentAfter, &temporaryBefore, &temporaryAfter, &metadataRaw, &item.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan admin bank ledger: %w", err)
		}
		item.PermanentDelta = normalizeLedgerText(permanent)
		item.RowID = financialLedgerRowID("bank", item.ID)
		item.Source = "bank"
		item.Unit = LedgerUnitCredit
		item.TemporaryDelta = normalizeLedgerText(temporary)
		item.DebtDelta = normalizeLedgerText(debtDelta)
		item.TransactionAmount = bankAdminTransactionAmount(item.Operation, permanent, debtDelta)
		item.DebtBefore = normalizeLedgerText(debtBefore)
		item.DebtAfter = normalizeLedgerText(debtAfter)
		if loanID.Valid {
			value := loanID.Int64
			item.LoanID = &value
		}
		if grantID.Valid {
			value := grantID.Int64
			item.GrantID = &value
		}
		item.PermanentBalanceBefore = nullableLedgerText(permanentBefore)
		item.PermanentBalanceAfter = nullableLedgerText(permanentAfter)
		item.TemporaryBalanceBefore = nullableLedgerText(temporaryBefore)
		item.TemporaryBalanceAfter = nullableLedgerText(temporaryAfter)
		if len(metadataRaw) > 0 {
			_ = json.Unmarshal(metadataRaw, &item.Metadata)
		}
		item.CreatedAt = item.CreatedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate admin bank ledger: %w", err)
	}
	return items, total, nil
}

func bankAdminTransactionAmount(operation, permanentDelta, debtDelta string) string {
	raw := debtDelta
	if operation == "exchange" {
		raw = permanentDelta
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return normalizeLedgerText(raw)
	}
	return formatLedgerAmount(math.Abs(value))
}

func normalizeLedgerText(raw string) string {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return raw
	}
	return formatLedgerAmount(value)
}

func nullableLedgerText(value sql.NullString) *string {
	if !value.Valid || value.String == "" {
		return nil
	}
	normalized := normalizeLedgerText(value.String)
	return &normalized
}
