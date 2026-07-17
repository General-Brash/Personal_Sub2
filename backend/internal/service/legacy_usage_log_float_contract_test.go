package service

import (
	"reflect"
	"testing"
)

func TestLegacyUsageLogAmountsRemainFloat64(t *testing.T) {
	typ := reflect.TypeOf(UsageLog{})
	wantFloat := reflect.TypeOf(float64(0))
	wantFloatPtr := reflect.TypeOf((*float64)(nil))

	for _, name := range []string{
		"ImageOutputCost",
		"InputCost",
		"OutputCost",
		"CacheCreationCost",
		"CacheReadCost",
		"TotalCost",
		"ActualCost",
		"RateMultiplier",
	} {
		field, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("UsageLog.%s is missing", name)
		}
		if field.Type != wantFloat {
			t.Fatalf("UsageLog.%s type = %s, want float64", name, field.Type)
		}
	}

	for _, name := range []string{"AccountRateMultiplier", "AccountStatsCost"} {
		field, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("UsageLog.%s is missing", name)
		}
		if field.Type != wantFloatPtr {
			t.Fatalf("UsageLog.%s type = %s, want *float64", name, field.Type)
		}
	}
}
