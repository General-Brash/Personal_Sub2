package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	SettingKeyBankExchangeExpiryRefundEnabled       = "bank_exchange_expiry_refund_enabled"
	SettingKeyBankExchangeExpiryRefundFeeBPS        = "bank_exchange_expiry_refund_fee_bps"
	SettingKeyBankExchangeExpiryRefundPolicyVersion = "bank_exchange_expiry_refund_policy_version"
	SettingKeyBankExchangeExpiryLocalTime           = "bank_exchange_expiry_local_time"

	BankExchangeExpiryEligibilityEligible     = "eligible"
	BankExchangeExpiryEligibilityIneligible   = "ineligible"
	BankExchangeExpiryEligibilityManualReview = "manual_review"

	bankExchangeExpiryTimezone = "Asia/Shanghai"
)

var (
	ErrBankExchangeExpiryPolicyInvalid         = infraerrors.BadRequest("INVALID_BANK_EXCHANGE_EXPIRY_POLICY", "bank exchange expiry policy is invalid")
	ErrBankExchangeExpiryPolicyVersionRequired = infraerrors.BadRequest("BANK_EXCHANGE_EXPIRY_POLICY_VERSION_REQUIRED", "bank exchange expiry policy version confirmation is required")
	ErrBankExchangeExpiryPolicyVersionConflict = infraerrors.Conflict("BANK_EXCHANGE_EXPIRY_POLICY_VERSION_CONFLICT", "bank exchange expiry policy version is stale")
	ErrBankExchangeExpiryQuoteConflict         = infraerrors.Conflict("BANK_EXCHANGE_EXPIRY_QUOTE_CONFLICT", "bank exchange expiry quote does not match the current policy")
)

type BankExchangeExpiryPolicyDTO struct {
	Enabled       bool      `json:"enabled"`
	FeeBPS        int       `json:"fee_bps"`
	FeeRate       string    `json:"fee_rate"`
	Timezone      string    `json:"timezone"`
	LocalTime     string    `json:"local_time"`
	PolicyVersion int64     `json:"policy_version"`
	UpdatedAt     time.Time `json:"updated_at"`
	DefaultFeeBPS int       `json:"default_fee_bps"`
	NewGrantsOnly bool      `json:"new_grants_only"`
}

// BankExchangeQuoteConfirmation is the client-visible fee/expiry snapshot a
// client can include when confirming an exchange.
type BankExchangeQuoteConfirmation struct {
	PolicyVersion int64  `json:"policy_version"`
	FeeBPS        int    `json:"fee_bps"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

// BankExchangeConfirmation binds an exchange to the policy the user saw.
// PolicyVersion is sufficient for legacy clients; Quote adds fee and expiry
// checks when the caller has a full preview.
type BankExchangeConfirmation struct {
	PolicyVersion int64                          `json:"policy_version,omitempty"`
	Quote         *BankExchangeQuoteConfirmation `json:"quote,omitempty"`
}

type BankExchangeCommitment struct {
	GrantID                     int64     `json:"grant_id"`
	PrincipalPermanent          string    `json:"principal_permanent"`
	GeneratedTemporary          string    `json:"generated_temporary"`
	RemainingTemporary          string    `json:"remaining_temporary"`
	RefundablePrincipalEstimate string    `json:"refundable_principal_estimate"`
	FeeEstimate                 string    `json:"fee_estimate"`
	NetRefundEstimate           string    `json:"net_refund_estimate"`
	FeeBPS                      int       `json:"fee_bps"`
	PolicyVersion               int64     `json:"policy_version"`
	Eligibility                 string    `json:"eligibility"`
	Status                      string    `json:"status"`
	ExpiresAt                   time.Time `json:"expires_at"`
}

type BankExchangeSettlementItem struct {
	ID                  int64     `json:"id"`
	EventID             string    `json:"event_id"`
	GrantID             int64     `json:"grant_id"`
	ExpiredRemaining    string    `json:"expired_remaining"`
	RefundablePrincipal string    `json:"refundable_principal"`
	FeeAmount           string    `json:"fee_amount"`
	NetRefund           string    `json:"net_refund"`
	Status              string    `json:"status"`
	Reason              string    `json:"reason"`
	PolicyVersion       int64     `json:"policy_version"`
	SettledAt           time.Time `json:"settled_at"`
}

type bankExchangeRefundCalculation struct {
	ExpiredRemaining    string
	RefundablePrincipal string
	FeeAmount           string
	NetRefund           string
}

type bankExchangeSettlementCandidate struct {
	GrantID             int64
	PrincipalPermanent  string
	GeneratedTemporary  string
	RemainingTemporary  string
	FeeBPS              int
	PolicyVersion       int64
	RefundableTemporary string
}

func applyBankExchangeExpiryPolicySettings(policy BankPolicy, values map[string]string) (BankPolicy, error) {
	if raw, ok := values[SettingKeyBankExchangeExpiryRefundEnabled]; ok {
		enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return BankPolicy{}, ErrBankExchangeExpiryPolicyInvalid
		}
		policy.exchangeExpiryRefundEnabled = enabled
	}
	if raw, ok := values[SettingKeyBankExchangeExpiryRefundFeeBPS]; ok {
		feeBPS, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || feeBPS < 0 || feeBPS > 10000 {
			return BankPolicy{}, ErrBankExchangeExpiryPolicyInvalid
		}
		policy.exchangeExpiryRefundFeeBPS = feeBPS
	}
	if raw, ok := values[SettingKeyBankExchangeExpiryRefundPolicyVersion]; ok {
		version, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || version <= 0 {
			return BankPolicy{}, ErrBankExchangeExpiryPolicyInvalid
		}
		policy.exchangeExpiryPolicyVersion = version
	}
	if raw, ok := values[SettingKeyBankExchangeExpiryLocalTime]; ok {
		if _, err := parseBankExchangeExpiryLocalTime(raw); err != nil {
			return BankPolicy{}, ErrBankExchangeExpiryPolicyInvalid
		}
		policy.exchangeExpiryLocalTime = strings.TrimSpace(raw)
	}
	return policy, nil
}

func (p BankPolicy) exchangeExpiryPolicyDTO(updatedAt time.Time) BankExchangeExpiryPolicyDTO {
	return BankExchangeExpiryPolicyDTO{
		Enabled:       p.exchangeExpiryRefundEnabled,
		FeeBPS:        p.exchangeExpiryRefundFeeBPS,
		FeeRate:       bankExchangeFeeRate(p.exchangeExpiryRefundFeeBPS),
		Timezone:      bankExchangeExpiryTimezone,
		LocalTime:     p.exchangeExpiryLocalTime,
		PolicyVersion: p.exchangeExpiryPolicyVersion,
		UpdatedAt:     updatedAt.UTC(),
		DefaultFeeBPS: 1000,
		NewGrantsOnly: true,
	}
}

func (s *BankService) GetExchangeExpiryPolicy(ctx context.Context) (*BankExchangeExpiryPolicyDTO, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("bank service database is nil")
	}
	policy, err := loadBankPolicy(ctx, s.db)
	if err != nil {
		return nil, err
	}
	var updatedAt time.Time
	if err := s.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&updatedAt); err != nil {
		return nil, fmt.Errorf("load bank exchange expiry policy clock: %w", err)
	}
	result := policy.exchangeExpiryPolicyDTO(updatedAt)
	return &result, nil
}

func (s *BankService) UpdateExchangeExpiryPolicyAtomic(
	ctx context.Context,
	actorID int64,
	dto BankExchangeExpiryPolicyDTO,
	claim *IdempotencyAtomicClaim,
) (*BankExchangeExpiryPolicyDTO, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("bank service database is nil")
	}
	if actorID <= 0 || claim == nil {
		return nil, ErrIdempotencyStoreUnavail
	}
	if dto.FeeBPS < 0 || dto.FeeBPS > 10000 {
		return nil, ErrBankExchangeExpiryPolicyInvalid
	}
	if dto.Timezone != "" && dto.Timezone != bankExchangeExpiryTimezone {
		return nil, ErrBankExchangeExpiryPolicyInvalid
	}
	if _, err := parseBankExchangeExpiryLocalTime(dto.LocalTime); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bank exchange expiry policy transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(78231702)); err != nil {
		return nil, err
	}
	current, err := loadBankPolicy(ctx, tx)
	if err != nil {
		return nil, err
	}
	nextVersion := current.exchangeExpiryPolicyVersion + 1
	if dto.PolicyVersion <= 0 || dto.PolicyVersion != current.exchangeExpiryPolicyVersion {
		return nil, ErrBankExchangeExpiryPolicyVersionConflict
	}
	var updatedAt time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&updatedAt); err != nil {
		return nil, fmt.Errorf("sample bank exchange expiry policy clock: %w", err)
	}
	values := map[string]string{
		SettingKeyBankExchangeExpiryRefundEnabled:       strconv.FormatBool(dto.Enabled),
		SettingKeyBankExchangeExpiryRefundFeeBPS:        strconv.Itoa(dto.FeeBPS),
		SettingKeyBankExchangeExpiryRefundPolicyVersion: strconv.FormatInt(nextVersion, 10),
		SettingKeyBankExchangeExpiryLocalTime:           strings.TrimSpace(dto.LocalTime),
	}
	for _, key := range []string{
		SettingKeyBankExchangeExpiryRefundEnabled,
		SettingKeyBankExchangeExpiryRefundFeeBPS,
		SettingKeyBankExchangeExpiryRefundPolicyVersion,
		SettingKeyBankExchangeExpiryLocalTime,
	} {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO settings (key, value, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`, key, values[key], updatedAt); err != nil {
			return nil, fmt.Errorf("write bank exchange expiry setting %s: %w", key, err)
		}
	}
	result := BankExchangeExpiryPolicyDTO{
		Enabled:       dto.Enabled,
		FeeBPS:        dto.FeeBPS,
		FeeRate:       bankExchangeFeeRate(dto.FeeBPS),
		Timezone:      bankExchangeExpiryTimezone,
		LocalTime:     strings.TrimSpace(dto.LocalTime),
		PolicyVersion: nextVersion,
		UpdatedAt:     updatedAt.UTC(),
		DefaultFeeBPS: 1000,
		NewGrantsOnly: true,
	}
	if err := claim.PersistSuccess(ctx, tx, &result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bank exchange expiry policy transaction: %w", err)
	}
	return &result, nil
}

func parseBankExchangeExpiryLocalTime(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return time.Time{}, ErrBankExchangeExpiryPolicyInvalid
	}
	return parsed, nil
}

func bankExchangeFeeRate(feeBPS int) string {
	return fmt.Sprintf("%d.%04d", feeBPS/100, feeBPS%100)
}

func validateBankExchangeConfirmation(policy BankPolicy, confirmation BankExchangeConfirmation, expectedExpiresAt time.Time) error {
	version := confirmation.PolicyVersion
	if version < 0 {
		return ErrBankExchangeExpiryPolicyInvalid
	}
	if quote := confirmation.Quote; quote != nil {
		if quote.PolicyVersion <= 0 || quote.FeeBPS < 0 || quote.FeeBPS > 10000 {
			return ErrBankExchangeExpiryPolicyInvalid
		}
		if version != 0 && version != quote.PolicyVersion {
			return ErrBankExchangeExpiryPolicyVersionConflict
		}
		version = quote.PolicyVersion
		if quote.FeeBPS != policy.exchangeExpiryRefundFeeBPS {
			return ErrBankExchangeExpiryQuoteConflict
		}
		if rawExpiresAt := strings.TrimSpace(quote.ExpiresAt); rawExpiresAt != "" {
			quotedExpiresAt, err := time.Parse(time.RFC3339, rawExpiresAt)
			if err != nil {
				return ErrBankExchangeExpiryPolicyInvalid
			}
			if !quotedExpiresAt.Equal(expectedExpiresAt) {
				return ErrBankExchangeExpiryQuoteConflict
			}
		}
	}
	if version == 0 {
		if policy.exchangeExpiryRefundEnabled {
			return ErrBankExchangeExpiryPolicyVersionRequired
		}
		return nil
	}
	if version != policy.exchangeExpiryPolicyVersion {
		return ErrBankExchangeExpiryPolicyVersionConflict
	}
	return nil
}

func nextBankExchangeExpiry(now time.Time, localTime string) (time.Time, error) {
	parsed, err := parseBankExchangeExpiryLocalTime(localTime)
	if err != nil {
		return time.Time{}, err
	}
	location, err := time.LoadLocation(bankExchangeExpiryTimezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load bank exchange expiry timezone: %w", err)
	}
	businessNow := now.In(location)
	expiry := time.Date(businessNow.Year(), businessNow.Month(), businessNow.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location)
	if !expiry.After(businessNow) {
		expiry = expiry.AddDate(0, 0, 1)
	}
	return expiry, nil
}

func calculateBankExchangeRefundFixed8(principal, remaining, generated string, feeBPS int) (bankExchangeRefundCalculation, error) {
	if feeBPS < 0 || feeBPS > 10000 {
		return bankExchangeRefundCalculation{}, ErrBankExchangeExpiryPolicyInvalid
	}
	principalUnits, err := parseBankFixed8(principal)
	if err != nil || principalUnits.Sign() <= 0 {
		return bankExchangeRefundCalculation{}, ErrBankExchangeExpiryPolicyInvalid
	}
	generatedUnits, err := parseBankFixed8(generated)
	if err != nil || generatedUnits.Sign() <= 0 {
		return bankExchangeRefundCalculation{}, ErrBankExchangeExpiryPolicyInvalid
	}
	remainingUnits, err := parseBankFixed8(remaining)
	if err != nil || remainingUnits.Sign() < 0 || remainingUnits.Cmp(generatedUnits) > 0 {
		return bankExchangeRefundCalculation{}, ErrBankExchangeExpiryPolicyInvalid
	}
	refundable := new(big.Int).Mul(principalUnits, remainingUnits)
	refundable.Quo(refundable, generatedUnits)
	fee := new(big.Int).Mul(refundable, big.NewInt(int64(feeBPS)))
	fee.Quo(fee, big.NewInt(10000))
	net := new(big.Int).Sub(new(big.Int).Set(refundable), fee)
	return bankExchangeRefundCalculation{
		ExpiredRemaining:    formatBankFixed8(remainingUnits),
		RefundablePrincipal: formatBankFixed8(refundable),
		FeeAmount:           formatBankFixed8(fee),
		NetRefund:           formatBankFixed8(net),
	}, nil
}

func parseBankFixed8(raw string) (*big.Int, error) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.ContainsAny(value, "eE") {
		return nil, ErrBankExchangeExpiryPolicyInvalid
	}
	negative := strings.HasPrefix(value, "-")
	if negative {
		value = strings.TrimPrefix(value, "-")
	}
	value = strings.TrimPrefix(value, "+")
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return nil, ErrBankExchangeExpiryPolicyInvalid
	}
	whole := parts[0]
	for _, char := range whole {
		if char < '0' || char > '9' {
			return nil, ErrBankExchangeExpiryPolicyInvalid
		}
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 8 {
		return nil, ErrBankExchangeExpiryPolicyInvalid
	}
	for _, char := range fraction {
		if char < '0' || char > '9' {
			return nil, ErrBankExchangeExpiryPolicyInvalid
		}
	}
	fraction += strings.Repeat("0", 8-len(fraction))
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	units, ok := new(big.Int).SetString(whole+fraction, 10)
	if !ok {
		return nil, ErrBankExchangeExpiryPolicyInvalid
	}
	if negative {
		units.Neg(units)
	}
	return units, nil
}

func formatBankFixed8(units *big.Int) string {
	if units == nil || units.Sign() == 0 {
		return "0.00000000"
	}
	sign := ""
	value := new(big.Int).Set(units)
	if value.Sign() < 0 {
		sign = "-"
		value.Abs(value)
	}
	scale := big.NewInt(100_000_000)
	whole, fraction := new(big.Int), new(big.Int)
	whole.QuoRem(value, scale, fraction)
	return fmt.Sprintf("%s%s.%08s", sign, whole.String(), fraction.String())
}

func insertBankExchangeGrantSnapshotTx(
	ctx context.Context,
	tx *sql.Tx,
	userID, grantID int64,
	principal, generated string,
	policy BankPolicy,
	expiresAt time.Time,
) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO bank_exchange_grant_snapshots
    (grant_id, user_id, principal_permanent, generated_temporary, fee_bps, policy_version, eligibility, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		grantID,
		userID,
		principal,
		generated,
		policy.exchangeExpiryRefundFeeBPS,
		policy.exchangeExpiryPolicyVersion,
		BankExchangeExpiryEligibilityEligible,
		expiresAt,
	); err != nil {
		return fmt.Errorf("record bank exchange grant snapshot: %w", err)
	}
	return nil
}

func settleDueBankExchangeRefundsLocked(ctx context.Context, tx *sql.Tx, userID int64, now time.Time) (bool, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT snapshot.grant_id,
       snapshot.principal_permanent::text,
       snapshot.generated_temporary::text,
       credit_grant.remaining_amount::text,
       (credit_grant.remaining_amount + COALESCE((
           SELECT SUM(a.expired_amount) FROM batch_image_credit_hold_allocations a
           JOIN batch_image_credit_holds h ON h.id=a.hold_id
           WHERE a.grant_id=credit_grant.id AND h.status IN ('captured','released')
       ),0))::text,
       snapshot.fee_bps,
       snapshot.policy_version
FROM bank_exchange_grant_snapshots AS snapshot
JOIN temporary_credit_grants AS credit_grant ON credit_grant.id = snapshot.grant_id
LEFT JOIN bank_exchange_expiry_settlements AS settlement ON settlement.grant_id = snapshot.grant_id
WHERE snapshot.user_id = $1
  AND snapshot.eligibility = 'eligible'
  AND snapshot.expires_at <= $2
  AND settlement.id IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM batch_image_credit_hold_allocations a
      JOIN batch_image_credit_holds h ON h.id=a.hold_id
      WHERE a.grant_id=credit_grant.id AND h.status='reserved'
  )
ORDER BY snapshot.grant_id
LIMIT 100
FOR UPDATE OF snapshot, credit_grant`, userID, now)
	if err != nil {
		return false, fmt.Errorf("list due bank exchange expiry refunds: %w", err)
	}
	candidates := make([]bankExchangeSettlementCandidate, 0)
	for rows.Next() {
		var candidate bankExchangeSettlementCandidate
		if err := rows.Scan(
			&candidate.GrantID,
			&candidate.PrincipalPermanent,
			&candidate.GeneratedTemporary,
			&candidate.RemainingTemporary,
			&candidate.RefundableTemporary,
			&candidate.FeeBPS,
			&candidate.PolicyVersion,
		); err != nil {
			_ = rows.Close()
			return false, fmt.Errorf("scan due bank exchange expiry refund: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, fmt.Errorf("iterate due bank exchange expiry refunds: %w", err)
	}
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("close due bank exchange expiry refunds: %w", err)
	}
	settled := false
	for _, candidate := range candidates {
		calculation, calcErr := calculateBankExchangeRefundFixed8(
			candidate.PrincipalPermanent,
			candidate.RefundableTemporary,
			candidate.GeneratedTemporary,
			candidate.FeeBPS,
		)
		if calcErr != nil {
			if err := markBankExchangeExpiryManualReviewTx(ctx, tx, userID, candidate, now); err != nil {
				return settled, err
			}
			settled = true
			continue
		}
		if err := settleBankExchangeExpiryRefundTx(ctx, tx, userID, candidate, calculation, now); err != nil {
			return settled, err
		}
		settled = true
	}
	return settled, nil
}

func markBankExchangeExpiryManualReviewTx(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	candidate bankExchangeSettlementCandidate,
	now time.Time,
) error {
	if _, err := tx.ExecContext(ctx, `
UPDATE bank_exchange_grant_snapshots
SET eligibility = 'manual_review'
WHERE grant_id = $1 AND user_id = $2`, candidate.GrantID, userID); err != nil {
		return fmt.Errorf("mark bank exchange snapshot manual review: %w", err)
	}
	eventID := bankExchangeExpiryEventID(candidate.GrantID)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO bank_exchange_expiry_settlements
    (event_id, user_id, grant_id, expired_remaining, refundable_principal, fee_amount, net_refund,
     status, reason, policy_version, settled_at, updated_at)
VALUES ($1, $2, $3, 0, 0, 0, 0, 'manual_review', 'invalid_snapshot_or_remaining', $4, $5, $5)
ON CONFLICT (grant_id) DO NOTHING`,
		eventID, userID, candidate.GrantID, candidate.PolicyVersion, now); err != nil {
		return fmt.Errorf("record bank exchange manual review settlement: %w", err)
	}
	return nil
}

func settleBankExchangeExpiryRefundTx(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	candidate bankExchangeSettlementCandidate,
	calculation bankExchangeRefundCalculation,
	now time.Time,
) error {
	result, err := tx.ExecContext(ctx, `
UPDATE temporary_credit_grants
SET remaining_amount = 0,
    updated_at = $1
WHERE id = $2 AND user_id = $3 AND remaining_amount = $4`,
		now, candidate.GrantID, userID, candidate.RemainingTemporary)
	if err != nil {
		return fmt.Errorf("expire bank exchange grant %d: %w", candidate.GrantID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("bank exchange grant %d changed before expiry settlement", candidate.GrantID)
	}
	var balanceAfter, balanceBefore, debt float64
	if err := tx.QueryRowContext(ctx, `
UPDATE users
SET balance = balance + $1,
    updated_at = $2
WHERE id = $3 AND deleted_at IS NULL
RETURNING balance, balance-$1::numeric, temporary_credit_debt`, calculation.NetRefund, now, userID).Scan(&balanceAfter, &balanceBefore, &debt); err != nil {
		return fmt.Errorf("credit bank exchange expiry refund: %w", err)
	}
	balanceAfter, err = normalizeDerivedLedgerAmount(balanceAfter)
	if err != nil {
		return fmt.Errorf("invalid permanent balance after bank exchange expiry refund")
	}
	eventID := bankExchangeExpiryEventID(candidate.GrantID)
	result, err = tx.ExecContext(ctx, `
INSERT INTO bank_exchange_expiry_settlements
    (event_id, user_id, grant_id, expired_remaining, refundable_principal, fee_amount, net_refund,
     status, reason, policy_version, settled_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'settled', '', $8, $9, $9)
ON CONFLICT (grant_id) DO NOTHING`,
		eventID,
		userID,
		candidate.GrantID,
		calculation.ExpiredRemaining,
		calculation.RefundablePrincipal,
		calculation.FeeAmount,
		calculation.NetRefund,
		candidate.PolicyVersion,
		now,
	)
	if err != nil {
		return fmt.Errorf("record bank exchange expiry settlement: %w", err)
	}
	affected, err = result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("bank exchange grant %d was settled concurrently", candidate.GrantID)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO bank_ledger
    (user_id, operation, grant_id, permanent_delta, temporary_delta, debt_delta, debt_before, debt_after, metadata,permanent_balance_before,permanent_balance_after)
VALUES ($1, 'exchange_expiry_refund', $2, $3, $4, 0, $6, $6, $5,$7,$8)`,
		userID,
		candidate.GrantID,
		calculation.NetRefund,
		formatBankFixed8(new(big.Int).Neg(mustParseBankFixed8(calculation.ExpiredRemaining))),
		marshalBankMetadata(map[string]any{
			"event_id":             eventID,
			"principal_permanent":  candidate.PrincipalPermanent,
			"generated_temporary":  candidate.GeneratedTemporary,
			"expired_remaining":    calculation.ExpiredRemaining,
			"refundable_principal": calculation.RefundablePrincipal,
			"fee_bps":              candidate.FeeBPS,
			"fee_amount":           calculation.FeeAmount,
			"net_refund":           calculation.NetRefund,
			"policy_version":       candidate.PolicyVersion,
			"balance_after":        formatLedgerAmount(balanceAfter),
		}),
		formatLedgerAmount(debt), formatLedgerAmount(balanceBefore), formatLedgerAmount(balanceAfter),
	); err != nil {
		return fmt.Errorf("record bank exchange expiry refund ledger: %w", err)
	}
	return nil
}

func mustParseBankFixed8(raw string) *big.Int {
	value, err := parseBankFixed8(raw)
	if err != nil {
		return new(big.Int)
	}
	return value
}

func bankExchangeExpiryEventID(grantID int64) string {
	return fmt.Sprintf("bank_exchange_expiry_refund:%d", grantID)
}

func loadBankExchangeCommitments(ctx context.Context, q bankQueryer, userID int64, limit int) ([]BankExchangeCommitment, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := q.QueryContext(ctx, `
SELECT snapshot.grant_id,
       snapshot.principal_permanent::text,
       snapshot.generated_temporary::text,
       credit_grant.remaining_amount::text,
       snapshot.fee_bps,
       snapshot.policy_version,
       snapshot.eligibility,
       snapshot.expires_at
FROM bank_exchange_grant_snapshots AS snapshot
JOIN temporary_credit_grants AS credit_grant ON credit_grant.id = snapshot.grant_id
LEFT JOIN bank_exchange_expiry_settlements AS settlement ON settlement.grant_id = snapshot.grant_id
WHERE snapshot.user_id = $1
  AND settlement.id IS NULL
ORDER BY snapshot.expires_at ASC, snapshot.grant_id ASC
LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("load bank exchange commitments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]BankExchangeCommitment, 0)
	for rows.Next() {
		var item BankExchangeCommitment
		if err := rows.Scan(
			&item.GrantID,
			&item.PrincipalPermanent,
			&item.GeneratedTemporary,
			&item.RemainingTemporary,
			&item.FeeBPS,
			&item.PolicyVersion,
			&item.Eligibility,
			&item.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan bank exchange commitment: %w", err)
		}
		if calculation, err := calculateBankExchangeRefundFixed8(
			item.PrincipalPermanent,
			item.RemainingTemporary,
			item.GeneratedTemporary,
			item.FeeBPS,
		); err == nil {
			item.RefundablePrincipalEstimate = calculation.RefundablePrincipal
			item.FeeEstimate = calculation.FeeAmount
			item.NetRefundEstimate = calculation.NetRefund
		} else {
			item.RefundablePrincipalEstimate = "0.00000000"
			item.FeeEstimate = "0.00000000"
			item.NetRefundEstimate = "0.00000000"
			item.Status = BankExchangeExpiryEligibilityManualReview
		}
		if item.Status == "" {
			item.Status = "pending"
		}
		item.ExpiresAt = item.ExpiresAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bank exchange commitments: %w", err)
	}
	return items, nil
}

func loadBankExchangeSettlements(ctx context.Context, q bankQueryer, userID int64, limit int) ([]BankExchangeSettlementItem, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := q.QueryContext(ctx, `
SELECT id, event_id, grant_id, expired_remaining::text, refundable_principal::text,
       fee_amount::text, net_refund::text, status, reason, policy_version, settled_at
FROM bank_exchange_expiry_settlements
WHERE user_id = $1
ORDER BY settled_at DESC, id DESC
LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("load bank exchange settlements: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]BankExchangeSettlementItem, 0)
	for rows.Next() {
		var item BankExchangeSettlementItem
		if err := rows.Scan(
			&item.ID,
			&item.EventID,
			&item.GrantID,
			&item.ExpiredRemaining,
			&item.RefundablePrincipal,
			&item.FeeAmount,
			&item.NetRefund,
			&item.Status,
			&item.Reason,
			&item.PolicyVersion,
			&item.SettledAt,
		); err != nil {
			return nil, fmt.Errorf("scan bank exchange settlement: %w", err)
		}
		item.SettledAt = item.SettledAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bank exchange settlements: %w", err)
	}
	return items, nil
}
