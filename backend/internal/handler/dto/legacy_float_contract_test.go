package dto

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestLegacyDTOAmountFieldsUseFloat64(t *testing.T) {
	float64Type := reflect.TypeOf(float64(0))
	float64PtrType := reflect.TypeOf((*float64)(nil))
	groupRatesType := reflect.TypeOf(map[int64]float64{})

	for _, test := range []struct {
		name      string
		typ       reflect.Type
		fieldName string
		want      reflect.Type
	}{
		{"user balance", reflect.TypeOf(User{}), "Balance", float64Type},
		{"user frozen balance", reflect.TypeOf(User{}), "FrozenBalance", float64Type},
		{"user balance threshold", reflect.TypeOf(User{}), "BalanceNotifyThreshold", float64PtrType},
		{"user total recharged", reflect.TypeOf(User{}), "TotalRecharged", float64Type},
		{"admin user group rates", reflect.TypeOf(AdminUser{}), "GroupRates", groupRatesType},
		{"api key quota", reflect.TypeOf(APIKey{}), "Quota", float64Type},
		{"api key quota used", reflect.TypeOf(APIKey{}), "QuotaUsed", float64Type},
		{"api key rate limit 5h", reflect.TypeOf(APIKey{}), "RateLimit5h", float64Type},
		{"api key rate limit 1d", reflect.TypeOf(APIKey{}), "RateLimit1d", float64Type},
		{"api key rate limit 7d", reflect.TypeOf(APIKey{}), "RateLimit7d", float64Type},
		{"api key usage 5h", reflect.TypeOf(APIKey{}), "Usage5h", float64Type},
		{"api key usage 1d", reflect.TypeOf(APIKey{}), "Usage1d", float64Type},
		{"api key usage 7d", reflect.TypeOf(APIKey{}), "Usage7d", float64Type},
		{"group rate multiplier", reflect.TypeOf(Group{}), "RateMultiplier", float64Type},
		{"group daily limit", reflect.TypeOf(Group{}), "DailyLimitUSD", float64PtrType},
		{"group image rate multiplier", reflect.TypeOf(Group{}), "ImageRateMultiplier", float64Type},
		{"group image price", reflect.TypeOf(Group{}), "ImagePrice1K", float64PtrType},
		{"group video rate multiplier", reflect.TypeOf(Group{}), "VideoRateMultiplier", float64Type},
		{"group web search price", reflect.TypeOf(Group{}), "WebSearchPricePerCall", float64PtrType},
		{"account rate multiplier", reflect.TypeOf(Account{}), "RateMultiplier", float64Type},
		{"redeem value", reflect.TypeOf(RedeemCode{}), "Value", float64Type},
		{"redeem update value", reflect.TypeOf(BatchUpdateRedeemCodeFields{}), "Value", float64PtrType},
		{"usage input cost", reflect.TypeOf(UsageLog{}), "InputCost", float64Type},
		{"usage total cost", reflect.TypeOf(UsageLog{}), "TotalCost", float64Type},
		{"usage image output cost", reflect.TypeOf(UsageLog{}), "ImageOutputCost", float64Type},
		{"admin usage account rate", reflect.TypeOf(AdminUsageLog{}), "AccountRateMultiplier", float64PtrType},
		{"admin usage account stats cost", reflect.TypeOf(AdminUsageLog{}), "AccountStatsCost", float64PtrType},
	} {
		t.Run(test.name, func(t *testing.T) {
			field, ok := test.typ.FieldByName(test.fieldName)
			require.Truef(t, ok, "%s.%s is missing", test.typ.Name(), test.fieldName)
			require.Equalf(t, test.want, field.Type, "%s.%s must keep the legacy float contract", test.typ.Name(), test.fieldName)
		})
	}
}

func TestLegacyDTOAmountMappersReturnNumericJSON(t *testing.T) {
	threshold := 0.25
	user := UserFromServiceShallow(&service.User{
		Balance:                1.25,
		FrozenBalance:          0.5,
		BalanceNotifyThreshold: &threshold,
		TotalRecharged:         3.75,
	})
	require.Equal(t, 1.25, user.Balance)
	require.Equal(t, 0.5, user.FrozenBalance)
	require.Equal(t, &threshold, user.BalanceNotifyThreshold)
	require.Equal(t, 3.75, user.TotalRecharged)

	body, err := json.Marshal(user)
	require.NoError(t, err)
	require.Contains(t, string(body), `"balance":1.25`)
	require.NotContains(t, string(body), `"balance":"1.25000000"`)

	quota := APIKeyFromService(&service.APIKey{
		Quota:       2.5,
		QuotaUsed:   0.25,
		RateLimit5h: 5,
		RateLimit1d: 10,
		RateLimit7d: 20,
	})
	require.Equal(t, 2.5, quota.Quota)
	require.Equal(t, 0.25, quota.QuotaUsed)
	require.Equal(t, 5.0, quota.RateLimit5h)

	groupRate := float64(1.75)
	group := GroupFromService(&service.Group{
		RateMultiplier:        groupRate,
		DailyLimitUSD:         &groupRate,
		ImageRateMultiplier:   groupRate,
		VideoRateMultiplier:   groupRate,
		WebSearchPricePerCall: &groupRate,
	})
	require.Equal(t, 1.75, group.RateMultiplier)
	require.Equal(t, 1.75, *group.DailyLimitUSD)
	require.Equal(t, 1.75, group.ImageRateMultiplier)
	require.Equal(t, 1.75, group.VideoRateMultiplier)
	require.Equal(t, 1.75, *group.WebSearchPricePerCall)

	groupRates := map[int64]float64{42: 1.75}
	adminUser := UserFromServiceAdmin(&service.User{GroupRates: groupRates})
	require.Equal(t, groupRates, adminUser.GroupRates)

	body, err = json.Marshal(adminUser)
	require.NoError(t, err)
	require.Contains(t, string(body), `"group_rates":{"42":1.75}`)
	require.NotContains(t, string(body), `"group_rates":{"42":"1.75000000"}`)

	nilGroupRates := UserFromServiceAdmin(&service.User{})
	require.Nil(t, nilGroupRates.GroupRates)
	body, err = json.Marshal(nilGroupRates)
	require.NoError(t, err)
	require.NotContains(t, string(body), `"group_rates"`)

	usageCost := 0.125
	usage := UsageLogFromServiceAdmin(&service.UsageLog{
		InputCost:             usageCost,
		TotalCost:             usageCost,
		ImageOutputCost:       usageCost,
		AccountRateMultiplier: &usageCost,
		AccountStatsCost:      &usageCost,
	})
	require.Equal(t, 0.125, usage.InputCost)
	require.Equal(t, 0.125, usage.TotalCost)
	require.Equal(t, 0.125, usage.ImageOutputCost)
	require.Equal(t, 0.125, *usage.AccountRateMultiplier)
	require.Equal(t, 0.125, *usage.AccountStatsCost)
}
