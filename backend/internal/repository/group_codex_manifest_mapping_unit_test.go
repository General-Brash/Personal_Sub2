package repository

import (
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestGroupEntityToServicePreservesCodexManifest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config domain.GroupCodexModelsManifestConfig
	}{
		{name: "legacy default"},
		{name: "enabled without fallback", config: domain.GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{22, 11}}},
		{name: "enabled with fallback", config: domain.GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{11, 22}, FallbackToScheduler: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := groupEntityToService(&dbent.Group{ID: 7, CodexModelsManifestConfig: tc.config, AllowBatchImageGeneration: true, BatchImageHoldMultiplier: 0.6})
			require.Equal(t, tc.config, got.CodexModelsManifestConfig)
			require.True(t, got.AllowBatchImageGeneration)
			require.Equal(t, 0.6, got.BatchImageHoldMultiplier)
		})
	}
	require.Nil(t, groupEntityToService(nil))
}
