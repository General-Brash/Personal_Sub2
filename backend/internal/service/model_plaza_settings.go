package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

const (
	// SettingKeyModelPlazaOverrides 存管理员对广场逐模型的展示覆盖（JSON map）。
	SettingKeyModelPlazaOverrides = "model_plaza_overrides"
	// SettingKeyModelPlazaHideNoAccount 收敛开关：隐藏"无任何账号支持"的幽灵模型。
	SettingKeyModelPlazaHideNoAccount = "model_plaza_hide_no_account"
)

// ModelPlazaModelOverride 管理员对单个模型在广场的展示覆盖。
type ModelPlazaModelOverride struct {
	Hidden    bool `json:"hidden"`
	Pinned    bool `json:"pinned"`
	SortOrder int  `json:"sort_order"`
}

// ModelPlazaAdminSettings 模型广场展示管理设置。
type ModelPlazaAdminSettings struct {
	// Overrides 以 ModelPlazaOverrideKey(platform, model) 为键。
	Overrides     map[string]ModelPlazaModelOverride `json:"overrides"`
	HideNoAccount bool                               `json:"hide_no_account"`
	Version       string                             `json:"version"`
}

// ModelPlazaOverrideKey 归一化 overrides 的键：小写 "platform:model_id"。
func ModelPlazaOverrideKey(platform, modelID string) string {
	return strings.ToLower(strings.TrimSpace(platform)) + ":" + strings.ToLower(strings.TrimSpace(modelID))
}

func modelPlazaSettingKeys() []string {
	return []string{SettingKeyModelPlazaOverrides, SettingKeyModelPlazaHideNoAccount}
}

func parseModelPlazaAdminSettings(values map[string]string) (ModelPlazaAdminSettings, error) {
	// HideNoAccount 默认开启：广场默认收敛掉"无任何账号支持"的幽灵模型，
	// 只有显式 "false" 才关闭、回退旧行为（展示全部播种）。
	s := ModelPlazaAdminSettings{Overrides: map[string]ModelPlazaModelOverride{}, HideNoAccount: true}
	if raw := strings.TrimSpace(values[SettingKeyModelPlazaHideNoAccount]); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return s, err
		}
		s.HideNoAccount = parsed
	}
	if raw := strings.TrimSpace(values[SettingKeyModelPlazaOverrides]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &s.Overrides); err != nil {
			return s, err
		}
		if s.Overrides == nil {
			s.Overrides = map[string]ModelPlazaModelOverride{}
		}
	}
	s.Version = s.computeVersion()
	return s, nil
}

func (s ModelPlazaAdminSettings) computeVersion() string {
	// json.Marshal 对 map 键有序输出，故版本仅由内容决定、稳定可比。
	payload := struct {
		Overrides     map[string]ModelPlazaModelOverride `json:"overrides"`
		HideNoAccount bool                               `json:"hide_no_account"`
	}{Overrides: s.Overrides, HideNoAccount: s.HideNoAccount}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

// GetModelPlazaAdminSettings 读取广场展示管理设置。
func (s *SettingService) GetModelPlazaAdminSettings(ctx context.Context) (ModelPlazaAdminSettings, error) {
	values, err := s.settingRepo.GetMultiple(ctx, modelPlazaSettingKeys())
	if err != nil {
		return ModelPlazaAdminSettings{}, err
	}
	return parseModelPlazaAdminSettings(values)
}

// UpdateModelPlazaAdminSettings 以版本乐观锁写入广场展示管理设置。
func (s *SettingService) UpdateModelPlazaAdminSettings(ctx context.Context, next ModelPlazaAdminSettings) (ModelPlazaAdminSettings, error) {
	if next.Overrides == nil {
		next.Overrides = map[string]ModelPlazaModelOverride{}
	}
	values, err := s.settingRepo.GetMultiple(ctx, modelPlazaSettingKeys())
	if err != nil {
		return next, err
	}
	current, err := parseModelPlazaAdminSettings(values)
	if err != nil {
		return next, err
	}
	if next.Version == "" || current.Version != next.Version {
		return next, ErrCheckinPolicyVersionStale
	}
	for _, key := range modelPlazaSettingKeys() {
		if _, ok := values[key]; !ok {
			values[key] = ""
		}
	}
	overridesRaw, _ := json.Marshal(next.Overrides)
	updates := map[string]string{
		SettingKeyModelPlazaOverrides:     string(overridesRaw),
		SettingKeyModelPlazaHideNoAccount: strconv.FormatBool(next.HideNoAccount),
	}
	repo, ok := s.settingRepo.(AtomicSettingPolicyRepository)
	if !ok {
		return next, ErrServiceUnavailable
	}
	applied, err := repo.CompareAndSetMultiple(ctx, values, updates)
	if err != nil {
		return next, err
	}
	if !applied {
		return next, ErrCheckinPolicyVersionStale
	}
	return parseModelPlazaAdminSettings(updates)
}
