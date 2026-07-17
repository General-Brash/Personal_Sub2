package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type updateDailyCheckinSettingsRequest struct {
	Enabled      *bool                        `json:"enabled"`
	MaxRewardDay *int                         `json:"max_reward_day"`
	RewardTiers  *[]dailyCheckinRewardTierDTO `json:"reward_tiers"`
}

type dailyCheckinRewardTierDTO struct {
	Day    int    `json:"day"`
	Amount string `json:"amount"`
}

type dailyCheckinPolicyDTO struct {
	Enabled      bool                        `json:"enabled"`
	MaxRewardDay int                         `json:"max_reward_day"`
	RewardTiers  []dailyCheckinRewardTierDTO `json:"reward_tiers"`
}

func (r updateDailyCheckinSettingsRequest) toPolicy() (*service.DailyCheckinPolicy, error) {
	if r.Enabled == nil || r.MaxRewardDay == nil || r.RewardTiers == nil {
		return nil, service.ErrDailyCheckinPolicyInvalid
	}

	rewardTiers := make([]service.DailyCheckinRewardTier, len(*r.RewardTiers))
	for index, tier := range *r.RewardTiers {
		amount, err := service.ParseStrictPositiveLedgerAmount(tier.Amount)
		if err != nil {
			return nil, service.ErrDailyCheckinPolicyInvalid
		}
		rewardTiers[index] = service.DailyCheckinRewardTier{
			Day:    tier.Day,
			Amount: amount,
		}
	}

	return &service.DailyCheckinPolicy{
		Enabled:      *r.Enabled,
		MaxRewardDay: *r.MaxRewardDay,
		RewardTiers:  rewardTiers,
	}, nil
}

func newDailyCheckinPolicyDTO(policy *service.DailyCheckinPolicy) *dailyCheckinPolicyDTO {
	if policy == nil {
		return nil
	}

	rewardTiers := make([]dailyCheckinRewardTierDTO, len(policy.RewardTiers))
	for index, tier := range policy.RewardTiers {
		rewardTiers[index] = dailyCheckinRewardTierDTO{
			Day:    tier.Day,
			Amount: strconv.FormatFloat(tier.Amount, 'f', 8, 64),
		}
	}

	return &dailyCheckinPolicyDTO{
		Enabled:      policy.Enabled,
		MaxRewardDay: policy.MaxRewardDay,
		RewardTiers:  rewardTiers,
	}
}

// GetDailyCheckinSettings handles GET /api/v1/admin/settings/checkin.
func (h *SettingHandler) GetDailyCheckinSettings(c *gin.Context) {
	policy, err := h.settingService.GetDailyCheckinPolicy(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, newDailyCheckinPolicyDTO(policy))
}

// UpdateDailyCheckinSettings handles PUT /api/v1/admin/settings/checkin.
func (h *SettingHandler) UpdateDailyCheckinSettings(c *gin.Context) {
	var req updateDailyCheckinSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, service.ErrDailyCheckinPolicyInvalid)
		return
	}

	policy, err := req.toPolicy()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := h.settingService.UpdateDailyCheckinPolicy(c.Request.Context(), policy); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	updated, err := h.settingService.GetDailyCheckinPolicy(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, newDailyCheckinPolicyDTO(updated))
}
