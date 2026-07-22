package handler

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCheckoutPurchaseLimitJSONContract(t *testing.T) {
	payload := checkoutInfoResponse{
		Balance: service.MallBalanceSummary{PermanentBalance: "1.00000000", TemporaryCreditAvailable: "2.00000000"},
		Plans: []checkoutPlan{{
			ID: 1, DailyPurchaseLimit: 2, DailyPurchaseRemaining: 1,
			TotalPurchaseLimit: 5, TotalPurchaseRemaining: 4,
			BenefitType: "daily_temporary_credit", PaymentCreditType: "temporary", DailyTemporaryCreditAmount: 10,
		}},
		CurrencyProducts: []checkoutCurrencyProduct{{
			ID: 2, DailyPurchaseLimit: 3, DailyPurchaseRemaining: 2,
			TotalPurchaseLimit: 7, TotalPurchaseRemaining: 6,
			PaymentCreditType: "permanent", CreditedType: "temporary", CreditedAmount: 5,
		}},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	var decoded map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &decoded))
	for _, key := range []string{"plans", "currency_products"} {
		var products []map[string]any
		require.NoError(t, json.Unmarshal(decoded[key], &products))
		require.Contains(t, products[0], "daily_purchase_limit")
		require.Contains(t, products[0], "daily_purchase_remaining")
		require.Contains(t, products[0], "total_purchase_limit")
		require.Contains(t, products[0], "total_purchase_remaining")
	}
	var balance map[string]string
	require.NoError(t, json.Unmarshal(decoded["balance"], &balance))
	require.Equal(t, "1.00000000", balance["permanent_balance"])
	require.Equal(t, "2.00000000", balance["temporary_credit_available"])
}
