package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsIncludeBareGPT56Alias(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-5.6")
}

func TestDefaultModelsIncludeGPT6Astra(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-6-astra")
	require.Contains(t, DefaultModelIDs(), "gpt-6")
	var displayName string
	for _, model := range DefaultModels {
		if model.ID == "gpt-6-astra" {
			displayName = model.DisplayName
			break
		}
	}
	require.Equal(t, "GPT-6 Astra", displayName)
}

func TestDefaultModelsPreferConcreteGPT56SolForAccountTests(t *testing.T) {
	require.NotEmpty(t, DefaultModels)
	require.Equal(t, "gpt-5.6-sol", DefaultModels[0].ID)
}

func TestDefaultModelsIncludeGPTImage25Models(t *testing.T) {
	ids := DefaultModelIDs()
	require.Contains(t, ids, "gpt-image-2.5-flare")
	require.Contains(t, ids, "gpt-image-2.5-sunburst")

	for _, tc := range []struct {
		id          string
		displayName string
		created     int64
	}{
		{id: "gpt-image-2.5-flare", displayName: "GPT Image 2.5 Flare", created: 1788825600},
		{id: "gpt-image-2.5-sunburst", displayName: "GPT Image 2.5 Sunburst", created: 1788825600},
	} {
		found := false
		for _, model := range DefaultModels {
			if model.ID != tc.id {
				continue
			}
			found = true
			require.Equal(t, "model", model.Object)
			require.Equal(t, "openai", model.OwnedBy)
			require.Equal(t, "model", model.Type)
			require.Equal(t, tc.displayName, model.DisplayName)
			require.Equal(t, tc.created, model.Created)
			break
		}
		require.True(t, found, "expected DefaultModels to contain %s", tc.id)
	}
}

func TestDefaultModelsKeepLegacyImageDefaults(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-image-1")
	require.Contains(t, DefaultModelIDs(), "gpt-image-1.5")
	require.Contains(t, DefaultModelIDs(), "gpt-image-2")
	require.NotContains(t, DefaultModelIDs(), "gpt-image-2.5")
	require.NotContains(t, DefaultModelIDs(), "gpt-image-2.5-flare-2026-09-08")
	require.NotContains(t, DefaultModelIDs(), "gpt-image-2.5-sunburst-2026-09-08")
}
