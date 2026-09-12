package handler

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type CheckinAdminAPIService interface {
	GetDailyCheckinPolicyV2(ctx context.Context) (*service.DailyCheckinPolicy, service.DailyCheckinPolicyV2, error)
	UpdateDailyCheckinPolicyV2(ctx context.Context, policy *service.DailyCheckinPolicy, extended *service.DailyCheckinPolicyV2, expectedVersion ...string) error
}

type CheckinAdminHandler struct {
	settingService CheckinAdminAPIService
}

func NewCheckinAdminHandler(settingService CheckinAdminAPIService) *CheckinAdminHandler {
	return &CheckinAdminHandler{settingService: settingService}
}

func ProvideCheckinAdminHandler(settingService *service.SettingService) *CheckinAdminHandler {
	return NewCheckinAdminHandler(settingService)
}

type checkinRewardTierDTO struct {
	Day             int    `json:"day"`
	Amount          string `json:"amount"`
	PermanentAmount string `json:"permanent_amount"`
}

type checkinAdminRandomDTO struct {
	Enabled bool `json:"enabled"`
	MinBps  int  `json:"min_bps"`
	MaxBps  int  `json:"max_bps"`
}

type checkinAdminSuperDTO struct {
	Enabled bool   `json:"enabled"`
	MinBps  int    `json:"min_bps"`
	MaxBps  int    `json:"max_bps"`
	Cost    string `json:"cost"`
}

type checkinAdminPolicyV2DTO struct {
	Enabled         bool                           `json:"enabled"`
	MaxRewardDay    int                            `json:"max_reward_day"`
	RewardTiers     []checkinRewardTierDTO         `json:"reward_tiers"`
	Version         string                         `json:"version"`
	RefreshTime     string                         `json:"refresh_time"`
	AutoFeeBps      int                            `json:"auto_fee_bps"`
	Reviewed        bool                           `json:"reviewed"`
	Normal          checkinAdminRandomDTO          `json:"normal"`
	Super           checkinAdminSuperDTO           `json:"super"`
	ExpectedVersion string                         `json:"expected_version,omitempty"`
	PendingRefresh  *service.DailyCheckinPendingV2 `json:"pending_refresh,omitempty"`
	NextResetAt     *time.Time                     `json:"next_reset_at,omitempty"`
}

func (dto checkinAdminPolicyV2DTO) toPolicy() (*service.DailyCheckinPolicy, *service.DailyCheckinPolicyV2, error) {
	tiers := make([]service.DailyCheckinRewardTier, len(dto.RewardTiers))
	for index, tier := range dto.RewardTiers {
		amount, err := service.ParseStrictPositiveLedgerAmount(tier.Amount)
		if err != nil {
			return nil, nil, service.ErrDailyCheckinPolicyInvalid
		}
		permanent, err := service.ParseStrictLedgerAmount(tier.PermanentAmount)
		if err != nil || permanent < 0 {
			return nil, nil, service.ErrDailyCheckinPolicyInvalid
		}
		tiers[index] = service.DailyCheckinRewardTier{Day: tier.Day, Amount: amount, PermanentAmount: permanent}
	}
	cost, err := service.ParseStrictLedgerAmount(dto.Super.Cost)
	if err != nil || cost < 0 {
		return nil, nil, service.ErrDailyCheckinPolicyInvalid
	}
	return &service.DailyCheckinPolicy{Enabled: dto.Enabled, MaxRewardDay: dto.MaxRewardDay, RewardTiers: tiers},
		&service.DailyCheckinPolicyV2{
			Version: dto.Version, RefreshTime: dto.RefreshTime, AutoFeeBps: dto.AutoFeeBps,
			Normal:         service.DailyCheckinRandomV2{Enabled: dto.Normal.Enabled, MinBps: dto.Normal.MinBps, MaxBps: dto.Normal.MaxBps},
			Super:          service.DailyCheckinSuperV2{Enabled: dto.Super.Enabled, MinBps: dto.Super.MinBps, MaxBps: dto.Super.MaxBps, Cost: cost},
			ReviewApproved: dto.Reviewed,
		}, nil
}

func newCheckinAdminPolicyV2DTO(base *service.DailyCheckinPolicy, extended service.DailyCheckinPolicyV2) checkinAdminPolicyV2DTO {
	tiers := make([]checkinRewardTierDTO, len(base.RewardTiers))
	for index, tier := range base.RewardTiers {
		tiers[index] = checkinRewardTierDTO{Day: tier.Day, Amount: strconv.FormatFloat(tier.Amount, 'f', 8, 64), PermanentAmount: strconv.FormatFloat(tier.PermanentAmount, 'f', 8, 64)}
	}
	var nextResetAt *time.Time
	if period, err := extended.CurrentPeriod(time.Now()); err == nil {
		utc := period.NextReset.UTC()
		nextResetAt = &utc
	}
	return checkinAdminPolicyV2DTO{
		Enabled: base.Enabled, MaxRewardDay: base.MaxRewardDay, RewardTiers: tiers,
		Version: extended.Version, RefreshTime: extended.RefreshTime, AutoFeeBps: extended.AutoFeeBps, Reviewed: extended.ReviewApproved,
		Normal:         checkinAdminRandomDTO{Enabled: extended.Normal.Enabled, MinBps: extended.Normal.MinBps, MaxBps: extended.Normal.MaxBps},
		Super:          checkinAdminSuperDTO{Enabled: extended.Super.Enabled, MinBps: extended.Super.MinBps, MaxBps: extended.Super.MaxBps, Cost: strconv.FormatFloat(extended.Super.Cost, 'f', 8, 64)},
		PendingRefresh: extended.PendingRefresh, NextResetAt: nextResetAt,
	}
}

func (h *CheckinAdminHandler) GetSettings(c *gin.Context) {
	base, extended, err := h.settingService.GetDailyCheckinPolicyV2(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, newCheckinAdminPolicyV2DTO(base, extended))
}

func (h *CheckinAdminHandler) UpdateSettings(c *gin.Context) {
	var dto checkinAdminPolicyV2DTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ErrorFrom(c, service.ErrDailyCheckinPolicyInvalid)
		return
	}
	base, extended, err := dto.toPolicy()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := h.settingService.UpdateDailyCheckinPolicyV2(c.Request.Context(), base, extended, dto.ExpectedVersion); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	updatedBase, updatedExtended, err := h.settingService.GetDailyCheckinPolicyV2(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, newCheckinAdminPolicyV2DTO(updatedBase, updatedExtended))
}
