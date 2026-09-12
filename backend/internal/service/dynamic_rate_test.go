package service

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

type dynamicRateRepoStub struct {
	policy *DynamicRatePolicy
	usage  DynamicRateUsageCounters
}

func (r *dynamicRateRepoStub) GetDynamicRatePolicy(context.Context, int64) (*DynamicRatePolicy, error) {
	return r.policy, nil
}
func (r *dynamicRateRepoStub) GetDynamicRateUsage(context.Context, int64, int64, string) (DynamicRateUsageCounters, error) {
	return r.usage, nil
}
func (r *dynamicRateRepoStub) UpsertDynamicRatePolicy(_ context.Context, p *DynamicRatePolicy) (*DynamicRatePolicy, error) {
	return p, nil
}

func TestDynamicRateDefaultsShanghaiWindow(t *testing.T) {
	p := DefaultDynamicRatePolicy(7)
	if p.Timezone != "Asia/Shanghai" || p.ResetTime != "00:00" || p.Enabled {
		t.Fatalf("unexpected defaults: %+v", p)
	}
	at := time.Date(2026, 9, 11, 16, 30, 0, 0, time.UTC)
	windowID, start, end, err := DynamicRateWindow(p, at)
	if err != nil {
		t.Fatal(err)
	}
	if windowID != "2026-09-11T16:00:00Z" || !start.Equal(at.Truncate(time.Hour)) || end.Sub(start) != 24*time.Hour {
		t.Fatalf("unexpected window: id=%s start=%s end=%s", windowID, start, end)
	}
}

func TestDynamicRateRejectsUnadaptedModesAndProviderCache(t *testing.T) {
	p := DefaultDynamicRatePolicy(1)
	p.Enabled = true
	for _, mode := range []string{DynamicRateModeImage, DynamicRateModeAudio, DynamicRateModeVideo, DynamicRateModeBatchImage} {
		p.IncludedModes = []string{mode}
		if err := ValidateDynamicRatePolicy(p); err == nil {
			t.Fatalf("mode %s must be rejected", mode)
		}
	}
	p.IncludedModes = []string{DynamicRateModeText}
	p.CacheTokenPolicy = DynamicRateCachePolicyProvider
	if err := ValidateDynamicRatePolicy(p); err == nil {
		t.Fatal("provider cache policy must be rejected until normalized accounting is adapted")
	}
}

func TestDynamicRateEnabledPolicyRequiresAdmissionSnapshot(t *testing.T) {
	p := DefaultDynamicRatePolicy(1)
	p.Enabled = true
	p.Tiers = []DynamicRateTier{{ID: "low", Threshold: decimal.Zero, Factor: 1}, {ID: "high", Threshold: decimal.NewFromInt(100), Factor: 2}}
	r := newDynamicRateResolver(&dynamicRateRepoStub{policy: p, usage: DynamicRateUsageCounters{NormalizedTokens: 101}})
	if _, err := r.ResolveForBilling(context.Background(), nil, 9, 1, DynamicRateModeText, DynamicRateMetricTokensM); err != ErrDynamicRateSnapshotRequired {
		t.Fatalf("want snapshot required, got %v", err)
	}
	snapshot, err := r.Freeze(context.Background(), 9, 1, DynamicRateModeText, 1.2, 1, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TierID != "high" || snapshot.DynamicFactor != 2 || snapshot.FinalFactor != 2.4 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

type historicalDynamicRateRepoStub struct {
	dynamicRateRepoStub
	effective time.Time
}

func (r *historicalDynamicRateRepoStub) GetDynamicRatePolicyAt(_ context.Context, _ int64, at time.Time) (*DynamicRatePolicy, error) {
	if at.Before(r.effective) {
		return nil, nil
	}
	return r.policy, nil
}
func TestDynamicRateDisabledAtAdmissionIsNotRetrofittedAtSettlement(t *testing.T) {
	effective := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	policy := DefaultDynamicRatePolicy(8)
	policy.Enabled = true
	policy.PolicyVersion = 1
	policy.EffectiveAt = effective
	repo := &historicalDynamicRateRepoStub{dynamicRateRepoStub: dynamicRateRepoStub{policy: policy}, effective: effective}
	resolver := newDynamicRateResolver(repo)
	ctx := WithDynamicRateAdmissionTime(context.Background(), effective.Add(-time.Minute))
	snapshot, err := resolver.ResolveForBilling(ctx, nil, 4, 8, DynamicRateModeText, "")
	if err != nil || snapshot != nil {
		t.Fatalf("old request was repriced after activation: %v", err)
	}
	_, err = resolver.ResolveForBilling(WithDynamicRateAdmissionTime(ctx, effective), nil, 4, 8, DynamicRateModeText, "")
	if err != ErrDynamicRateSnapshotRequired {
		t.Fatalf("new requests still require admission snapshot: %v", err)
	}
}
func TestDynamicRateMediaSnapshotsFreezeImageBaseAndWalletWindow(t *testing.T) {
	p := DefaultDynamicRatePolicy(7)
	p.Enabled = true
	p.Metric = DynamicRateMetricWalletSpend
	p.IncludedModes = []string{DynamicRateModeText, DynamicRateModeImage, DynamicRateModeBatchImage}
	p.Tiers[0].Factor = 0.8
	repo := &dynamicRateRepoStub{policy: p, usage: DynamicRateUsageCounters{WalletSpent: decimal.NewFromInt(9)}}
	snapshot, err := newDynamicRateResolver(repo).Freeze(context.Background(), 4, 7, DynamicRateModeImage, 0.9, 1, time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), false)
	if err != nil {
		t.Fatal(err)
	}
	imageBase := 2.0
	snapshot.ImageBaseFactor = &imageBase
	snapshot.PricingSnapshotID = BuildDynamicRateSnapshotID(snapshot)
	text, image, _, _ := applyDynamicRateMultipliers(snapshot, 99, 99, 99, 99, 99)
	if text != snapshot.FinalFactor || image != 1.6 {
		t.Fatalf("frozen image/text factors changed: %v %v", text, image)
	}
	if err := ValidateDynamicRateSnapshotForRequest(snapshot, 4, 7, DynamicRateModeImage, DynamicRateMetricWalletSpend); err != nil {
		t.Fatal(err)
	}
	imageBase = 3
	if err := ValidateDynamicRateSnapshotForRequest(snapshot, 4, 7, DynamicRateModeImage, DynamicRateMetricWalletSpend); err == nil {
		t.Fatal("altered media price was accepted")
	}
}

func TestDynamicRateClockTransitionNeverOverlapsOldPolicyWindow(t *testing.T) {
	policy := DefaultDynamicRatePolicy(1)
	policy.ResetTime = "06:00"
	policy.EffectiveAt = time.Date(2026, 9, 11, 16, 0, 0, 0, time.UTC)
	_, start, end, err := DynamicRateWindow(policy, policy.EffectiveAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !start.Equal(policy.EffectiveAt) || !end.Equal(policy.EffectiveAt.Add(6*time.Hour)) {
		t.Fatalf("transition must be non-overlapping [midnight,06:00): %s %s", start, end)
	}
}
