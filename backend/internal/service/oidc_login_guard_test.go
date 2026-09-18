package service

import (
	"testing"
	"time"
)

func TestOIDCLoginAttemptGuardBlocksAfterBoundedFailures(t *testing.T) {
	guard := newOIDCLoginAttemptGuard()
	now := time.Unix(100, 0)
	for i := 0; i < oidcLoginGuardThreshold-1; i++ {
		if guard.failure("198.51.100.10|user@example.com", now) {
			t.Fatalf("attempt %d unexpectedly blocked", i+1)
		}
	}
	if retry, blocked := guard.check("198.51.100.10|user@example.com", now); blocked || retry != 0 {
		t.Fatalf("key blocked before threshold: retry=%s blocked=%v", retry, blocked)
	}
	if !guard.failure("198.51.100.10|user@example.com", now) {
		t.Fatal("threshold failure must block")
	}
	if retry, blocked := guard.check("198.51.100.10|user@example.com", now); !blocked || retry <= 0 {
		t.Fatalf("blocked key not rejected: retry=%s blocked=%v", retry, blocked)
	}
}

func TestOIDCLoginAttemptGuardSuccessClearsFailures(t *testing.T) {
	guard := newOIDCLoginAttemptGuard()
	now := time.Unix(200, 0)
	guard.failure("203.0.113.10|user@example.com", now)
	guard.success("203.0.113.10|user@example.com", now)
	if retry, blocked := guard.check("203.0.113.10|user@example.com", now); blocked || retry != 0 {
		t.Fatalf("successful login did not clear guard state: retry=%s blocked=%v", retry, blocked)
	}
}

func TestOIDCLoginAttemptGuardNilFailsClosed(t *testing.T) {
	var guard *oidcLoginAttemptGuard
	if retry, blocked := guard.check("", time.Unix(300, 0)); !blocked || retry <= 0 {
		t.Fatalf("nil guard must fail closed: retry=%s blocked=%v", retry, blocked)
	}
}
