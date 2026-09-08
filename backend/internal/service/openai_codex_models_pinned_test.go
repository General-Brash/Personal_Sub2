package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestMergeCodexModelsManifestBodiesUnionAndOrder(t *testing.T) {
	first := `{"object":"codex.manifest","models":[{"slug":"model-a","display_name":"A from first"},{"slug":"model-b"}]}`
	second := `{"object":"codex.manifest","models":[{"slug":"model-a","display_name":"A from second"},{"slug":"model-c"}]}`

	merged, err := mergeCodexModelsManifestBodies([][]byte{[]byte(first), []byte(second)})
	require.NoError(t, err)

	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(merged, &envelope))
	var objectField string
	require.NoError(t, json.Unmarshal(envelope["object"], &objectField))
	require.Equal(t, "codex.manifest", objectField)

	var models []map[string]any
	require.NoError(t, json.Unmarshal(envelope["models"], &models))
	require.Len(t, models, 3)
	require.Equal(t, "model-a", models[0]["slug"])
	require.Equal(t, "A from first", models[0]["display_name"], "重复 slug 必须取配置顺序靠前账号的条目")
	require.Equal(t, "model-b", models[1]["slug"])
	require.Equal(t, "model-c", models[2]["slug"])
}

func TestMergeCodexModelsManifestBodiesEnvelopeFromFirstBody(t *testing.T) {
	first := `{"object":"codex.manifest","extra_field":"from-first","models":[{"slug":"model-a"}]}`
	second := `{"object":"other","extra_field":"from-second","another":"only-in-second","models":[]}`

	merged, err := mergeCodexModelsManifestBodies([][]byte{[]byte(first), []byte(second)})
	require.NoError(t, err)

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(merged, &envelope))
	require.Equal(t, "codex.manifest", envelope["object"])
	require.Equal(t, "from-first", envelope["extra_field"], "顶层字段以第一个信封为基底")
	require.NotContains(t, envelope, "another")
}

func TestMergeCodexModelsManifestBodiesHandlesSluglessEntries(t *testing.T) {
	first := `{"models":[{"slug":"model-a"},{"display_name":"no slug one"}]}`
	second := `{"models":[{"display_name":"no slug one"},{"display_name":"no slug two"},{"slug":"model-a"}]}`

	merged, err := mergeCodexModelsManifestBodies([][]byte{[]byte(first), []byte(second)})
	require.NoError(t, err)

	var envelope struct {
		Models []map[string]any `json:"models"`
	}
	require.NoError(t, json.Unmarshal(merged, &envelope))
	require.Len(t, envelope.Models, 3)
	require.Equal(t, "model-a", envelope.Models[0]["slug"])
	require.Equal(t, "no slug one", envelope.Models[1]["display_name"], "无 slug 条目按出现顺序保留一次")
	require.Equal(t, "no slug two", envelope.Models[2]["display_name"])
}

func TestMergeCodexModelsManifestBodiesSingleBody(t *testing.T) {
	body := `{"models":[{"slug":"model-a"}]}`

	merged, err := mergeCodexModelsManifestBodies([][]byte{[]byte(body)})
	require.NoError(t, err)
	require.JSONEq(t, body, string(merged))
}

func TestMergeCodexModelsManifestBodiesRejectsInvalidInput(t *testing.T) {
	_, err := mergeCodexModelsManifestBodies(nil)
	require.Error(t, err)

	_, err = mergeCodexModelsManifestBodies([][]byte{[]byte(`{`)})
	require.Error(t, err)

	_, err = mergeCodexModelsManifestBodies([][]byte{[]byte(`{}`), []byte(`{"models":{}`)})
	require.Error(t, err, "models 非数组必须报错")
}

// pinnedCodexModelsAccountRepo exposes only the group listing needed by the
// pinned manifest path; embedding keeps the stub scoped without introducing
// repository behavior that could touch storage.
type pinnedCodexModelsAccountRepo struct {
	AccountRepository
	accounts []Account
}

func (r pinnedCodexModelsAccountRepo) ListByGroup(_ context.Context, _ int64) ([]Account, error) {
	return append([]Account(nil), r.accounts...), nil
}

func newPinnedCodexService(accounts []Account, upstream HTTPUpstream) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		accountRepo:  pinnedCodexModelsAccountRepo{accounts: accounts},
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled: false,
		}}},
	}
}

func newPinnedCodexAPIKeyAccount(id int64, schedulable bool) Account {
	return Account{
		ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: schedulable,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  fmt.Sprintf("sk-pinned-%d", id),
			"base_url": fmt.Sprintf("https://pinned-%d.example/v1", id),
		},
	}
}

func TestFetchPinnedCodexModelsManifestSkipsInvalidMembersAndMergesInConfigOrder(t *testing.T) {
	accounts := []Account{
		newPinnedCodexAPIKeyAccount(10, true),
		newPinnedCodexAPIKeyAccount(20, true),
		newPinnedCodexAPIKeyAccount(30, false),
	}
	started := make(chan int64, 2)
	release := make(chan struct{})
	upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
		started <- accountID
		<-release
		body := map[int64]string{
			10: `{"object":"codex.manifest","models":[{"slug":"first"},{"slug":"shared","display_name":"from-10"}]}`,
			20: `{"object":"codex.manifest","models":[{"slug":"second"},{"slug":"shared","display_name":"from-20"}]}`,
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body[accountID]))}, nil
	}}
	svc := newPinnedCodexService(accounts, upstream)
	group := &Group{ID: 701, Platform: PlatformOpenAI, CodexModelsManifestConfig: GroupCodexModelsManifestConfig{
		Enabled: true, AccountIDs: []int64{999, 20, 30, 10},
	}}
	result := make(chan struct {
		manifest *CodexModelsManifest
		account  *Account
		err      error
	}, 1)
	go func() {
		manifest, account, err := svc.FetchPinnedCodexModelsManifest(context.Background(), group, "0.1")
		result <- struct {
			manifest *CodexModelsManifest
			account  *Account
			err      error
		}{manifest, account, err}
	}()
	require.ElementsMatch(t, []int64{20, 10}, []int64{<-started, <-started}, "only valid active+schedulable pinned members are fetched")
	close(release)
	got := <-result
	require.NoError(t, got.err)
	require.NotNil(t, got.manifest)
	require.NotNil(t, got.account)
	require.Equal(t, int64(20), got.account.ID, "first successful account follows configured order")
	require.Equal(t, []string{"second", "shared", "first"}, codexManifestModelSlugs(t, got.manifest.Body))
}

func TestFetchPinnedCodexModelsManifestPartialFailureKeepsSuccessfulBodies(t *testing.T) {
	accounts := []Account{newPinnedCodexAPIKeyAccount(1, true), newPinnedCodexAPIKeyAccount(2, true)}
	upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
		if accountID == 1 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(`{"error":"temporary"}`))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"models":[{"slug":"survivor"}]}`))}, nil
	}}
	svc := newPinnedCodexService(accounts, upstream)
	group := &Group{ID: 702, Platform: PlatformOpenAI, CodexModelsManifestConfig: GroupCodexModelsManifestConfig{
		Enabled: true, AccountIDs: []int64{1, 2},
	}}
	manifest, account, err := svc.FetchPinnedCodexModelsManifest(context.Background(), group, "")
	require.NoError(t, err)
	require.Equal(t, int64(2), account.ID)
	require.Equal(t, []string{"survivor"}, codexManifestModelSlugs(t, manifest.Body))
}

func TestFetchPinnedCodexModelsManifestAllFailureReturnsError(t *testing.T) {
	upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
		return nil, fmt.Errorf("account %d failed", accountID)
	}}
	svc := newPinnedCodexService([]Account{newPinnedCodexAPIKeyAccount(1, true), newPinnedCodexAPIKeyAccount(2, true)}, upstream)
	group := &Group{ID: 703, Platform: PlatformOpenAI, CodexModelsManifestConfig: GroupCodexModelsManifestConfig{
		Enabled: true, AccountIDs: []int64{1, 2},
	}}
	manifest, account, err := svc.FetchPinnedCodexModelsManifest(context.Background(), group, "")
	require.Error(t, err)
	require.Nil(t, manifest)
	require.Nil(t, account)
	require.Contains(t, err.Error(), "account 2 failed", "all-failure result is not silently replaced by scheduler output")
}

func TestFetchPinnedCodexModelsManifestNoUsableConfiguredIDDoesNotSchedule(t *testing.T) {
	upstreamCalls := atomic.Int32{}
	upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		upstreamCalls.Add(1)
		return nil, errors.New("must not be called")
	}}
	svc := newPinnedCodexService([]Account{newPinnedCodexAPIKeyAccount(1, false)}, upstream)
	group := &Group{ID: 704, Platform: PlatformOpenAI, CodexModelsManifestConfig: GroupCodexModelsManifestConfig{
		Enabled: true, AccountIDs: []int64{999, 1}, FallbackToScheduler: false,
	}}
	manifest, account, err := svc.FetchPinnedCodexModelsManifest(context.Background(), group, "")
	require.ErrorIs(t, err, ErrNoPinnedCodexModelsAccounts)
	require.Nil(t, manifest)
	require.Nil(t, account)
	require.Zero(t, upstreamCalls.Load())
}
