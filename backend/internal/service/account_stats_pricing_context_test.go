//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayServiceRecordUsage_WebSearchAccountingBypassesTokenGroupCard(t *testing.T) {
	for _, tt := range []struct {
		name         string
		pricePerCall *float64
		wantCost     float64
	}{
		{name: "default_search_price", wantCost: 0.03},
		{name: "group_search_price", pricePerCall: testPtrFloat64(0.005), wantCost: 0.015},
		{name: "explicit_free_search", pricePerCall: testPtrFloat64(0), wantCost: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{}
			billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(
				usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil,
			)
			groupID := int64(11)
			const model = "gpt-5.6-sol"
			group := &Group{
				ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 0.75,
				WebSearchPricePerCall: tt.pricePerCall,
				ModelPricing: []ChannelModelPricing{{
					Models: []string{model}, BillingMode: BillingModeToken,
					InputPrice: testPtrFloat64(1e-6), OutputPrice: testPtrFloat64(2e-6),
				}},
			}
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID: "search_account_stats_" + tt.name,
					Model:     model, UpstreamModel: model, WebSearchCalls: 3, Duration: time.Second,
				},
				APIKey: &APIKey{ID: 1000, GroupID: &groupID, Group: group},
				User:   &User{ID: 2000},
				Account: &Account{
					ID: 3000, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
					RateMultiplier: testPtrFloat64(2), Extra: map[string]any{"quota_limit": float64(100)},
				},
			})
			require.NoError(t, err)
			usageLog := requireUnifiedUsageLog(t, billingRepo, usageRepo)
			require.InDelta(t, tt.wantCost, usageLog.TotalCost, 1e-12)
			require.InDelta(t, tt.wantCost*0.75, usageLog.ActualCost, 1e-12)
			require.NotNil(t, usageLog.AccountStatsCost)
			require.InDelta(t, tt.wantCost, *usageLog.AccountStatsCost, 1e-12)
			require.InDelta(t, tt.wantCost*2, billingRepo.lastCmd.AccountQuotaCost, 1e-12)
		})
	}
}

func TestOpenAIGatewayServiceRecordUsage_AccountStatsUseGroupPricesBeforeDefaults(t *testing.T) {
	for _, tt := range []struct {
		name       string
		inputPrice float64
		wantCost   float64
	}{
		{name: "group_token_price", inputPrice: 0.0002, wantCost: 0.02},
		{name: "explicit_free_group_price", inputPrice: 0, wantCost: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{}
			billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
			svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(
				usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil,
			)
			const model = "gpt-5.1"
			svc.billingService = newTestBillingServiceWithPrices(map[string]*ModelPricing{
				model: {InputPricePerToken: 0.001, OutputPricePerToken: 0.002},
			})
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			groupID := int64(11)
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID: "group_account_stats_" + tt.name, Model: model, UpstreamModel: model,
					Usage: OpenAIUsage{InputTokens: 100}, Duration: time.Second,
				},
				APIKey: &APIKey{ID: 1000, GroupID: &groupID, Group: &Group{
					ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 0.75,
					ModelPricing: []ChannelModelPricing{{
						Models: []string{model}, BillingMode: BillingModeToken,
						InputPrice: testPtrFloat64(tt.inputPrice),
					}},
				}},
				User: &User{ID: 2000},
				Account: &Account{
					ID: 3000, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
					RateMultiplier: testPtrFloat64(2), Extra: map[string]any{"quota_limit": float64(100)},
				},
			})
			require.NoError(t, err)
			usageLog := requireUnifiedUsageLog(t, billingRepo, usageRepo)
			require.InDelta(t, tt.wantCost, usageLog.TotalCost, 1e-12)
			require.InDelta(t, tt.wantCost*0.75, usageLog.ActualCost, 1e-12)
			require.NotNil(t, usageLog.AccountStatsCost)
			require.InDelta(t, tt.wantCost, *usageLog.AccountStatsCost, 1e-12)
			require.InDelta(t, tt.wantCost*2, billingRepo.lastCmd.AccountQuotaCost, 1e-12)
		})
	}
}
