package service

import (
	"context"
	"testing"
)

func TestLegacyPermanentAmountServiceContractsRemainFloat64(t *testing.T) {
	threshold := 3.75
	user := User{
		Balance:                1.25,
		FrozenBalance:          2.5,
		BalanceNotifyThreshold: &threshold,
		TotalRecharged:         4.5,
	}
	code := RedeemCode{Value: 5.5}
	request := GenerateCodesRequest{Value: 6.5}
	response := RedeemCodeResponse{Value: 7.5}
	batchUpdate := RedeemCodeBatchUpdateFields{Value: &threshold}
	account := Account{RateMultiplier: &threshold}
	apiKey := APIKey{
		Quota:       8.5,
		QuotaUsed:   9.5,
		RateLimit5h: 10.5,
		RateLimit1d: 11.5,
		RateLimit7d: 12.5,
		Usage5h:     13.5,
		Usage1d:     14.5,
		Usage7d:     15.5,
	}
	profile := UpdateProfileRequest{BalanceNotifyThreshold: &threshold}
	createAPIKey := CreateAPIKeyRequest{
		Quota:       16.5,
		RateLimit5h: 17.5,
		RateLimit1d: 18.5,
		RateLimit7d: 19.5,
	}
	updateAPIKey := UpdateAPIKeyRequest{
		Quota:       &threshold,
		RateLimit5h: &threshold,
		RateLimit1d: &threshold,
		RateLimit7d: &threshold,
	}
	rateLimitData := APIKeyRateLimitData{Usage5h: 20.5, Usage1d: 21.5, Usage7d: 22.5}
	quotaState := APIKeyQuotaUsageState{QuotaUsed: 23.5, Quota: 24.5}

	assertLegacyServiceFloat64(t, user.Balance)
	assertLegacyServiceFloat64(t, user.FrozenBalance)
	assertLegacyServiceFloat64(t, *user.BalanceNotifyThreshold)
	assertLegacyServiceFloat64(t, user.TotalRecharged)
	assertLegacyServiceFloat64(t, code.Value)
	assertLegacyServiceFloat64(t, request.Value)
	assertLegacyServiceFloat64(t, response.Value)
	assertLegacyServiceFloat64(t, *batchUpdate.Value)
	assertLegacyServiceFloat64(t, account.BillingRateMultiplier())
	assertLegacyServiceFloat64(t, apiKey.GetQuotaRemaining())
	assertLegacyServiceFloat64(t, apiKey.EffectiveUsage5h())
	assertLegacyServiceFloat64(t, *profile.BalanceNotifyThreshold)
	assertLegacyServiceFloat64(t, createAPIKey.Quota)
	assertLegacyServiceFloat64(t, createAPIKey.RateLimit5h)
	assertLegacyServiceFloat64(t, *updateAPIKey.Quota)
	assertLegacyServiceFloat64(t, *updateAPIKey.RateLimit5h)
	assertLegacyServiceFloat64(t, rateLimitData.EffectiveUsage5h())
	assertLegacyServiceFloat64(t, quotaState.QuotaUsed)

	var _ func(*APIKeyService, context.Context, int64, float64) error = (*APIKeyService).UpdateQuotaUsed
	var _ func(*APIKeyService, context.Context, int64, float64) error = (*APIKeyService).UpdateRateLimitUsage
	var _ APIKeyQuotaUpdater = (*legacyAPIKeyQuotaUpdaterStub)(nil)

	postBilling := postUsageBillingParams{AccountRateMultiplier: 1.25}
	longContext := RecordUsageLongContextInput{LongContextMultiplier: 2.0}
	assertLegacyServiceFloat64(t, postBilling.AccountRateMultiplier)
	assertLegacyServiceFloat64(t, longContext.LongContextMultiplier)
}

type legacyAPIKeyQuotaUpdaterStub struct{}

func (*legacyAPIKeyQuotaUpdaterStub) UpdateQuotaUsed(context.Context, int64, float64) error {
	return nil
}

func (*legacyAPIKeyQuotaUpdaterStub) UpdateRateLimitUsage(context.Context, int64, float64) error {
	return nil
}

func assertLegacyServiceFloat64(t *testing.T, value float64) {
	t.Helper()
	if value < 0 {
		t.Fatal("unexpected negative compatibility fixture")
	}
}
