package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// 固定账号 manifest 配置必须在认证快照 JSON 往返后完整保留，
// 否则开关在缓存路径上会静默失效（投影对账由集成测试兜底，此处覆盖快照序列化）。
func TestAPIKeyAuthSnapshotGroupCodexModelsManifestRoundtrip(t *testing.T) {
	groupID := int64(60)
	apiKey := &APIKey{
		ID: 92, UserID: 46, GroupID: &groupID, Key: "sk-codex-manifest-roundtrip", Status: StatusActive,
		User: &User{ID: 46, Status: StatusActive},
		Group: &Group{
			ID: groupID, Name: "codex-manifest-roundtrip", Platform: PlatformOpenAI, Status: StatusActive,
			Hydrated: true,
			CodexModelsManifestConfig: GroupCodexModelsManifestConfig{
				Enabled:             true,
				AccountIDs:          []int64{7, 8},
				FallbackToScheduler: true,
			},
		},
	}
	svc := &APIKeyService{}

	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), apiKey)})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &cached))

	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.NotNil(t, materialized.Group)
	require.True(t, materialized.Group.CodexModelsManifestConfig.Enabled)
	require.Equal(t, []int64{7, 8}, materialized.Group.CodexModelsManifestConfig.AccountIDs)
	require.True(t, materialized.Group.CodexModelsManifestConfig.FallbackToScheduler)
	require.Equal(t, apiKeyAuthSnapshotVersion, cached.Snapshot.Version)
}

type codexManifestAuthRepoStub struct {
	APIKeyRepository
	getByKeyForAuth   func(context.Context, string) (*APIKey, error)
	listKeysByGroupID func(context.Context, int64) ([]string, error)
}

func (r *codexManifestAuthRepoStub) GetByKeyForAuth(ctx context.Context, key string) (*APIKey, error) {
	if r.getByKeyForAuth == nil {
		return nil, ErrAPIKeyNotFound
	}
	return r.getByKeyForAuth(ctx, key)
}

func (r *codexManifestAuthRepoStub) ListKeysByGroupID(ctx context.Context, groupID int64) ([]string, error) {
	if r.listKeysByGroupID == nil {
		return nil, nil
	}
	return r.listKeysByGroupID(ctx, groupID)
}

type codexManifestAuthCacheStub struct {
	APIKeyCache
	entry     *APIKeyAuthCacheEntry
	deleted   []string
	published []string
}

func (c *codexManifestAuthCacheStub) GetAuthCache(context.Context, string) (*APIKeyAuthCacheEntry, error) {
	return c.entry, nil
}

func (c *codexManifestAuthCacheStub) DeleteAuthCache(_ context.Context, key string) error {
	c.deleted = append(c.deleted, key)
	return nil
}

func (c *codexManifestAuthCacheStub) PublishAuthCacheInvalidation(_ context.Context, key string) error {
	c.published = append(c.published, key)
	return nil
}

func TestAPIKeyAuthCacheL2ProjectionPreservesCodexManifestConfig(t *testing.T) {
	groupID := int64(61)
	key := "sk-codex-manifest-l2"
	source := &APIKey{
		ID: 93, UserID: 47, GroupID: &groupID, Key: key, Status: StatusActive,
		User: &User{ID: 47, Status: StatusActive},
		Group: &Group{
			ID: groupID, Name: "codex-manifest-l2", Platform: PlatformOpenAI, Status: StatusActive,
			Hydrated: true,
			CodexModelsManifestConfig: GroupCodexModelsManifestConfig{
				Enabled: true, AccountIDs: []int64{17, 18}, FallbackToScheduler: false,
			},
		},
	}
	producer := &APIKeyService{}
	entry := &APIKeyAuthCacheEntry{Snapshot: producer.snapshotFromAPIKey(context.Background(), source)}
	cache := &codexManifestAuthCacheStub{entry: entry}
	repo := &codexManifestAuthRepoStub{}
	svc := NewAPIKeyService(repo, nil, nil, nil, nil, cache, &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 60},
	})

	materialized, err := svc.GetByKey(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, materialized)
	require.NotNil(t, materialized.Group)
	require.True(t, materialized.Group.CodexModelsManifestConfig.Enabled)
	require.Equal(t, []int64{17, 18}, materialized.Group.CodexModelsManifestConfig.AccountIDs)
	require.False(t, materialized.Group.CodexModelsManifestConfig.FallbackToScheduler)
}

func TestAPIKeyAuthCacheGroupInvalidationDeletesAndPublishesEveryBoundKey(t *testing.T) {
	const groupID int64 = 62
	repo := &codexManifestAuthRepoStub{
		listKeysByGroupID: func(_ context.Context, gotGroupID int64) ([]string, error) {
			require.Equal(t, groupID, gotGroupID)
			return []string{"key-one", "", "key-two"}, nil
		},
	}
	cache := &codexManifestAuthCacheStub{}
	svc := &APIKeyService{apiKeyRepo: repo, cache: cache}

	svc.InvalidateAuthCacheByGroupID(context.Background(), groupID)

	want := []string{svc.authCacheKey("key-one"), svc.authCacheKey("key-two")}
	require.Equal(t, want, cache.deleted)
	require.Equal(t, want, cache.published)
	require.NotEqual(t, "key-one", cache.deleted[0])
}
