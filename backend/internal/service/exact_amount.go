package service

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const (
	ledgerAmountScale   = 8
	ledgerAmountFactor  = 100000000.0
	ledgerAmountEpsilon = 0.5 / ledgerAmountFactor
	maxLedgerAmount     = 1000000000000.0
)

var (
	strictLedgerAmountPattern       = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,11})(?:\.[0-9]{1,8})?$`)
	strictSignedLedgerAmountPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]{0,11})(?:\.[0-9]{1,8})?$`)
)

// ParseStrictLedgerAmount accepts the frozen JSON-string amount grammar and
// normalizes the parsed float64 value to the database's eight-decimal scale.
func ParseStrictLedgerAmount(raw string) (float64, error) {
	if raw != strings.TrimSpace(raw) || !strictLedgerAmountPattern.MatchString(raw) {
		return 0, fmt.Errorf("invalid ledger amount %q", raw)
	}
	amount, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse ledger amount %q: %w", raw, err)
	}
	amount, err = normalizeLedgerAmount(amount)
	if err != nil {
		return 0, fmt.Errorf("invalid ledger amount %q: %w", raw, err)
	}
	return amount, nil
}

func ParseStrictPositiveLedgerAmount(raw string) (float64, error) {
	amount, err := ParseStrictLedgerAmount(raw)
	if err != nil {
		return 0, err
	}
	if amount <= 0 {
		return 0, fmt.Errorf("ledger amount must be positive")
	}
	return amount, nil
}

// ParseStrictSignedLedgerAmount accepts the same fixed-scale grammar as a
// ledger amount while allowing an explicit negative sign for adjustments.
func ParseStrictSignedLedgerAmount(raw string) (float64, error) {
	if raw != strings.TrimSpace(raw) || !strictSignedLedgerAmountPattern.MatchString(raw) {
		return 0, fmt.Errorf("invalid signed ledger amount %q", raw)
	}
	amount, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse signed ledger amount %q: %w", raw, err)
	}
	amount, err = normalizeLedgerAmount(amount)
	if err != nil {
		return 0, fmt.Errorf("invalid signed ledger amount %q: %w", raw, err)
	}
	return amount, nil
}

func normalizeDerivedLedgerAmount(amount float64) (float64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, fmt.Errorf("ledger amount must be finite")
	}
	scaled := amount * ledgerAmountFactor
	if math.IsInf(scaled, 0) {
		// At this magnitude float64 has no fractional precision left to round.
		return amount, nil
	}
	normalized := math.Round(scaled) / ledgerAmountFactor
	if math.Abs(normalized) < ledgerAmountEpsilon {
		return 0, nil
	}
	return normalized, nil
}

func normalizeLedgerAmount(amount float64) (float64, error) {
	normalized, err := normalizeDerivedLedgerAmount(amount)
	if err != nil {
		return 0, err
	}
	if math.Abs(amount) >= maxLedgerAmount || math.Abs(normalized) >= maxLedgerAmount {
		return 0, fmt.Errorf("ledger amount must be less than %.0f", maxLedgerAmount)
	}
	return normalized, nil
}

func formatLedgerAmount(amount float64) string {
	normalized, err := normalizeDerivedLedgerAmount(amount)
	if err != nil {
		return strconv.FormatFloat(amount, 'f', ledgerAmountScale, 64)
	}
	return strconv.FormatFloat(normalized, 'f', ledgerAmountScale, 64)
}
