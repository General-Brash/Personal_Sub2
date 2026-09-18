package service

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"
)

// oidcLoginAttemptGuard is deliberately local, bounded, and fail-closed. It
// protects the Provider's password/TOTP form before a browser session exists.
// A future shared limiter may replace it, but a limiter outage must never turn
// this path into unlimited password attempts.
const (
	oidcLoginGuardThreshold = 5
	oidcLoginGuardWindow    = 5 * time.Minute
	oidcLoginGuardBlock     = 15 * time.Minute
	oidcLoginGuardCapacity  = 4096
)

type oidcLoginAttempt struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
}

type oidcLoginAttemptGuard struct {
	mu      sync.Mutex
	entries map[string]oidcLoginAttempt
}

func newOIDCLoginAttemptGuard() *oidcLoginAttemptGuard {
	return &oidcLoginAttemptGuard{entries: make(map[string]oidcLoginAttempt, oidcLoginGuardCapacity)}
}

func (g *oidcLoginAttemptGuard) key(raw string) string {
	if raw == "" {
		raw = "unknown"
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (g *oidcLoginAttemptGuard) check(raw string, now time.Time) (time.Duration, bool) {
	if g == nil {
		// A nil guard is a programming/configuration failure, not permission to
		// disable brute-force protection.
		return oidcLoginGuardBlock, true
	}
	key := g.key(raw)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cleanupLocked(now)
	entry, ok := g.entries[key]
	if ok && entry.blockedUntil.After(now) {
		return entry.blockedUntil.Sub(now), true
	}
	if !ok && len(g.entries) >= oidcLoginGuardCapacity {
		// Bounded storage is intentionally fail-closed when saturated.
		return oidcLoginGuardBlock, true
	}
	return 0, false
}

func (g *oidcLoginAttemptGuard) failure(raw string, now time.Time) (blocked bool) {
	if g == nil {
		return true
	}
	key := g.key(raw)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cleanupLocked(now)
	entry, ok := g.entries[key]
	if !ok {
		if len(g.entries) >= oidcLoginGuardCapacity {
			return true
		}
		entry = oidcLoginAttempt{windowStart: now}
	}
	if entry.blockedUntil.After(now) {
		g.entries[key] = entry
		return true
	}
	if entry.windowStart.IsZero() || !now.Before(entry.windowStart.Add(oidcLoginGuardWindow)) {
		entry.windowStart = now
		entry.failures = 0
	}
	entry.failures++
	if entry.failures >= oidcLoginGuardThreshold {
		entry.failures = 0
		entry.blockedUntil = now.Add(oidcLoginGuardBlock)
		entry.windowStart = entry.blockedUntil
		blocked = true
	}
	g.entries[key] = entry
	if blocked {
		slog.Warn("oidc provider login attempt guard blocked key", "event", "oidc_login_guard_block", "key_digest", key[:16], "block_seconds", int(oidcLoginGuardBlock/time.Second))
	}
	return blocked
}

func (g *oidcLoginAttemptGuard) success(raw string, now time.Time) {
	if g == nil {
		return
	}
	key := g.key(raw)
	g.mu.Lock()
	defer g.mu.Unlock()
	if entry, ok := g.entries[key]; ok && !entry.blockedUntil.After(now) {
		delete(g.entries, key)
	}
}

func (g *oidcLoginAttemptGuard) cleanupLocked(now time.Time) {
	for key, entry := range g.entries {
		if !entry.blockedUntil.After(now) && !entry.windowStart.IsZero() && !now.Before(entry.windowStart.Add(oidcLoginGuardWindow)) {
			delete(g.entries, key)
		}
	}
}
