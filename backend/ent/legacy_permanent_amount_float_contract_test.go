package ent

import (
	"encoding/json"
	"testing"
)

func TestLegacyPermanentAmountFieldsRemainFloat64(t *testing.T) {
	threshold := 3.75
	user := User{
		Balance:                1.25,
		FrozenBalance:          2.5,
		BalanceNotifyThreshold: &threshold,
		TotalRecharged:         4.5,
	}
	redeemCode := RedeemCode{Value: 5.5}
	account := Account{RateMultiplier: 1.5}
	apiKey := APIKey{
		Quota:       6.5,
		QuotaUsed:   7.5,
		RateLimit5h: 8.5,
		RateLimit1d: 9.5,
		RateLimit7d: 10.5,
		Usage5h:     11.5,
		Usage1d:     12.5,
		Usage7d:     13.5,
	}

	assertLegacyFloat64(t, user.Balance)
	assertLegacyFloat64(t, user.FrozenBalance)
	assertLegacyFloat64(t, *user.BalanceNotifyThreshold)
	assertLegacyFloat64(t, user.TotalRecharged)
	assertLegacyFloat64(t, redeemCode.Value)
	assertLegacyFloat64(t, account.RateMultiplier)
	assertLegacyFloat64(t, apiKey.Quota)
	assertLegacyFloat64(t, apiKey.QuotaUsed)
	assertLegacyFloat64(t, apiKey.RateLimit5h)
	assertLegacyFloat64(t, apiKey.RateLimit1d)
	assertLegacyFloat64(t, apiKey.RateLimit7d)
	assertLegacyFloat64(t, apiKey.Usage5h)
	assertLegacyFloat64(t, apiKey.Usage1d)
	assertLegacyFloat64(t, apiKey.Usage7d)

	assertJSONNumber(t, user, "balance")
	assertJSONNumber(t, redeemCode, "value")
	assertJSONNumber(t, account, "rate_multiplier")
	assertJSONNumber(t, apiKey, "quota")
}

func assertLegacyFloat64(t *testing.T, value float64) {
	t.Helper()
	if value < 0 {
		t.Fatal("unexpected negative compatibility fixture")
	}
}

func assertJSONNumber(t *testing.T, value any, key string) {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", key, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal %s: %v", key, err)
	}
	if _, ok := decoded[key].(float64); !ok {
		t.Fatalf("%s must remain a JSON number, got %T", key, decoded[key])
	}
}
