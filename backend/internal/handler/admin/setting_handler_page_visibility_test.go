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

type pageVisibilityResponse struct {
	Code int `json:"code"`
	Data struct {
		UserChannelStatusEnabled      bool `json:"user_channel_status_enabled"`
		UserSubscriptionsEnabled      bool `json:"user_subscriptions_enabled"`
		AdminSubscriptionsEnabled     bool `json:"admin_subscriptions_enabled"`
		AdminPromoCodesEnabled        bool `json:"admin_promo_codes_enabled"`
		AdminChannelManagementEnabled bool `json:"admin_channel_management_enabled"`
		AdminFinanceEnabled           bool `json:"admin_finance_enabled"`
		AdminBankTransactionsEnabled  bool `json:"admin_bank_transactions_enabled"`
		AdminAuditLogsEnabled         bool `json:"admin_audit_logs_enabled"`
		AdminOpsEnabled               bool `json:"admin_ops_enabled"`
	} `json:"data"`
}

func TestSettingHandler_GetSettings_ExposesPageVisibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: map[string]string{
		service.SettingKeyUserChannelStatusEnabled:      "false",
		service.SettingKeyUserSubscriptionsEnabled:      "true",
		service.SettingKeyAdminSubscriptionsEnabled:     "true",
		service.SettingKeyAdminPromoCodesEnabled:        "false",
		service.SettingKeyAdminChannelManagementEnabled: "true",
		service.SettingKeyAdminFinanceEnabled:           "false",
		service.SettingKeyAdminBankTransactionsEnabled:  "true",
		service.SettingKeyAdminAuditLogsEnabled:         "false",
		service.SettingKeyAdminOpsEnabled:               "true",
	}}
	handler := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), nil, nil, nil, nil, nil, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)

	handler.GetSettings(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp pageVisibilityResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.False(t, resp.Data.UserChannelStatusEnabled)
	require.True(t, resp.Data.UserSubscriptionsEnabled)
	require.True(t, resp.Data.AdminSubscriptionsEnabled)
	require.False(t, resp.Data.AdminPromoCodesEnabled)
	require.True(t, resp.Data.AdminChannelManagementEnabled)
	require.False(t, resp.Data.AdminFinanceEnabled)
	require.True(t, resp.Data.AdminBankTransactionsEnabled)
	require.False(t, resp.Data.AdminAuditLogsEnabled)
	require.True(t, resp.Data.AdminOpsEnabled)
}

func TestSettingHandler_UpdateSettings_MergesAndPersistsPageVisibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: map[string]string{
		service.SettingKeyPromoCodeEnabled:              "true",
		service.SettingKeyUserChannelStatusEnabled:      "true",
		service.SettingKeyUserSubscriptionsEnabled:      "true",
		service.SettingKeyAdminSubscriptionsEnabled:     "true",
		service.SettingKeyAdminPromoCodesEnabled:        "true",
		service.SettingKeyAdminChannelManagementEnabled: "true",
		service.SettingKeyAdminFinanceEnabled:           "true",
		service.SettingKeyAdminBankTransactionsEnabled:  "true",
		service.SettingKeyAdminAuditLogsEnabled:         "true",
		service.SettingKeyAdminOpsEnabled:               "true",
	}}
	handler := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), nil, nil, nil, nil, nil, nil)
	body, err := json.Marshal(map[string]any{
		"promo_code_enabled":          true,
		"user_channel_status_enabled": false,
		"admin_promo_codes_enabled":   false,
		"admin_finance_enabled":       false,
		"admin_audit_logs_enabled":    false,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.UpdateSettings(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyUserChannelStatusEnabled])
	require.Equal(t, "true", repo.values[service.SettingKeyUserSubscriptionsEnabled])
	require.Equal(t, "true", repo.values[service.SettingKeyAdminSubscriptionsEnabled])
	require.Equal(t, "false", repo.values[service.SettingKeyAdminPromoCodesEnabled])
	require.Equal(t, "true", repo.values[service.SettingKeyAdminChannelManagementEnabled])
	require.Equal(t, "false", repo.values[service.SettingKeyAdminFinanceEnabled])
	require.Equal(t, "true", repo.values[service.SettingKeyAdminBankTransactionsEnabled])
	require.Equal(t, "false", repo.values[service.SettingKeyAdminAuditLogsEnabled])
	require.Equal(t, "true", repo.values[service.SettingKeyAdminOpsEnabled])

	var resp pageVisibilityResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.False(t, resp.Data.UserChannelStatusEnabled)
	require.True(t, resp.Data.UserSubscriptionsEnabled)
	require.True(t, resp.Data.AdminSubscriptionsEnabled)
	require.False(t, resp.Data.AdminPromoCodesEnabled)
	require.True(t, resp.Data.AdminChannelManagementEnabled)
	require.False(t, resp.Data.AdminFinanceEnabled)
	require.True(t, resp.Data.AdminBankTransactionsEnabled)
	require.False(t, resp.Data.AdminAuditLogsEnabled)
	require.True(t, resp.Data.AdminOpsEnabled)
}

func TestDiffSettings_IncludesPageVisibility(t *testing.T) {
	before := &service.SystemSettings{
		UserChannelStatusEnabled:      true,
		UserSubscriptionsEnabled:      true,
		AdminSubscriptionsEnabled:     true,
		AdminPromoCodesEnabled:        true,
		AdminChannelManagementEnabled: true,
		AdminFinanceEnabled:           true,
		AdminBankTransactionsEnabled:  true,
		AdminAuditLogsEnabled:         true,
		AdminOpsEnabled:               true,
	}
	after := &service.SystemSettings{}

	changed := diffSettings(before, after, nil, nil, UpdateSettingsRequest{})

	require.ElementsMatch(t, []string{
		"user_channel_status_enabled",
		"user_subscriptions_enabled",
		"admin_subscriptions_enabled",
		"admin_promo_codes_enabled",
		"admin_channel_management_enabled",
		"admin_finance_enabled",
		"admin_bank_transactions_enabled",
		"admin_audit_logs_enabled",
		"admin_ops_enabled",
	}, changed)
}
