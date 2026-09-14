package service

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

type p35EntitlementRepo struct {
	EntitlementRepository
	snapshot *EntitlementSnapshot
	calls    int
}

func (r *p35EntitlementRepo) ResolveEntitlement(context.Context, int64) (*EntitlementSnapshot, error) {
	r.calls++
	return r.snapshot, nil
}
func (r *p35EntitlementRepo) PreviewEntitlementChange(context.Context, []int64, string) (*EntitlementChangePreview, error) {
	r.calls++
	return &EntitlementChangePreview{}, nil
}
func (r *p35EntitlementRepo) ApplyEntitlementChange(context.Context, []int64, string, int64, string, string, string) (*EntitlementChangeResult, error) {
	r.calls++
	return &EntitlementChangeResult{}, nil
}
func (r *p35EntitlementRepo) UpdateEntitlementTierPolicy(context.Context, UpdateEntitlementTierPolicyInput, int64) (*EntitlementTierPolicy, error) {
	r.calls++
	return &EntitlementTierPolicy{}, nil
}

func TestEntitlementWriteTierRejectsBlankWithoutChangingReadCompatibility(t *testing.T) {
	require.Equal(t, EntitlementTierStandard, NormalizeEntitlementTier("  "))
	for _, tier := range []string{"", " ", "\t\n", "gold", "super_admin"} {
		t.Run(tier, func(t *testing.T) {
			repo := &p35EntitlementRepo{}
			svc := NewEntitlementService(repo)
			version := int64(1)
			_, err := svc.PreviewTierChange(context.Background(), []int64{7}, tier)
			require.ErrorIs(t, err, ErrEntitlementTierUnknown)
			_, err = svc.ApplyTierChange(context.Background(), []int64{7}, tier, 1, "reason", "key", "token")
			require.ErrorIs(t, err, ErrEntitlementTierUnknown)
			_, err = svc.UpdateTierPolicy(context.Background(), UpdateEntitlementTierPolicyInput{Tier: tier, Reason: "reason", RequestID: "key", ExpectedVersion: &version}, 1)
			require.ErrorIs(t, err, ErrEntitlementTierUnknown)
			require.Zero(t, repo.calls)
		})
	}
	require.Equal(t, EntitlementTierPremium, NormalizeEntitlementWriteTier(" premium "))
}

func TestEntitlementSnapshotNonNullCollectionsPreserveRealData(t *testing.T) {
	repo := &p35EntitlementRepo{snapshot: &EntitlementSnapshot{Tier: "", Sources: []EntitlementSource{{Source: "manual", Tier: EntitlementTierPremium}}, ManualGroups: []int64{4}}}
	snapshot, err := NewEntitlementService(repo).Resolve(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, EntitlementTierStandard, snapshot.Tier)
	body, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var wire map[string]any
	require.NoError(t, json.Unmarshal(body, &wire))
	for _, field := range []string{"allowed_groups", "tier_groups", "sources", "manual_groups", "subscription_groups"} {
		require.IsType(t, []any{}, wire[field], field)
	}
	require.Len(t, wire["sources"], 1)
	require.Equal(t, []int64{4}, snapshot.ManualGroups)
	require.NotNil(t, snapshot.DefaultRates)
}

func TestEntitlementInvalidTargetsAndMissingWriteMetadataMakeNoRepositoryCall(t *testing.T) {
	repo := &p35EntitlementRepo{}
	svc := NewEntitlementService(repo)
	for _, ids := range [][]int64{nil, {}, {0}, {7, -1}, make([]int64, 1001)} {
		_, err := svc.PreviewTierChange(context.Background(), ids, "premium")
		require.ErrorIs(t, err, ErrEntitlementTargetsInvalid)
		_, err = svc.ApplyTierChange(context.Background(), ids, "premium", 1, "reason", "key", "token")
		require.ErrorIs(t, err, ErrEntitlementTargetsInvalid)
	}
	_, err := svc.ApplyTierChange(context.Background(), []int64{7}, "premium", 1, "reason", "", "token")
	require.ErrorIs(t, err, ErrEntitlementRequestIDRequired)
	_, err = svc.ApplyTierChange(context.Background(), []int64{7}, "premium", 1, " ", "key", "token")
	require.ErrorIs(t, err, ErrEntitlementReasonRequired)
	require.Zero(t, repo.calls)
}
