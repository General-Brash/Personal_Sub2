package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestCreateDailyTemporaryCreditPlanAndLegacyDefaults(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	config := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)

	daily, err := config.CreatePlan(ctx, CreatePlanRequest{
		GroupID: 0, Name: "Weekly temporary credit", Price: 5.12345678,
		ValidityDays: 7, ValidityUnit: "day", ForSale: true,
		BenefitType: "daily_temporary_credit", PaymentCreditType: "temporary",
		DailyTemporaryCreditAmount: 10.87654321,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), daily.GroupID)
	require.Equal(t, "daily_temporary_credit", daily.BenefitType)
	require.Equal(t, "temporary", daily.PaymentCreditType)
	require.Equal(t, "5.12345678", formatLedgerAmount(daily.Price))
	require.Equal(t, "10.87654321", formatLedgerAmount(daily.DailyTemporaryCreditAmount))

	legacy, err := config.CreatePlan(ctx, CreatePlanRequest{
		GroupID: 9, Name: "Legacy Sub2", Price: 8, ValidityDays: 30, ValidityUnit: "day",
	})
	require.NoError(t, err)
	require.Equal(t, "sub2", legacy.BenefitType)
	require.Equal(t, "permanent", legacy.PaymentCreditType)
	require.Zero(t, legacy.DailyTemporaryCreditAmount)
}

func TestDailyTemporaryCreditPlanRejectsGroupAndMissingDailyAmount(t *testing.T) {
	client := newPaymentConfigServiceTestClient(t)
	config := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)
	_, err := config.CreatePlan(context.Background(), CreatePlanRequest{
		GroupID: 3, Name: "Invalid daily", Price: 5, ValidityDays: 7, ValidityUnit: "day",
		BenefitType: "daily_temporary_credit", PaymentCreditType: "permanent",
	})
	require.Equal(t, "PLAN_GROUP_INVALID", infraerrors.Reason(err))
}

func TestDailyTemporaryCreditPlanRequiresSingularDayValidityUnit(t *testing.T) {
	client := newPaymentConfigServiceTestClient(t)
	config := NewPaymentConfigService(client, &paymentConfigSettingRepoStub{}, nil)

	_, err := config.CreatePlan(context.Background(), CreatePlanRequest{
		GroupID: 0, Name: "Invalid daily unit", Price: 5, ValidityDays: 7, ValidityUnit: "days",
		BenefitType: "daily_temporary_credit", PaymentCreditType: "permanent", DailyTemporaryCreditAmount: 10,
	})
	require.Equal(t, "PLAN_VALIDITY_REQUIRED", infraerrors.Reason(err))
}
