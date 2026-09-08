package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupMapperRoundTripsCodexModelsManifestConfig(t *testing.T) {
	group := &service.Group{
		ID: 31, Name: "codex-manifest-dto", Platform: service.PlatformOpenAI, Status: service.StatusActive,
		CodexModelsManifestConfig: service.GroupCodexModelsManifestConfig{
			Enabled: true, AccountIDs: []int64{101, 202}, FallbackToScheduler: true,
		},
	}

	adminEnvelope := GroupFromServiceAdmin(group)
	require.NotNil(t, adminEnvelope)
	require.True(t, adminEnvelope.CodexModelsManifestConfig.Enabled)
	require.Equal(t, []int64{101, 202}, adminEnvelope.CodexModelsManifestConfig.AccountIDs)
	require.True(t, adminEnvelope.CodexModelsManifestConfig.FallbackToScheduler)

	userJSON, err := json.Marshal(GroupFromService(group))
	require.NoError(t, err)
	require.NotContains(t, string(userJSON), "codex_models_manifest_config")
}
