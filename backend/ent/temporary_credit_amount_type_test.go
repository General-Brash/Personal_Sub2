package ent

import (
	"reflect"
	"testing"
)

func TestTemporaryCreditLedgerAmountsUseFloat64(t *testing.T) {
	want := reflect.TypeOf(float64(0))
	for name, got := range map[string]any{
		"daily checkin reward":             DailyCheckin{}.RewardAmount,
		"temporary credit grant amount":    TemporaryCreditGrant{}.Amount,
		"temporary credit grant remainder": TemporaryCreditGrant{}.RemainingAmount,
		"temporary credit consumption":     TemporaryCreditConsumption{}.Amount,
	} {
		if reflect.TypeOf(got) != want {
			t.Errorf("%s type = %v, want %v", name, reflect.TypeOf(got), want)
		}
	}
}
