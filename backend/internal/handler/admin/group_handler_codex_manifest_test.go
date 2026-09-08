//go:build unit

package admin

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCreateGroupRequestDecodesCodexModelsManifestConfig(t *testing.T) {
	var req CreateGroupRequest
	err := json.Unmarshal([]byte(`{
		"name":"manifest-group",
		"platform":"openai",
		"rate_multiplier":1,
		"codex_models_manifest_config":{
			"enabled":true,
			"account_ids":[11,22],
			"fallback_to_scheduler":true
		}
	}`), &req)

	require.NoError(t, err)
	require.Equal(t, service.GroupCodexModelsManifestConfig{
		Enabled:             true,
		AccountIDs:          []int64{11, 22},
		FallbackToScheduler: true,
	}, req.CodexModelsManifestConfig)
}

func TestUpdateGroupRequestDistinguishesAbsentAndExplicitCodexManifestConfig(t *testing.T) {
	var absent UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"manifest-group"}`), &absent))
	require.Nil(t, absent.CodexModelsManifestConfig)

	var explicit UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"name":"manifest-group",
		"codex_models_manifest_config":{
			"enabled":false,
			"account_ids":[22,11]
		}
	}`), &explicit))
	require.NotNil(t, explicit.CodexModelsManifestConfig)
	require.Equal(t, service.GroupCodexModelsManifestConfig{AccountIDs: []int64{22, 11}}, *explicit.CodexModelsManifestConfig)
}
