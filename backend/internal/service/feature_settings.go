package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const SettingKeyTemporaryCreditSourceExpiry = "temporary_credit_source_expiry"

type PersonalFeatureSettings struct {
	ModelPlazaV2Enabled      bool              `json:"model_plaza_v2_enabled"`
	PlayerInvitationsEnabled bool              `json:"player_invitations_enabled"`
	InvitationTTLSeconds     int64             `json:"invitation_ttl_seconds"`
	SourceExpiry             map[string]string `json:"source_expiry"`
	Version                  string            `json:"version"`
}

func featureSettingKeys() []string {
	return []string{"model_plaza_v2_enabled", SettingKeyPlayerInvitationsEnabled, SettingKeyPlayerInvitationTTLSeconds, SettingKeyTemporaryCreditSourceExpiry}
}
func parsePersonalFeatureSettings(values map[string]string) (PersonalFeatureSettings, error) {
	p := PersonalFeatureSettings{SourceExpiry: map[string]string{"checkin": "00:00", "admin_grant": "00:00"}}
	for _, entry := range []struct {
		key  string
		dest *bool
	}{{"model_plaza_v2_enabled", &p.ModelPlazaV2Enabled}, {SettingKeyPlayerInvitationsEnabled, &p.PlayerInvitationsEnabled}} {
		if raw := values[entry.key]; raw != "" {
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				return p, err
			}
			*entry.dest = parsed
		}
	}
	if raw := values[SettingKeyPlayerInvitationTTLSeconds]; raw != "" {
		seconds, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return p, err
		}
		p.InvitationTTLSeconds = seconds
	}
	if raw := values[SettingKeyTemporaryCreditSourceExpiry]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &p.SourceExpiry); err != nil {
			return p, err
		}
	}
	if err := p.Validate(); err != nil {
		return p, err
	}
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	p.Version = hex.EncodeToString(sum[:16])
	return p, nil
}
func (p PersonalFeatureSettings) Validate() error {
	if p.SourceExpiry == nil || p.SourceExpiry["checkin"] == "" || p.SourceExpiry["admin_grant"] == "" {
		return fmt.Errorf("expiry clocks for checkin and admin_grant are required")
	}
	if p.InvitationTTLSeconds < 0 || p.InvitationTTLSeconds > int64((1<<63-1)/time.Second) || (p.PlayerInvitationsEnabled && p.InvitationTTLSeconds == 0) {
		return fmt.Errorf("an explicit positive invitation expiry is required before enabling")
	}
	for source, clock := range p.SourceExpiry {
		if source != "checkin" && source != "admin_grant" {
			return fmt.Errorf("unsupported expiry source %q: bank, mall, and subscriptions retain independent contracts", source)
		}
		if _, err := parseCheckinRefreshTime(clock); err != nil {
			return fmt.Errorf("invalid source expiry clock")
		}
	}
	return nil
}
func (s *SettingService) GetPersonalFeatureSettings(ctx context.Context) (PersonalFeatureSettings, error) {
	values, err := s.settingRepo.GetMultiple(ctx, featureSettingKeys())
	if err != nil {
		return PersonalFeatureSettings{}, err
	}
	return parsePersonalFeatureSettings(values)
}
func (s *SettingService) UpdatePersonalFeatureSettings(ctx context.Context, p PersonalFeatureSettings) (PersonalFeatureSettings, error) {
	if err := p.Validate(); err != nil {
		return p, err
	}
	values, err := s.settingRepo.GetMultiple(ctx, featureSettingKeys())
	if err != nil {
		return p, err
	}
	current, err := parsePersonalFeatureSettings(values)
	if err != nil {
		return p, err
	}
	if p.Version == "" || current.Version != p.Version {
		return p, ErrCheckinPolicyVersionStale
	}
	for _, key := range featureSettingKeys() {
		if _, ok := values[key]; !ok {
			values[key] = ""
		}
	}
	raw, _ := json.Marshal(p.SourceExpiry)
	updates := map[string]string{"model_plaza_v2_enabled": strconv.FormatBool(p.ModelPlazaV2Enabled), SettingKeyPlayerInvitationsEnabled: strconv.FormatBool(p.PlayerInvitationsEnabled), SettingKeyPlayerInvitationTTLSeconds: strconv.FormatInt(p.InvitationTTLSeconds, 10), SettingKeyTemporaryCreditSourceExpiry: string(raw)}
	repo, ok := s.settingRepo.(AtomicSettingPolicyRepository)
	if !ok {
		return p, ErrServiceUnavailable
	}
	applied, err := repo.CompareAndSetMultiple(ctx, values, updates)
	if err != nil {
		return p, err
	}
	if !applied {
		return p, ErrCheckinPolicyVersionStale
	}
	return parsePersonalFeatureSettings(updates)
}
func (s *SettingService) TemporaryCreditSourceExpiry(ctx context.Context, source TemporaryCreditSource, at time.Time) (time.Time, string, error) {
	settings, err := s.GetPersonalFeatureSettings(ctx)
	if err != nil {
		return time.Time{}, "", err
	}
	clock, ok := settings.SourceExpiry[string(source)]
	if !ok {
		return time.Time{}, "", nil
	}
	minute, err := parseCheckinRefreshTime(clock)
	if err != nil {
		return time.Time{}, "", err
	}
	local := at.In(beijingLocation)
	end := time.Date(local.Year(), local.Month(), local.Day(), minute/60, minute%60, 0, 0, beijingLocation)
	if !end.After(local) {
		end = end.AddDate(0, 0, 1)
	}
	return end, settings.Version, nil
}
