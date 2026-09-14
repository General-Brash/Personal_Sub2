package service

import (
	"context"
	"testing"
	"time"
)

type dynamicRateResolverAtRepoStub struct {
	current       *DynamicRatePolicy
	past          *DynamicRatePolicy
	future        *DynamicRatePolicy
	defaultAt     *DynamicRatePolicy
	boundary      time.Time
	usage         DynamicRateUsageCounters
	currentCalls  int
	policyAtCalls []time.Time
}

func (r *dynamicRateResolverAtRepoStub) GetDynamicRatePolicy(context.Context, int64) (*DynamicRatePolicy, error) {
	r.currentCalls++
	return r.current, nil
}

func (r *dynamicRateResolverAtRepoStub) GetDynamicRateUsage(context.Context, int64, int64, string) (DynamicRateUsageCounters, error) {
	return r.usage, nil
}

func (r *dynamicRateResolverAtRepoStub) UpsertDynamicRatePolicy(_ context.Context, policy *DynamicRatePolicy) (*DynamicRatePolicy, error) {
	return policy, nil
}

func (r *dynamicRateResolverAtRepoStub) GetDynamicRatePolicyAt(_ context.Context, _ int64, at time.Time) (*DynamicRatePolicy, error) {
	r.policyAtCalls = append(r.policyAtCalls, at)
	if r.defaultAt != nil {
		return r.defaultAt, nil
	}
	if at.Before(r.boundary) {
		return r.past, nil
	}
	return r.future, nil
}

func dynamicRateResolverTestPolicy(groupID, version int64, effectiveAt time.Time) *DynamicRatePolicy {
	policy := DefaultDynamicRatePolicy(groupID)
	policy.Enabled = true
	policy.Timezone = "UTC"
	policy.PolicyVersion = version
	policy.EffectiveAt = effectiveAt
	return policy
}

func TestDynamicRateResolverStatusAndFreezeUseSameExplicitAtPolicy(t *testing.T) {
	boundary := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	past := dynamicRateResolverTestPolicy(7, 11, boundary.Add(-24*time.Hour))
	future := dynamicRateResolverTestPolicy(7, 22, boundary)
	current := dynamicRateResolverTestPolicy(7, 99, boundary)
	repo := &dynamicRateResolverAtRepoStub{
		current:  current,
		past:     past,
		future:   future,
		boundary: boundary,
	}
	resolver := newDynamicRateResolver(repo)

	cases := []struct {
		name            string
		at              time.Time
		expectedVersion int64
	}{
		{name: "past", at: boundary.Add(-time.Hour), expectedVersion: past.PolicyVersion},
		{name: "effective boundary", at: boundary, expectedVersion: future.PolicyVersion},
		{name: "future", at: boundary.Add(24 * time.Hour), expectedVersion: future.PolicyVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, policy, err := resolver.Status(context.Background(), 42, 7, tc.at)
			if err != nil {
				t.Fatal(err)
			}
			if status == nil || policy == nil {
				t.Fatalf("expected status and policy, got status=%v policy=%v", status, policy)
			}
			if policy.PolicyVersion != tc.expectedVersion || status.PolicyVersion != tc.expectedVersion {
				t.Fatalf("status selected policy version %d, want %d: status=%+v policy=%+v", status.PolicyVersion, tc.expectedVersion, status, policy)
			}

			snapshot, err := resolver.Freeze(context.Background(), 42, 7, DynamicRateModeText, 1, 1, tc.at, false)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot == nil {
				t.Fatal("expected pricing snapshot")
			}
			if snapshot.PolicyVersion != tc.expectedVersion {
				t.Fatalf("freeze selected policy version %d, want %d: snapshot=%+v", snapshot.PolicyVersion, tc.expectedVersion, snapshot)
			}
			if status.WindowID != snapshot.WindowID {
				t.Fatalf("status/freeze selected different windows: status=%q snapshot=%q", status.WindowID, snapshot.WindowID)
			}
		})
	}

	if repo.currentCalls != 0 {
		t.Fatalf("explicit at lookups must not use the NOW policy path, got %d current calls", repo.currentCalls)
	}
	if len(repo.policyAtCalls) != len(cases)*2 {
		t.Fatalf("expected one historical lookup per Status and Freeze call, got %d", len(repo.policyAtCalls))
	}
}

func TestDynamicRateResolverStatusZeroAtUsesCurrentEffectiveTime(t *testing.T) {
	policy := dynamicRateResolverTestPolicy(7, 31, time.Unix(0, 0).UTC())
	repo := &dynamicRateResolverAtRepoStub{current: policy, defaultAt: policy}

	status, selected, err := newDynamicRateResolver(repo).Status(context.Background(), 42, 7, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if status == nil || selected == nil {
		t.Fatalf("expected status and policy for zero at, got status=%v policy=%v", status, selected)
	}
	if selected.PolicyVersion != policy.PolicyVersion || status.PolicyVersion != policy.PolicyVersion {
		t.Fatalf("zero at selected unexpected policy: status=%+v policy=%+v", status, selected)
	}
	if repo.currentCalls != 0 {
		t.Fatalf("zero at should use the shared effective-at helper, got %d current calls", repo.currentCalls)
	}
	if len(repo.policyAtCalls) != 1 || repo.policyAtCalls[0].IsZero() {
		t.Fatalf("zero at must be normalized before historical selection: calls=%v", repo.policyAtCalls)
	}
}
