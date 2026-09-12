package service

import (
	"testing"
)

func TestPlayerInvitationRetryTokenIsStablePrivateAndUserScoped(t *testing.T) {
	s := &PlayerInvitationService{}
	s.SetSigningKey("test-only-secret-long-enough")
	first, hash, err := s.reservationToken(1, "00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	again, againHash, err := s.reservationToken(1, "00000000-0000-4000-8000-000000000001")
	if err != nil || again != first || hash != againHash {
		t.Fatal("retry changed credential")
	}
	other, _, _ := s.reservationToken(2, "00000000-0000-4000-8000-000000000001")
	if other == first {
		t.Fatal("credential shared across users")
	}
	s.SetSigningKey("another-test-only-secret-long-enough")
	rotated, stableHash, _ := s.reservationToken(1, "00000000-0000-4000-8000-000000000001")
	if rotated == first || stableHash != hash {
		t.Fatal("rotation must preserve dedup identity but change private credential")
	}
}
