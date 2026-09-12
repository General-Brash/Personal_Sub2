package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type liveEntitlementStub struct {
	snapshot *EntitlementSnapshot
	err      error
}

func (s *liveEntitlementStub) Resolve(context.Context, int64) (*EntitlementSnapshot, error) {
	return s.snapshot, s.err
}

func TestAPIKeyLiveEntitlementsRevocationDoesNotMutateCachedUser(t *testing.T) {
	original := &APIKey{UserID: 7, User: &User{ID: 7, AllowedGroups: []int64{2, 9}}}
	resolver := &liveEntitlementStub{snapshot: &EntitlementSnapshot{UserID: 7, Tier: EntitlementTierPremium, Version: 4, AllowedGroups: []int64{2, 9}}}
	svc := &APIKeyService{entitlements: resolver}
	first, err := svc.applyLiveEntitlements(context.Background(), original)
	if err != nil || !first.User.CanBindGroup(9, true) {
		t.Fatalf("first resolution: %v", err)
	}
	resolver.snapshot = &EntitlementSnapshot{UserID: 7, Tier: EntitlementTierStandard, Version: 5, AllowedGroups: []int64{2}}
	next, err := svc.applyLiveEntitlements(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	if next.User.CanBindGroup(9, true) || !next.User.CanBindGroup(2, true) {
		t.Fatal("revocation must remove only tier access on the next cached-key use")
	}
	if !reflect.DeepEqual(original.User.AllowedGroups, []int64{2, 9}) {
		t.Fatal("credential cache was mutated")
	}
	if next.User.EntitlementVersion != 5 {
		t.Fatal("effective version was not refreshed")
	}
}

func TestAPIKeyLiveEntitlementsFailureCannotGrantAccess(t *testing.T) {
	svc := &APIKeyService{entitlements: &liveEntitlementStub{err: errors.New("resolver unavailable")}}
	key, err := svc.applyLiveEntitlements(context.Background(), &APIKey{UserID: 7, User: &User{ID: 7}})
	if err == nil || key != nil {
		t.Fatal("entitlement errors must fail closed")
	}
}
