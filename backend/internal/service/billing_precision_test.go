package service

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCostBreakdownUsesLegacyFloat64Amounts(t *testing.T) {
	floatType := reflect.TypeOf(float64(0))
	breakdownType := reflect.TypeOf(CostBreakdown{})

	for _, fieldName := range []string{
		"InputCost",
		"OutputCost",
		"ImageOutputCost",
		"CacheCreationCost",
		"CacheReadCost",
		"TotalCost",
		"ActualCost",
	} {
		field, ok := breakdownType.FieldByName(fieldName)
		require.Truef(t, ok, "CostBreakdown missing %s", fieldName)
		require.Equalf(t, floatType, field.Type, "CostBreakdown.%s must be float64", fieldName)
	}
}

func TestBillingCostInputsUseLegacyFloat64Multipliers(t *testing.T) {
	floatType := reflect.TypeOf(float64(0))

	for _, subject := range []struct {
		typ       reflect.Type
		fieldName string
	}{
		{reflect.TypeOf(CostInput{}), "RateMultiplier"},
		{reflect.TypeOf(ModelPricing{}), "LongContextInputMultiplier"},
		{reflect.TypeOf(ModelPricing{}), "LongContextOutputMultiplier"},
	} {
		field, ok := subject.typ.FieldByName(subject.fieldName)
		require.Truef(t, ok, "%s missing %s", subject.typ.Name(), subject.fieldName)
		require.Equalf(t, floatType, field.Type, "%s.%s must be float64", subject.typ.Name(), subject.fieldName)
	}
}

func TestGroupAndAccountBillingInputsUseFloat64(t *testing.T) {
	floatType := reflect.TypeOf(float64(0))
	floatPointerType := reflect.PointerTo(floatType)

	for _, fieldName := range []string{
		"RateMultiplier",
		"PeakRateMultiplier",
		"ImageRateMultiplier",
		"BatchImageDiscountMultiplier",
		"BatchImageHoldMultiplier",
		"VideoRateMultiplier",
	} {
		field, ok := reflect.TypeOf(Group{}).FieldByName(fieldName)
		require.Truef(t, ok, "Group missing %s", fieldName)
		require.Equalf(t, floatType, field.Type, "Group.%s must be float64", fieldName)
	}

	for _, fieldName := range []string{
		"ImagePrice1K",
		"ImagePrice2K",
		"ImagePrice4K",
		"VideoPrice480P",
		"VideoPrice720P",
		"VideoPrice1080P",
		"WebSearchPricePerCall",
	} {
		field, ok := reflect.TypeOf(Group{}).FieldByName(fieldName)
		require.Truef(t, ok, "Group missing %s", fieldName)
		require.Equalf(t, floatPointerType, field.Type, "Group.%s must be *float64", fieldName)
	}

	accountMultiplier, ok := reflect.TypeOf(Account{}).FieldByName("RateMultiplier")
	require.True(t, ok)
	require.Equal(t, floatPointerType, accountMultiplier.Type)
}

func TestAuthenticationAndBulkBillingInputsUseFloat64(t *testing.T) {
	floatType := reflect.TypeOf(float64(0))
	floatPointerType := reflect.PointerTo(floatType)

	accountBulkMultiplier, ok := reflect.TypeOf(AccountBulkUpdate{}).FieldByName("RateMultiplier")
	require.True(t, ok)
	require.Equal(t, floatPointerType, accountBulkMultiplier.Type)

	for _, fieldName := range []string{
		"Quota",
		"QuotaUsed",
		"RateLimit5h",
		"RateLimit1d",
		"RateLimit7d",
		"Usage5h",
		"Usage1d",
		"Usage7d",
	} {
		field, ok := reflect.TypeOf(APIKey{}).FieldByName(fieldName)
		require.Truef(t, ok, "APIKey missing %s", fieldName)
		require.Equalf(t, floatType, field.Type, "APIKey.%s must be float64", fieldName)
	}

	for _, fieldName := range []string{"Balance", "FrozenBalance", "TotalRecharged"} {
		field, ok := reflect.TypeOf(User{}).FieldByName(fieldName)
		require.Truef(t, ok, "User missing %s", fieldName)
		require.Equalf(t, floatType, field.Type, "User.%s must be float64", fieldName)
	}

	threshold, ok := reflect.TypeOf(User{}).FieldByName("BalanceNotifyThreshold")
	require.True(t, ok)
	require.Equal(t, floatPointerType, threshold.Type)
}

func TestAdminBalanceInputsUseFloat64(t *testing.T) {
	floatPointerType := reflect.PointerTo(reflect.TypeOf(float64(0)))

	for _, subject := range []struct {
		typ       reflect.Type
		fieldName string
	}{
		{reflect.TypeOf(CreateUserInput{}), "Balance"},
		{reflect.TypeOf(UpdateUserInput{}), "Balance"},
	} {
		field, ok := subject.typ.FieldByName(subject.fieldName)
		require.Truef(t, ok, "%s missing %s", subject.typ.Name(), subject.fieldName)
		require.Equalf(t, floatPointerType, field.Type,
			"%s.%s must be *float64", subject.typ.Name(), subject.fieldName)
	}
}

func TestPricingServiceAcceptsScientificNotationPrice(t *testing.T) {
	service := NewPricingService(nil, nil)
	pricingData, err := service.parsePricingData([]byte(`{
		"exact-price-model": {
			"input_cost_per_token": 1e-6,
			"output_cost_per_token": 2e-6
		}
	}`))
	require.NoError(t, err)
	require.Equal(t, 1e-6, pricingData["exact-price-model"].InputCostPerToken)
	require.Equal(t, 2e-6, pricingData["exact-price-model"].OutputCostPerToken)
}

func TestLiteLLMModelPricingUsesLegacyFloat64Amounts(t *testing.T) {
	floatType := reflect.TypeOf(float64(0))
	pricingType := reflect.TypeOf(LiteLLMModelPricing{})

	for _, fieldName := range []string{
		"InputCostPerToken",
		"InputCostPerTokenPriority",
		"OutputCostPerToken",
		"OutputCostPerTokenPriority",
		"CacheCreationInputTokenCost",
		"CacheCreationInputTokenCostPriority",
		"CacheCreationInputTokenCostAbove1hr",
		"CacheReadInputTokenCost",
		"CacheReadInputTokenCostPriority",
		"LongContextInputCostMultiplier",
		"LongContextOutputCostMultiplier",
		"OutputCostPerImage",
		"OutputCostPerImageToken",
	} {
		field, ok := pricingType.FieldByName(fieldName)
		require.Truef(t, ok, "LiteLLMModelPricing missing %s", fieldName)
		require.Equalf(t, floatType, field.Type, "LiteLLMModelPricing.%s must be float64", fieldName)
	}
}

func TestLiteLLMRawEntryUsesLegacyFloat64Pointers(t *testing.T) {
	floatPointerType := reflect.PointerTo(reflect.TypeOf(float64(0)))
	rawEntryType := reflect.TypeOf(LiteLLMRawEntry{})

	for _, fieldName := range []string{
		"InputCostPerToken",
		"InputCostPerTokenPriority",
		"OutputCostPerToken",
		"OutputCostPerTokenPriority",
		"CacheCreationInputTokenCost",
		"CacheCreationInputTokenCostPriority",
		"CacheCreationInputTokenCostAbove1hr",
		"CacheReadInputTokenCost",
		"CacheReadInputTokenCostPriority",
		"LongContextInputCostMultiplier",
		"LongContextOutputCostMultiplier",
		"OutputCostPerImage",
		"OutputCostPerImageToken",
	} {
		field, ok := rawEntryType.FieldByName(fieldName)
		require.Truef(t, ok, "LiteLLMRawEntry missing %s", fieldName)
		require.Equalf(t, floatPointerType, field.Type, "LiteLLMRawEntry.%s must be *float64", fieldName)
	}
}

func TestChannelPricingUsesLegacyFloat64Amounts(t *testing.T) {
	floatType := reflect.TypeOf(float64(0))
	floatPointerType := reflect.PointerTo(floatType)

	for _, pricingType := range []reflect.Type{
		reflect.TypeOf(ChannelModelPricing{}),
		reflect.TypeOf(PricingInterval{}),
	} {
		for _, fieldName := range []string{
			"InputPrice",
			"OutputPrice",
			"CacheWritePrice",
			"CacheReadPrice",
			"PerRequestPrice",
		} {
			field, ok := pricingType.FieldByName(fieldName)
			require.Truef(t, ok, "%s missing %s", pricingType.Name(), fieldName)
			require.Equalf(t, floatPointerType, field.Type,
				"%s.%s must be *float64", pricingType.Name(), fieldName)
		}
	}

	channelPricingType := reflect.TypeOf(ChannelModelPricing{})
	imageOutputPrice, ok := channelPricingType.FieldByName("ImageOutputPrice")
	require.True(t, ok)
	require.Equal(t, floatPointerType, imageOutputPrice.Type)

	resolvedType := reflect.TypeOf(ResolvedPricing{})
	defaultPerRequestPrice, ok := resolvedType.FieldByName("DefaultPerRequestPrice")
	require.True(t, ok)
	require.Equal(t, floatType, defaultPerRequestPrice.Type)
}

func TestBatchImageBillingAmountsUseLegacyFloat64(t *testing.T) {
	floatType := reflect.TypeOf(float64(0))
	floatPointerType := reflect.PointerTo(floatType)
	jobType := reflect.TypeOf(BatchImageJob{})

	for _, fieldName := range []string{
		"EstimatedCost",
		"BillableUnitPrice",
	} {
		field, ok := jobType.FieldByName(fieldName)
		require.Truef(t, ok, "BatchImageJob missing %s", fieldName)
		require.Equalf(t, floatType, field.Type, "BatchImageJob.%s must be float64", fieldName)
	}

	for _, fieldName := range []string{
		"HoldAmount",
		"ActualCost",
	} {
		field, ok := jobType.FieldByName(fieldName)
		require.Truef(t, ok, "BatchImageJob missing %s", fieldName)
		require.Equalf(t, floatPointerType, field.Type,
			"BatchImageJob.%s must be *float64", fieldName)
	}

	resultType := reflect.TypeOf(BatchImageSettlementResult{})
	actualCost, ok := resultType.FieldByName("ActualCost")
	require.True(t, ok)
	require.Equal(t, floatType, actualCost.Type)

	holdCommandType := reflect.TypeOf(BatchImageBalanceHoldCommand{})
	for _, fieldName := range []string{"HoldAmount", "ActualAmount"} {
		field, ok := holdCommandType.FieldByName(fieldName)
		require.Truef(t, ok, "BatchImageBalanceHoldCommand missing %s", fieldName)
		require.Equalf(t, floatType, field.Type,
			"BatchImageBalanceHoldCommand.%s must be float64", fieldName)
	}
}

func TestUsageLogCreationKeepsLegacyFloat64Amounts(t *testing.T) {
	floatType := reflect.TypeOf(float64(0))
	floatPointerType := reflect.PointerTo(floatType)

	for _, subject := range []struct {
		typ        reflect.Type
		fieldNames []string
	}{
		{
			typ: reflect.TypeOf(CreateUsageLogRequest{}),
			fieldNames: []string{
				"InputCost", "OutputCost", "ImageOutputCost", "CacheCreationCost",
				"CacheReadCost", "TotalCost", "ActualCost", "RateMultiplier",
			},
		},
		{
			typ: reflect.TypeOf(UsageLog{}),
			fieldNames: []string{
				"InputCost", "OutputCost", "ImageOutputCost", "CacheCreationCost",
				"CacheReadCost", "TotalCost", "ActualCost", "RateMultiplier",
			},
		},
	} {
		for _, fieldName := range subject.fieldNames {
			field, ok := subject.typ.FieldByName(fieldName)
			require.Truef(t, ok, "%s missing %s", subject.typ.Name(), fieldName)
			require.Equalf(t, floatType, field.Type, "%s.%s must be float64", subject.typ.Name(), fieldName)
		}
	}

	for _, fieldName := range []string{"AccountRateMultiplier", "AccountStatsCost"} {
		field, ok := reflect.TypeOf(UsageLog{}).FieldByName(fieldName)
		require.Truef(t, ok, "UsageLog missing %s", fieldName)
		require.Equalf(t, floatPointerType, field.Type, "UsageLog.%s must be *float64", fieldName)
	}
}
