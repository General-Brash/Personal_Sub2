package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type quickJumpResponse struct {
	Code int `json:"code"`
	Data struct {
		QuickJumpEnabled bool `json:"quick_jump_enabled"`
		QuickJumpItems   []struct {
			ID         string `json:"id"`
			Label      string `json:"label"`
			URL        string `json:"url"`
			Visibility string `json:"visibility"`
			SortOrder  int    `json:"sort_order"`
		} `json:"quick_jump_items"`
		OIDCConsentPromptMode string `json:"oidc_consent_prompt_mode"`
	} `json:"data"`
}

func newQuickJumpHandler(values map[string]string) (*SettingHandler, *settingHandlerRepoStub) {
	gin.SetMode(gin.TestMode)
	if values == nil {
		values = map[string]string{}
	}
	values[service.SettingKeyPromoCodeEnabled] = "true"
	repo := &settingHandlerRepoStub{values: values}
	return NewSettingHandler(service.NewSettingService(repo, &config.Config{}), nil, nil, nil, nil, nil, nil), repo
}

func putSettings(t *testing.T, handler *SettingHandler, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.UpdateSettings(c)
	return recorder
}

func TestSettingHandler_UpdateSettings_PersistsQuickJumpItems(t *testing.T) {
	handler, repo := newQuickJumpHandler(nil)

	recorder := putSettings(t, handler, map[string]any{
		"promo_code_enabled": true,
		"quick_jump_enabled": true,
		"quick_jump_items": []map[string]any{
			{"id": "forum", "label": "社区论坛", "url": "https://forum.example.com", "visibility": "user", "sort_order": 0},
			{"id": "ops", "label": "运维面板", "url": "https://ops.example.com", "visibility": "admin", "sort_order": 1},
		},
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyQuickJumpEnabled])

	var stored []map[string]any
	require.NoError(t, json.Unmarshal([]byte(repo.values[service.SettingKeyQuickJumpItems]), &stored))
	require.Len(t, stored, 2)
	require.Equal(t, "https://forum.example.com", stored[0]["url"])
	require.Equal(t, "admin", stored[1]["visibility"])

	var resp quickJumpResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.True(t, resp.Data.QuickJumpEnabled)
	require.Len(t, resp.Data.QuickJumpItems, 2)
}

// 未提交 quick_jump_items 时必须保留原值，否则管理员改任何其他设置都会清空跳转配置。
func TestSettingHandler_UpdateSettings_OmittedQuickJumpItemsKeepsPrevious(t *testing.T) {
	previous := `[{"id":"forum","label":"社区论坛","icon_svg":"","url":"https://forum.example.com","visibility":"user","sort_order":0}]`
	handler, repo := newQuickJumpHandler(map[string]string{
		service.SettingKeyQuickJumpItems:   previous,
		service.SettingKeyQuickJumpEnabled: "true",
	})

	recorder := putSettings(t, handler, map[string]any{"promo_code_enabled": true})

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, previous, repo.values[service.SettingKeyQuickJumpItems])
	require.Equal(t, "true", repo.values[service.SettingKeyQuickJumpEnabled])
}

func TestSettingHandler_UpdateSettings_RejectsInvalidQuickJumpItems(t *testing.T) {
	tooMany := make([]map[string]any, 0, 21)
	for i := 0; i < 21; i++ {
		tooMany = append(tooMany, map[string]any{"label": "x", "url": "https://example.com", "visibility": "user"})
	}

	cases := []struct {
		name  string
		items []map[string]any
	}{
		{
			name:  "超过 20 条",
			items: tooMany,
		},
		{
			name:  "label 为空",
			items: []map[string]any{{"label": "  ", "url": "https://example.com", "visibility": "user"}},
		},
		{
			name:  "非 http(s) 协议",
			items: []map[string]any{{"label": "x", "url": "javascript:alert(1)", "visibility": "user"}},
		},
		{
			// 快捷跳转只做新标签外链，不支持自定义菜单那套 md:<slug> 内嵌页
			name:  "md:slug 内嵌页语法",
			items: []map[string]any{{"label": "x", "url": "md:help", "visibility": "user"}},
		},
		{
			name:  "相对路径",
			items: []map[string]any{{"label": "x", "url": "/admin/settings", "visibility": "user"}},
		},
		{
			name:  "visibility 非法",
			items: []map[string]any{{"label": "x", "url": "https://example.com", "visibility": "everyone"}},
		},
		{
			name: "ID 重复",
			items: []map[string]any{
				{"id": "dup", "label": "a", "url": "https://a.example.com", "visibility": "user"},
				{"id": "dup", "label": "b", "url": "https://b.example.com", "visibility": "user"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, repo := newQuickJumpHandler(nil)
			recorder := putSettings(t, handler, map[string]any{
				"promo_code_enabled": true,
				"quick_jump_items":   tc.items,
			})
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Empty(t, repo.values[service.SettingKeyQuickJumpItems], "校验失败不得写入任何条目")
		})
	}
}

// 缺失 ID 时由服务端补齐，避免前端新增条目后 key 冲突。
func TestSettingHandler_UpdateSettings_GeneratesMissingQuickJumpItemID(t *testing.T) {
	handler, repo := newQuickJumpHandler(nil)

	recorder := putSettings(t, handler, map[string]any{
		"promo_code_enabled": true,
		"quick_jump_items": []map[string]any{
			{"label": "社区", "url": "https://forum.example.com", "visibility": "user"},
		},
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	var stored []map[string]any
	require.NoError(t, json.Unmarshal([]byte(repo.values[service.SettingKeyQuickJumpItems]), &stored))
	require.Len(t, stored, 1)
	require.NotEmpty(t, stored[0]["id"])
}

func TestSettingHandler_UpdateSettings_OIDCConsentPromptMode(t *testing.T) {
	t.Run("接受 remember", func(t *testing.T) {
		handler, repo := newQuickJumpHandler(nil)
		recorder := putSettings(t, handler, map[string]any{
			"promo_code_enabled":       true,
			"oidc_consent_prompt_mode": "remember",
		})
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, service.OIDCConsentPromptModeRemember, repo.values[service.SettingKeyOIDCConsentPromptMode])
	})

	t.Run("拒绝非法值", func(t *testing.T) {
		handler, repo := newQuickJumpHandler(nil)
		recorder := putSettings(t, handler, map[string]any{
			"promo_code_enabled":       true,
			"oidc_consent_prompt_mode": "skip",
		})
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		require.Empty(t, repo.values[service.SettingKeyOIDCConsentPromptMode])
	})

	t.Run("未提交时保留原值", func(t *testing.T) {
		handler, repo := newQuickJumpHandler(map[string]string{
			service.SettingKeyOIDCConsentPromptMode: service.OIDCConsentPromptModeRemember,
		})
		recorder := putSettings(t, handler, map[string]any{"promo_code_enabled": true})
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, service.OIDCConsentPromptModeRemember, repo.values[service.SettingKeyOIDCConsentPromptMode])
	})
}
