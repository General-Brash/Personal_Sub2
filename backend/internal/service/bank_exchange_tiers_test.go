package service

import "testing"

func TestCalculateBankTieredExchangeSplitsContinuousBands(t *testing.T) {
	first := 50.0
	second := 150.0
	tiers := []BankExchangeTier{{UpTo: &first, Rate: 2}, {UpTo: &second, Rate: 1.9}, {Rate: 1.8}}
	granted, allocations, err := calculateBankTieredExchange(tiers, 0, 200)
	if err != nil {
		t.Fatalf("calculate tiers: %v", err)
	}
	if got, want := formatLedgerAmount(granted), "380.00000000"; got != want {
		t.Fatalf("granted = %s, want %s", got, want)
	}
	if len(allocations) != 3 || allocations[0].PermanentAmount != "50.00000000" || allocations[1].PermanentAmount != "100.00000000" || allocations[2].PermanentAmount != "50.00000000" {
		t.Fatalf("unexpected allocations: %#v", allocations)
	}
}

func TestCalculateBankTieredExchangeStartsAtExistingDailyUsage(t *testing.T) {
	first := 50.0
	second := 150.0
	tiers := []BankExchangeTier{{UpTo: &first, Rate: 2}, {UpTo: &second, Rate: 1.9}, {Rate: 1.8}}
	granted, allocations, err := calculateBankTieredExchange(tiers, 40, 30)
	if err != nil {
		t.Fatalf("calculate tiers: %v", err)
	}
	if got, want := formatLedgerAmount(granted), "58.00000000"; got != want {
		t.Fatalf("granted = %s, want %s", got, want)
	}
	if len(allocations) != 2 || allocations[0].PermanentAmount != "10.00000000" || allocations[1].PermanentAmount != "20.00000000" {
		t.Fatalf("unexpected allocations: %#v", allocations)
	}
}

func TestNormalizeBankExchangeTiersRequiresFinalTail(t *testing.T) {
	first := 50.0
	if _, err := normalizeBankExchangeTiers([]BankExchangeTier{{UpTo: &first, Rate: 2}}, 1); err == nil {
		t.Fatal("expected a finite final tier to be rejected")
	}
	if _, err := normalizeBankExchangeTiers([]BankExchangeTier{{Rate: 2}, {Rate: 1}}, 1); err == nil {
		t.Fatal("expected an unbounded non-final tier to be rejected")
	}
}

func TestBankExchangeProgressReportsNextBand(t *testing.T) {
	first := 50.0
	second := 150.0
	tiers := []BankExchangeTier{{UpTo: &first, Rate: 2}, {UpTo: &second, Rate: 1.9}, {Rate: 1.8}}
	progress := bankExchangeProgress(tiers, "2026-07-23", 40)
	if progress.CurrentTierIndex != 0 || progress.CurrentTierRate != "2.00000000" {
		t.Fatalf("unexpected current tier: %#v", progress)
	}
	if progress.AmountUntilNextTier == nil || *progress.AmountUntilNextTier != "10.00000000" {
		t.Fatalf("unexpected next-band distance: %#v", progress.AmountUntilNextTier)
	}
}
