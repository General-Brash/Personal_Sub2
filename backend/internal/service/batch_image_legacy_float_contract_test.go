package service

import (
	"reflect"
	"testing"
)

func TestBatchImageLegacyFloat64Contract(t *testing.T) {
	wantFloat := reflect.TypeOf(float64(0))
	wantFloatPtr := reflect.TypeOf((*float64)(nil))

	assertBatchImageFloatFields(t, reflect.TypeOf(BatchImageJob{}), wantFloat, wantFloatPtr)
	assertBatchImageFloatFields(t, reflect.TypeOf(CreateBatchImageJobParams{}), wantFloat, wantFloatPtr)
	assertBatchImageSnapshotFloatFields(t, reflect.TypeOf(BatchImagePricingSnapshot{}), wantFloat)
	assertBatchImagePublicFloatFields(t, reflect.TypeOf(BatchImagePublicBatch{}), wantFloat, wantFloatPtr)

	settled := reflect.TypeOf(MarkBatchImageJobSettledParams{})
	actualCost, ok := settled.FieldByName("ActualCost")
	if !ok {
		t.Fatal("MarkBatchImageJobSettledParams.ActualCost is missing")
	}
	if actualCost.Type != wantFloat {
		t.Fatalf("MarkBatchImageJobSettledParams.ActualCost type = %s, want float64", actualCost.Type)
	}

	pricing := reflect.TypeOf((*BatchImagePricingResolver)(nil)).Elem()
	method, ok := pricing.MethodByName("BatchImageUnitPrice")
	if !ok {
		t.Fatal("BatchImagePricingResolver.BatchImageUnitPrice is missing")
	}
	if method.Type.NumOut() != 2 || method.Type.Out(0) != wantFloat {
		t.Fatalf("BatchImagePricingResolver.BatchImageUnitPrice first return = %s, want float64", method.Type.Out(0))
	}
}

func assertBatchImageSnapshotFloatFields(t *testing.T, typ, wantFloat reflect.Type) {
	t.Helper()
	for _, name := range []string{
		"BaseUnitPrice",
		"GroupRateMultiplier",
		"AccountRateMultiplier",
		"BatchDiscountMultiplier",
		"HoldMultiplier",
		"BillableUnitPrice",
		"HoldUnitPrice",
		"EstimatedCost",
		"HoldAmount",
	} {
		field, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("%s.%s is missing", typ.Name(), name)
		}
		if field.Type != wantFloat {
			t.Fatalf("%s.%s type = %s, want float64", typ.Name(), name, field.Type)
		}
	}
}

func assertBatchImageFloatFields(t *testing.T, typ, wantFloat, wantFloatPtr reflect.Type) {
	t.Helper()
	for _, name := range []string{
		"BaseUnitPrice",
		"GroupRateMultiplier",
		"AccountRateMultiplier",
		"BatchDiscountMultiplier",
		"HoldMultiplier",
		"BillableUnitPrice",
		"HoldUnitPrice",
		"EstimatedCost",
	} {
		field, ok := typ.FieldByName(name)
		if !ok {
			continue
		}
		if field.Type != wantFloat {
			t.Fatalf("%s.%s type = %s, want float64", typ.Name(), name, field.Type)
		}
	}

	for _, name := range []string{"HoldAmount", "ActualCost"} {
		field, ok := typ.FieldByName(name)
		if !ok {
			continue
		}
		if field.Type != wantFloatPtr {
			t.Fatalf("%s.%s type = %s, want *float64", typ.Name(), name, field.Type)
		}
	}
}

func assertBatchImagePublicFloatFields(t *testing.T, typ, wantFloat, wantFloatPtr reflect.Type) {
	t.Helper()
	for _, name := range []string{"EstimatedCost", "HoldAmount"} {
		field, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("%s.%s is missing", typ.Name(), name)
		}
		if field.Type != wantFloat {
			t.Fatalf("%s.%s type = %s, want float64", typ.Name(), name, field.Type)
		}
	}

	field, ok := typ.FieldByName("ActualCost")
	if !ok {
		t.Fatalf("%s.ActualCost is missing", typ.Name())
	}
	if field.Type != wantFloatPtr {
		t.Fatalf("%s.ActualCost type = %s, want *float64", typ.Name(), field.Type)
	}
}
