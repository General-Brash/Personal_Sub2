//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminServiceCreateGroupRejectsEnabledCodexManifest(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{groupRepo: repo}

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:           "codex-manifest-create",
		Platform:       PlatformOpenAI,
		RateMultiplier: 1,
		CodexModelsManifestConfig: GroupCodexModelsManifestConfig{
			Enabled:    true,
			AccountIDs: []int64{101},
		},
	})

	require.Error(t, err)
	require.Nil(t, repo.created)
	require.Contains(t, err.Error(), "cannot be enabled at group creation")
}

func TestAdminServiceCreateGroupNormalizesCodexManifest(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		want     GroupCodexModelsManifestConfig
	}{
		{
			name:     "openai preserves personal selection while disabled",
			platform: PlatformOpenAI,
			want:     GroupCodexModelsManifestConfig{AccountIDs: []int64{9, 2}},
		},
		{
			name:     "non-openai silently zeros config",
			platform: PlatformAnthropic,
			want:     GroupCodexModelsManifestConfig{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &groupRepoStubForAdmin{}
			svc := &adminServiceImpl{groupRepo: repo}
			group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
				Name:           "codex-manifest-create-normalize",
				Platform:       tt.platform,
				RateMultiplier: 1,
				CodexModelsManifestConfig: GroupCodexModelsManifestConfig{
					AccountIDs: []int64{9, 0, 2, 9, -1},
				},
			})
			require.NoError(t, err)
			require.Equal(t, tt.want, group.CodexModelsManifestConfig)
		})
	}
}

func TestAdminServiceUpdateGroupValidatesCodexManifestMembersBeforePersist(t *testing.T) {
	existing := &Group{ID: 7, Platform: PlatformOpenAI, Status: StatusActive}
	groupRepo := &groupRepoStubForAdmin{getByID: existing}
	accountRepo := &accountRepoStubForBulkUpdate{listByGroupData: map[int64][]Account{
		existing.ID: {
			{ID: 101, Platform: PlatformOpenAI, Status: StatusActive},
			{ID: 202, Platform: PlatformOpenAI, Status: StatusDisabled},
			{ID: 303, Platform: PlatformAnthropic, Status: StatusActive},
		},
	}}
	svc := &adminServiceImpl{groupRepo: groupRepo, accountRepo: accountRepo}

	_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		CodexModelsManifestConfig: &GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{101, 202}},
	})

	require.Error(t, err)
	require.Nil(t, groupRepo.updated)
	require.Contains(t, err.Error(), "INVALID_CODEX_MODELS_MANIFEST_CONFIG")
}

func TestAdminServiceUpdateGroupNormalizesDisabledCodexManifestWithoutMemberLookup(t *testing.T) {
	existing := &Group{
		ID:       7,
		Platform: PlatformOpenAI,
		Status:   StatusActive,
		CodexModelsManifestConfig: GroupCodexModelsManifestConfig{
			Enabled:    true,
			AccountIDs: []int64{101, 202},
		},
	}
	groupRepo := &groupRepoStubForAdmin{getByID: existing}
	svc := &adminServiceImpl{groupRepo: groupRepo}

	updated, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		CodexModelsManifestConfig: &GroupCodexModelsManifestConfig{AccountIDs: []int64{202, 202, 0, 101}},
	})

	require.NoError(t, err)
	require.Equal(t, GroupCodexModelsManifestConfig{AccountIDs: []int64{202, 101}}, updated.CodexModelsManifestConfig)
}

func TestAdminServiceUpdateGroupZerosCodexManifestAfterPlatformChange(t *testing.T) {
	existing := &Group{
		ID:       7,
		Platform: PlatformOpenAI,
		Status:   StatusActive,
		CodexModelsManifestConfig: GroupCodexModelsManifestConfig{
			Enabled:             true,
			AccountIDs:          []int64{101},
			FallbackToScheduler: true,
		},
	}
	groupRepo := &groupRepoStubForAdmin{getByID: existing}
	svc := &adminServiceImpl{groupRepo: groupRepo}

	updated, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{Platform: PlatformAnthropic})

	require.NoError(t, err)
	require.Equal(t, GroupCodexModelsManifestConfig{}, updated.CodexModelsManifestConfig)
}

func TestCloneGroupForDuplicateResetsCodexManifestAndPreservesPersonalOpenAIFields(t *testing.T) {
	source := &Group{
		ID:                          7,
		Platform:                    PlatformOpenAI,
		ForceOpenAIFast:             true,
		FreeOpenAIFast:              true,
		MaxReasoningEffortOverLimit: "medium",
		CodexModelsManifestConfig: GroupCodexModelsManifestConfig{
			Enabled:             true,
			AccountIDs:          []int64{101, 202},
			FallbackToScheduler: true,
		},
	}

	duplicate := cloneGroupForDuplicate(source, "operation")

	require.Equal(t, GroupCodexModelsManifestConfig{}, duplicate.CodexModelsManifestConfig)
	require.True(t, duplicate.ForceOpenAIFast)
	require.True(t, duplicate.FreeOpenAIFast)
	require.Equal(t, source.MaxReasoningEffortOverLimit, duplicate.MaxReasoningEffortOverLimit)
}

func TestAdminServiceUpdateGroupAcceptsOrderedCodexManifestWithFallbackDisabled(t *testing.T) {
	existing := &Group{ID: 8, Platform: PlatformOpenAI, Status: StatusActive}
	groupRepo := &groupRepoStubForAdmin{getByID: existing}
	accountRepo := &accountRepoStubForBulkUpdate{listByGroupData: map[int64][]Account{
		existing.ID: {
			{ID: 101, Platform: PlatformOpenAI, Status: StatusActive},
			{ID: 202, Platform: PlatformOpenAI, Status: StatusActive},
			{ID: 303, Platform: PlatformOpenAI, Status: StatusActive},
		},
	}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		groupRepo: groupRepo, accountRepo: accountRepo, authCacheInvalidator: invalidator,
	}

	updated, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
		CodexModelsManifestConfig: &GroupCodexModelsManifestConfig{
			Enabled: true, AccountIDs: []int64{303, 101, 303, 202}, FallbackToScheduler: false,
		},
	})

	require.NoError(t, err)
	require.Equal(t, []int64{303, 101, 202}, updated.CodexModelsManifestConfig.AccountIDs)
	require.False(t, updated.CodexModelsManifestConfig.FallbackToScheduler)
	require.Equal(t, []int64{303, 101, 202}, groupRepo.updated.CodexModelsManifestConfig.AccountIDs)
	require.Equal(t, []int64{existing.ID}, invalidator.groupIDs)
}
