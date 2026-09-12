//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelAvailabilityResolver_AnonymousIsCatalogOnlyAndPrivateIsHidden(t *testing.T) {
	resolver := NewModelAvailabilityResolver()
	public := ModelCatalogItem{Routes: []ModelCatalogRoute{{GroupID: 1, AvailabilityKnown: true, Schedulable: true}}}
	result := resolver.Resolve(public, ModelAccessInput{Anonymous: true})
	require.Equal(t, ModelAvailabilityCatalogOnly, result.State)

	private := ModelCatalogItem{Routes: []ModelCatalogRoute{{GroupID: 2, Exclusive: true, AvailabilityKnown: true, Schedulable: true}}}
	result = resolver.Resolve(private, ModelAccessInput{Anonymous: true})
	require.Equal(t, ModelAvailabilityNotEntitled, result.State)
}

func TestModelAvailabilityResolver_DistinguishesEligibilityStates(t *testing.T) {
	resolver := NewModelAvailabilityResolver()
	item := ModelCatalogItem{Routes: []ModelCatalogRoute{
		{GroupID: 1, AvailabilityKnown: true, Schedulable: true},
		{GroupID: 2, AvailabilityKnown: true, Schedulable: false},
	}}
	result := resolver.Resolve(item, ModelAccessInput{AllowedGroupIDs: map[int64]struct{}{}})
	require.Equal(t, ModelAvailabilityEligible, result.State)
	require.Equal(t, []int64{1}, result.EligibleGroupIDs)
	require.Equal(t, []int64{2}, result.UnavailableGroups)

	result = resolver.Resolve(ModelCatalogItem{Routes: item.Routes[1:2]}, ModelAccessInput{AllowedGroupIDs: map[int64]struct{}{}})
	require.Equal(t, ModelAvailabilityTemporarilyUnavailable, result.State)

	result = resolver.Resolve(ModelCatalogItem{Routes: []ModelCatalogRoute{{GroupID: 3, Subscription: true, AvailabilityKnown: true, Schedulable: true}}}, ModelAccessInput{})
	require.Equal(t, ModelAvailabilityNotEntitled, result.State)

	result = resolver.Resolve(ModelCatalogItem{Routes: []ModelCatalogRoute{{GroupID: 4, AvailabilityKnown: false}}}, ModelAccessInput{})
	require.Equal(t, ModelAvailabilityUnknown, result.State)
}
