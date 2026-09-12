package service

import (
	"context"
	"testing"
	"time"
)

func TestPersonalFeatureDefaultsAndRequiredActivationContract(t *testing.T) {
	p, err := parsePersonalFeatureSettings(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if p.ModelPlazaV2Enabled || p.PlayerInvitationsEnabled || p.SourceExpiry["checkin"] != "00:00" {
		t.Fatal("new features must default off and preserve expiry")
	}
	p.PlayerInvitationsEnabled = true
	if p.Validate() == nil {
		t.Fatal("invitation expiry cannot be invented")
	}
	p.InvitationTTLSeconds = 3600
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p.SourceExpiry["bank_advance"] = "06:00"
	if p.Validate() == nil {
		t.Fatal("new source policy must not change bank debt contracts")
	}
}

type sourceExpiryStub struct {
	calls int
	at    time.Time
}

func (s *sourceExpiryStub) TemporaryCreditSourceExpiry(context.Context, TemporaryCreditSource, time.Time) (time.Time, string, error) {
	s.calls++
	return s.at, "policy-1", nil
}
func TestTemporaryCreditSourceExpiryDoesNotOverrideExplicitOrPurchasedPeriods(t *testing.T) {
	now := time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC)
	provider := &sourceExpiryStub{at: now.Add(7 * time.Hour)}
	svc := NewTemporaryCreditServiceWithClock(nil, func() time.Time { return now })
	svc.SetSourceExpiryPolicy(provider)
	admin := int64(2)
	grant, err := svc.newGrantWithPolicy(context.Background(), CreateTemporaryCreditGrantInput{UserID: 1, Source: TemporaryCreditSourceAdminGrant, Amount: 1, GrantedBy: &admin, Notes: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !grant.ExpiresAt().Equal(provider.at) || grant.ExpiryPolicyVersion != "policy-1" {
		t.Fatal("source policy not frozen")
	}
	explicit := now.Add(2 * time.Hour)
	grant, err = svc.newGrantWithPolicy(context.Background(), CreateTemporaryCreditGrantInput{UserID: 1, Source: TemporaryCreditSourceAdminGrant, Amount: 1, GrantedBy: &admin, Notes: "test", expiresAt: &explicit})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || !grant.ExpiresAt().Equal(explicit) {
		t.Fatal("explicit paid period changed")
	}
}
