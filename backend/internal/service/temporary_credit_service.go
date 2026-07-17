package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrTemporaryCreditAmountMustBePositive = errors.New("temporary credit amount must be positive")
var ErrTemporaryCreditAmountInvalid = errors.New("temporary credit amount is invalid")
var ErrTemporaryCreditRequestReferenceRequired = errors.New("temporary credit request reference is required")
var ErrTemporaryCreditTransactionRequired = errors.New("temporary credit transaction is required")

const temporaryCreditRequestIDMaxLen = 255

type TemporaryCreditSource string

const (
	TemporaryCreditSourceCheckin    TemporaryCreditSource = "checkin"
	TemporaryCreditSourceAdminGrant TemporaryCreditSource = "admin_grant"
)

type TemporaryCreditGrant struct {
	ID              int64
	UserID          int64
	Source          TemporaryCreditSource
	CheckinID       *int64
	Amount          float64
	RemainingAmount float64
	expiresAt       time.Time
	Notes           string
	GrantedBy       *int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ExpiresAt returns the service-computed expiration without allowing callers
// to inject an arbitrary value into repository grant creation.
func (g TemporaryCreditGrant) ExpiresAt() time.Time {
	return g.expiresAt
}

type TemporaryCreditConsumptionReference struct {
	UsageLogID *int64
	RequestID  string
}

type TemporaryCreditRepository interface {
	CreateGrant(ctx context.Context, grant TemporaryCreditGrant) (*TemporaryCreditGrant, error)
	CreateGrantTx(ctx context.Context, tx *sql.Tx, grant TemporaryCreditGrant) (*TemporaryCreditGrant, error)
	AvailableSummary(ctx context.Context, userID int64) (float64, *time.Time, error)
	ConsumeFEFO(ctx context.Context, tx *sql.Tx, userID int64, amount float64, reference TemporaryCreditConsumptionReference) (float64, error)
}

type CreateTemporaryCreditGrantInput struct {
	UserID    int64
	Source    TemporaryCreditSource
	CheckinID *int64
	Amount    float64
	Notes     string
	GrantedBy *int64

	// businessNow is set only by same-package workflows that have already
	// captured an authoritative business instant. Public callers cannot choose
	// an arbitrary expiry and continue to use the service clock.
	businessNow *time.Time
}

type TemporaryCreditService struct {
	repo                       TemporaryCreditRepository
	now                        func() time.Time
	availableCreditInvalidator AvailableCreditInvalidator
}

func NewTemporaryCreditService(repo TemporaryCreditRepository) *TemporaryCreditService {
	return NewTemporaryCreditServiceWithClock(repo, time.Now)
}

// NewTemporaryCreditServiceWithAvailableCreditInvalidator wires the
// post-commit available-credit cache invalidator for standalone grants.
func NewTemporaryCreditServiceWithAvailableCreditInvalidator(repo TemporaryCreditRepository, invalidator AvailableCreditInvalidator) *TemporaryCreditService {
	return newTemporaryCreditService(repo, time.Now, invalidator)
}

// NewTemporaryCreditServiceWithClock provides deterministic business-time tests
// without allowing callers to choose an arbitrary expiration instant.
func NewTemporaryCreditServiceWithClock(repo TemporaryCreditRepository, now func() time.Time) *TemporaryCreditService {
	return newTemporaryCreditService(repo, now, nil)
}

func newTemporaryCreditService(repo TemporaryCreditRepository, now func() time.Time, invalidator AvailableCreditInvalidator) *TemporaryCreditService {
	if now == nil {
		now = time.Now
	}
	return &TemporaryCreditService{repo: repo, now: now, availableCreditInvalidator: invalidator}
}

// ValidateTemporaryCreditAmount enforces the storage contract shared by
// check-in rewards and temporary-credit ledger amounts.
func ValidateTemporaryCreditAmount(amount float64) error {
	normalized, err := normalizeLedgerAmount(amount)
	if err != nil {
		return ErrTemporaryCreditAmountInvalid
	}
	if normalized <= 0 {
		return ErrTemporaryCreditAmountMustBePositive
	}
	return nil
}

func (s *TemporaryCreditService) CreateGrant(ctx context.Context, input CreateTemporaryCreditGrantInput) (*TemporaryCreditGrant, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("temporary credit repository is nil")
	}
	grant, err := s.newGrant(input)
	if err != nil {
		return nil, err
	}
	created, err := s.repo.CreateGrant(ctx, grant)
	if err != nil {
		return nil, err
	}
	created.expiresAt = grant.expiresAt
	s.invalidateAvailableCredit(ctx, created.UserID)
	return created, nil
}

func (s *TemporaryCreditService) invalidateAvailableCredit(ctx context.Context, userID int64) {
	if s == nil || s.availableCreditInvalidator == nil {
		return
	}
	_ = s.availableCreditInvalidator.InvalidateAvailableCredit(ctx, userID)
}

// CreateAdminGrant records a manually issued, expiring credit batch. It never
// mutates a user's permanent balance or recharge total.
func (s *TemporaryCreditService) CreateAdminGrant(ctx context.Context, userID, grantedBy int64, amount float64, notes string) (*TemporaryCreditGrant, error) {
	if grantedBy <= 0 {
		return nil, infraerrors.BadRequest("INVALID_ADMIN_ID", "administrator id must be positive")
	}
	return s.CreateGrant(ctx, CreateTemporaryCreditGrantInput{
		UserID:    userID,
		Source:    TemporaryCreditSourceAdminGrant,
		Amount:    amount,
		Notes:     notes,
		GrantedBy: &grantedBy,
	})
}

// CreateGrantTx joins the caller's transaction so a check-in row and its
// temporary-credit batch can be committed or rolled back atomically.
func (s *TemporaryCreditService) CreateGrantTx(ctx context.Context, tx *sql.Tx, input CreateTemporaryCreditGrantInput) (*TemporaryCreditGrant, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("temporary credit repository is nil")
	}
	if tx == nil {
		return nil, errors.New("temporary credit grant transaction is nil")
	}
	grant, err := s.newGrant(input)
	if err != nil {
		return nil, err
	}
	created, err := s.repo.CreateGrantTx(ctx, tx, grant)
	if err != nil {
		return nil, err
	}
	created.expiresAt = grant.expiresAt
	return created, nil
}

func (s *TemporaryCreditService) newGrant(input CreateTemporaryCreditGrantInput) (TemporaryCreditGrant, error) {
	amount := input.Amount
	if err := ValidateTemporaryCreditAmount(amount); err != nil {
		return TemporaryCreditGrant{}, err
	}
	amount, _ = normalizeLedgerAmount(amount)
	businessNow := s.now()
	if input.businessNow != nil {
		businessNow = *input.businessNow
	}
	expiresAt, err := nextTemporaryCreditExpiry(businessNow)
	if err != nil {
		return TemporaryCreditGrant{}, err
	}
	grant := TemporaryCreditGrant{
		UserID:          input.UserID,
		Source:          input.Source,
		CheckinID:       input.CheckinID,
		Amount:          amount,
		RemainingAmount: amount,
		expiresAt:       expiresAt,
		Notes:           input.Notes,
		GrantedBy:       input.GrantedBy,
	}
	if err := validateTemporaryCreditGrant(grant); err != nil {
		return TemporaryCreditGrant{}, err
	}
	return grant, nil
}

func (s *TemporaryCreditService) ConsumeFEFO(ctx context.Context, tx *sql.Tx, userID int64, amount float64, reference TemporaryCreditConsumptionReference) (float64, error) {
	if s == nil || s.repo == nil {
		return 0, errors.New("temporary credit repository is nil")
	}
	if tx == nil {
		return 0, ErrTemporaryCreditTransactionRequired
	}
	if err := ValidateTemporaryCreditAmount(amount); err != nil {
		return 0, err
	}
	amount, _ = normalizeLedgerAmount(amount)
	var err error
	reference, err = normalizeTemporaryCreditReference(reference)
	if err != nil {
		return 0, err
	}
	return s.repo.ConsumeFEFO(ctx, tx, userID, amount, reference)
}

// CanonicalTemporaryCreditRequestID validates the immutable upstream billing
// identifier without changing its value. The database stores this raw value.
func CanonicalTemporaryCreditRequestID(requestID string) (string, error) {
	if requestID == "" {
		return "", ErrTemporaryCreditRequestReferenceRequired
	}
	if requestID != strings.TrimSpace(requestID) {
		return "", fmt.Errorf("%w: request_id must not contain leading or trailing whitespace", ErrTemporaryCreditRequestReferenceRequired)
	}
	if utf8.RuneCountInString(requestID) > temporaryCreditRequestIDMaxLen {
		return "", fmt.Errorf("temporary credit request reference exceeds %d characters", temporaryCreditRequestIDMaxLen)
	}
	return requestID, nil
}

func normalizeTemporaryCreditReference(reference TemporaryCreditConsumptionReference) (TemporaryCreditConsumptionReference, error) {
	if (reference.UsageLogID == nil) == (reference.RequestID == "") {
		return TemporaryCreditConsumptionReference{}, ErrTemporaryCreditRequestReferenceRequired
	}
	if reference.UsageLogID != nil && *reference.UsageLogID <= 0 {
		return TemporaryCreditConsumptionReference{}, ErrTemporaryCreditRequestReferenceRequired
	}
	if reference.RequestID != "" {
		canonical, err := CanonicalTemporaryCreditRequestID(reference.RequestID)
		if err != nil {
			return TemporaryCreditConsumptionReference{}, err
		}
		reference.RequestID = canonical
	}
	return reference, nil
}

func validateTemporaryCreditGrant(grant TemporaryCreditGrant) error {
	if grant.UserID <= 0 {
		return fmt.Errorf("temporary credit user id must be positive")
	}
	if err := ValidateTemporaryCreditAmount(grant.Amount); err != nil {
		return err
	}
	remaining, err := normalizeLedgerAmount(grant.RemainingAmount)
	if err != nil || remaining < 0 || remaining-grant.Amount > ledgerAmountEpsilon {
		return fmt.Errorf("temporary credit remaining amount must be between zero and amount")
	}
	if grant.expiresAt.IsZero() {
		return fmt.Errorf("temporary credit expiration is required")
	}
	switch grant.Source {
	case TemporaryCreditSourceCheckin:
		if grant.CheckinID == nil || grant.GrantedBy != nil || grant.Notes != "" {
			return fmt.Errorf("checkin grant must have checkin id and no administrator metadata")
		}
	case TemporaryCreditSourceAdminGrant:
		if grant.CheckinID != nil || grant.GrantedBy == nil || *grant.GrantedBy <= 0 {
			return fmt.Errorf("admin grant must have administrator metadata and no checkin id")
		}
	default:
		return fmt.Errorf("unknown temporary credit source %q", grant.Source)
	}
	return nil
}

func nextTemporaryCreditExpiry(now time.Time) (time.Time, error) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Time{}, fmt.Errorf("load Asia/Shanghai location: %w", err)
	}
	businessNow := now.In(location)
	return time.Date(businessNow.Year(), businessNow.Month(), businessNow.Day()+1, 0, 0, 0, 0, location), nil
}
