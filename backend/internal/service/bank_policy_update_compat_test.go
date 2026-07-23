package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestBankPolicyDTOExchangeTiersDistinguishesOmittedAndEmpty(t *testing.T) {
	var omitted BankPolicyDTO
	require.NoError(t, json.Unmarshal([]byte(`{"exchange_rate":"2.00000000"}`), &omitted))
	require.Nil(t, omitted.ExchangeTiers)

	var explicitEmpty BankPolicyDTO
	require.NoError(t, json.Unmarshal([]byte(`{"exchange_rate":"2.00000000","exchange_tiers":[]}`), &explicitEmpty))
	require.NotNil(t, explicitEmpty.ExchangeTiers)
	require.Empty(t, explicitEmpty.ExchangeTiers)

	var explicitNull BankPolicyDTO
	require.NoError(t, json.Unmarshal([]byte(`{"exchange_rate":"2.00000000","exchange_tiers":null}`), &explicitNull))
	require.Nil(t, explicitNull.ExchangeTiers)

	omittedJSON, err := json.Marshal(omitted)
	require.NoError(t, err)
	emptyJSON, err := json.Marshal(explicitEmpty)
	require.NoError(t, err)
	require.Contains(t, string(omittedJSON), `"exchange_tiers":null`)
	require.Contains(t, string(emptyJSON), `"exchange_tiers":[]`)
	require.NotEqual(t, string(omittedJSON), string(emptyJSON))
}

func TestUpdatePolicyAtomicPreservesStoredTiersWhenLegacyClientOmitsField(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	stored := multiTierBankPolicyForUpdateTest()
	dto := validBankPolicyUpdateDTO()
	dto.ExchangeRate = "9.00000000"
	require.Nil(t, dto.ExchangeTiers)

	expected, err := bankPolicyFromDTO(dto)
	require.NoError(t, err)
	expected.ExchangeTiers = stored.ExchangeTiers
	expected, err = expected.normalized()
	require.NoError(t, err)

	mock.ExpectBegin()
	expectBankPolicy(mock, stored)
	expectBankPolicyWrites(t, mock, expected)
	mock.ExpectExec("UPDATE idempotency_records").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	claim := newAtomicClaimForTest(time.Now().Add(time.Hour))
	result, err := NewBankService(db, nil, nil).UpdatePolicyAtomic(context.Background(), 7, dto, claim)

	require.NoError(t, err)
	require.Equal(t, "6.00000000", result.AdvanceMinAmount)
	require.Equal(t, "2.00000000", result.ExchangeRate)
	require.Equal(t, bankExchangeTierDTOs(stored.ExchangeTiers), result.ExchangeTiers)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdatePolicyAtomicReplacesTiersOnlyWhenFieldIsExplicit(t *testing.T) {
	boundary := "25.00000000"
	for _, testCase := range []struct {
		name  string
		tiers []BankExchangeTierDTO
		want  []BankExchangeTierDTO
	}{
		{
			name: "explicit tiers replace stored policy",
			tiers: []BankExchangeTierDTO{
				{UpTo: &boundary, Rate: "3.00000000"},
				{Rate: "2.50000000"},
			},
			want: []BankExchangeTierDTO{
				{UpTo: &boundary, Rate: "3.00000000"},
				{Rate: "2.50000000"},
			},
		},
		{
			name:  "explicit empty tiers reset to flat rate",
			tiers: []BankExchangeTierDTO{},
			want:  []BankExchangeTierDTO{{Rate: "1.75000000"}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			dto := validBankPolicyUpdateDTO()
			dto.ExchangeRate = "1.75000000"
			dto.ExchangeTiers = testCase.tiers
			expected, err := bankPolicyFromDTO(dto)
			require.NoError(t, err)

			mock.ExpectBegin()
			expectBankPolicyWrites(t, mock, expected)
			mock.ExpectExec("UPDATE idempotency_records").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			claim := newAtomicClaimForTest(time.Now().Add(time.Hour))
			result, err := NewBankService(db, nil, nil).UpdatePolicyAtomic(context.Background(), 7, dto, claim)

			require.NoError(t, err)
			require.Equal(t, testCase.want, result.ExchangeTiers)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func expectBankPolicyWrites(t *testing.T, mock sqlmock.Sqlmock, policy BankPolicy) {
	t.Helper()
	values, err := bankPolicyValues(policy)
	require.NoError(t, err)
	for _, key := range bankPolicyKeys {
		mock.ExpectExec("INSERT INTO settings").
			WithArgs(key, values[key]).
			WillReturnResult(sqlmock.NewResult(1, 1))
	}
}

func validBankPolicyUpdateDTO() BankPolicyDTO {
	return BankPolicyDTO{
		AdvanceMinAmount:                "6.00000000",
		AdvanceMaxAmount:                "30.00000000",
		DebtGraceDays:                   4,
		DebtConversionRatio:             "1.10000000",
		ExchangeRate:                    "2.00000000",
		UnusedAdvanceDebtReductionRatio: "0.80000000",
		EarlyRepayTemporaryRatio:        "1.10000000",
		EarlyRepayPermanentRatio:        "2.10000000",
	}
}

func multiTierBankPolicyForUpdateTest() BankPolicy {
	first, second := float64(50), float64(150)
	policy := DefaultBankPolicy()
	policy.ExchangeRate = 2
	policy.ExchangeTiers = []BankExchangeTier{
		{UpTo: &first, Rate: 2},
		{UpTo: &second, Rate: 1.9},
		{Rate: 1.8},
	}
	return policy
}
