package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	SettingKeyServerAutoRetryConfig = "server_auto_retry_config"
	serverAutoRetryCacheTTL         = 60 * time.Second
	serverAutoRetryErrorTTL         = 5 * time.Second
)

type ServerAutoRetryScope struct {
	Platform   string   `json:"platform"`
	GroupIDs   []int64  `json:"group_ids"`
	AccountIDs []int64  `json:"account_ids"`
	Models     []string `json:"models"`
}

type ServerAutoRetryRule struct {
	Enabled    bool `json:"enabled"`
	MaxRetries int  `json:"max_retries"`
}

type ServerAutoRetryConfig struct {
	SchemaVersion          int                  `json:"schema_version"`
	Enabled                bool                 `json:"enabled"`
	Scope                  ServerAutoRetryScope `json:"scope"`
	MaxAdditionalAttempts  int                  `json:"max_additional_attempts"`
	MaxTotalRetryWaitMS    int                  `json:"max_total_retry_wait_ms"`
	Azure400               ServerAutoRetryRule  `json:"azure400"`
	EmptyResponse          ServerAutoRetryRule  `json:"empty_response"`
	EmptyRetryStrategy     string               `json:"empty_retry_strategy"`
	SameAccountRetries     int                  `json:"same_account_retries"`
	RateLimitRetryStrategy string               `json:"rate_limit_retry_strategy"`
	EmptyBackoffBaseMS     int                  `json:"empty_backoff_base_ms"`
	RateLimitBackoffBaseMS int                  `json:"rate_limit_backoff_base_ms"`
	BackoffCapMS           int                  `json:"backoff_cap_ms"`
	JitterMS               int                  `json:"jitter_ms"`
	RespectRetryAfter      bool                 `json:"respect_retry_after"`
}

type cachedServerAutoRetryConfig struct {
	config    ServerAutoRetryConfig
	expiresAt time.Time
}

func DefaultServerAutoRetryConfig() ServerAutoRetryConfig {
	return ServerAutoRetryConfig{
		SchemaVersion: 1, Enabled: true,
		Scope:                 ServerAutoRetryScope{Platform: "openai", GroupIDs: []int64{}, AccountIDs: []int64{}, Models: []string{}},
		MaxAdditionalAttempts: 3, MaxTotalRetryWaitMS: 30000,
		Azure400:           ServerAutoRetryRule{Enabled: true, MaxRetries: 1},
		EmptyResponse:      ServerAutoRetryRule{Enabled: true, MaxRetries: 2},
		EmptyRetryStrategy: "same_then_switch", SameAccountRetries: 1,
		RateLimitRetryStrategy: "prefer_other_account",
		EmptyBackoffBaseMS:     500, RateLimitBackoffBaseMS: 2000,
		BackoffCapMS: 10000, JitterMS: 250, RespectRetryAfter: true,
	}
}

func ValidateServerAutoRetryConfig(cfg ServerAutoRetryConfig) error {
	invalid := func(message string) error {
		return infraerrors.BadRequest("INVALID_SERVER_AUTO_RETRY_CONFIG", message)
	}
	if cfg.SchemaVersion != 1 {
		return invalid("schema_version must be 1")
	}
	if cfg.Scope.Platform != "openai" {
		return invalid("scope.platform must be openai")
	}
	for _, ids := range [][]int64{cfg.Scope.GroupIDs, cfg.Scope.AccountIDs} {
		for _, id := range ids {
			if id <= 0 {
				return invalid("scope group_ids and account_ids must contain positive IDs")
			}
		}
	}
	for _, model := range cfg.Scope.Models {
		if strings.TrimSpace(model) == "" || model != strings.TrimSpace(model) {
			return invalid("scope.models must contain nonempty model names without surrounding whitespace")
		}
	}
	if cfg.MaxAdditionalAttempts < 0 || cfg.MaxAdditionalAttempts > 5 {
		return invalid("max_additional_attempts must be between 0 and 5")
	}
	if cfg.Azure400.MaxRetries < 0 || cfg.Azure400.MaxRetries > 1 || cfg.Azure400.MaxRetries > cfg.MaxAdditionalAttempts {
		return invalid("azure400.max_retries must be between 0 and 1 and not exceed max_additional_attempts")
	}
	if cfg.EmptyResponse.MaxRetries < 0 || cfg.EmptyResponse.MaxRetries > 5 || cfg.EmptyResponse.MaxRetries > cfg.MaxAdditionalAttempts {
		return invalid("empty_response.max_retries must be between 0 and 5 and not exceed max_additional_attempts")
	}
	if cfg.SameAccountRetries < 0 || cfg.SameAccountRetries > cfg.EmptyResponse.MaxRetries {
		return invalid("same_account_retries must be between 0 and empty_response.max_retries")
	}
	switch cfg.EmptyRetryStrategy {
	case "same_account_only", "same_then_switch", "prefer_other_account":
	default:
		return invalid("invalid empty_retry_strategy")
	}
	switch cfg.RateLimitRetryStrategy {
	case "same_account_only", "prefer_other_account":
	default:
		return invalid("invalid rate_limit_retry_strategy")
	}
	for name, value := range map[string]int{
		"max_total_retry_wait_ms":    cfg.MaxTotalRetryWaitMS,
		"empty_backoff_base_ms":      cfg.EmptyBackoffBaseMS,
		"rate_limit_backoff_base_ms": cfg.RateLimitBackoffBaseMS,
		"backoff_cap_ms":             cfg.BackoffCapMS, "jitter_ms": cfg.JitterMS,
	} {
		// Values are converted to time.Duration by the request-level retry engine.
		if value < 0 || int64(value) > int64((1<<63-1)/time.Millisecond) {
			return invalid(name + " must be a nonnegative duration in milliseconds")
		}
	}
	return nil
}

func cloneServerAutoRetryConfig(cfg ServerAutoRetryConfig) ServerAutoRetryConfig {
	cfg.Scope.GroupIDs = append([]int64{}, cfg.Scope.GroupIDs...)
	cfg.Scope.AccountIDs = append([]int64{}, cfg.Scope.AccountIDs...)
	cfg.Scope.Models = append([]string{}, cfg.Scope.Models...)
	return cfg
}

// GetServerAutoRetryConfig waits on storage only for a cold cache; each request owns its slice copies.
func (s *SettingService) GetServerAutoRetryConfig(ctx context.Context) ServerAutoRetryConfig {
	if s == nil || s.settingRepo == nil {
		cfg := DefaultServerAutoRetryConfig()
		cfg.Enabled = false
		return cfg
	}
	cached, _ := s.serverAutoRetryCache.Load().(*cachedServerAutoRetryConfig)
	if cached == nil || !time.Now().Before(cached.expiresAt) {
		refresh := s.serverAutoRetrySF.DoChan(SettingKeyServerAutoRetryConfig, func() (any, error) {
			s.serverAutoRetryMu.Lock()
			defer s.serverAutoRetryMu.Unlock()
			if current, _ := s.serverAutoRetryCache.Load().(*cachedServerAutoRetryConfig); current != nil && time.Now().Before(current.expiresAt) {
				return nil, nil
			}
			_, err := s.loadServerAutoRetryConfigLocked(context.Background())
			return nil, err
		})
		if cached == nil {
			if ctx == nil {
				ctx = context.Background()
			}
			waitCtx, cancel := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
			defer cancel()
			select {
			case <-refresh:
				cached, _ = s.serverAutoRetryCache.Load().(*cachedServerAutoRetryConfig)
			case <-waitCtx.Done():
				cfg := DefaultServerAutoRetryConfig()
				cfg.Enabled = false
				return cfg
			}
		}
	}
	if cached == nil {
		cfg := DefaultServerAutoRetryConfig()
		cfg.Enabled = false
		return cfg
	}
	return cloneServerAutoRetryConfig(cached.config)
}

// LoadServerAutoRetryConfig synchronously reads storage for administrator GETs.
func (s *SettingService) LoadServerAutoRetryConfig(ctx context.Context) (ServerAutoRetryConfig, error) {
	if s == nil || s.settingRepo == nil {
		cfg := DefaultServerAutoRetryConfig()
		cfg.Enabled = false
		return cfg, infraerrors.ServiceUnavailable("SERVER_AUTO_RETRY_SETTINGS_UNAVAILABLE", "Settings repository not available")
	}
	s.serverAutoRetryMu.Lock()
	defer s.serverAutoRetryMu.Unlock()
	return s.loadServerAutoRetryConfigLocked(ctx)
}

// The same lock covers read+publish and save+publish, so an older refresh cannot
// replace a successful concurrent save with a stale snapshot.
func (s *SettingService) loadServerAutoRetryConfigLocked(ctx context.Context) (ServerAutoRetryConfig, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
	defer cancel()
	cfg := DefaultServerAutoRetryConfig()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyServerAutoRetryConfig)
	if errors.Is(err, ErrSettingNotFound) {
		err = nil
	} else if err == nil && strings.TrimSpace(raw) != "" {
		cfg = ServerAutoRetryConfig{}
		err = json.Unmarshal([]byte(raw), &cfg)
		if err == nil {
			err = ValidateServerAutoRetryConfig(cfg)
		}
	}
	if err != nil {
		if prior, _ := s.serverAutoRetryCache.Load().(*cachedServerAutoRetryConfig); prior != nil {
			cfg = prior.config
		} else {
			cfg = DefaultServerAutoRetryConfig()
			cfg.Enabled = false
		}
		s.serverAutoRetryCache.Store(&cachedServerAutoRetryConfig{config: cloneServerAutoRetryConfig(cfg), expiresAt: time.Now().Add(serverAutoRetryErrorTTL)})
		return cloneServerAutoRetryConfig(cfg), fmt.Errorf("load server auto retry config: %w", err)
	}
	cfg = cloneServerAutoRetryConfig(cfg)
	s.serverAutoRetryCache.Store(&cachedServerAutoRetryConfig{config: cfg, expiresAt: time.Now().Add(serverAutoRetryCacheTTL)})
	return cloneServerAutoRetryConfig(cfg), nil
}

func (s *SettingService) UpdateServerAutoRetryConfig(ctx context.Context, cfg ServerAutoRetryConfig) (ServerAutoRetryConfig, error) {
	if err := ValidateServerAutoRetryConfig(cfg); err != nil {
		return ServerAutoRetryConfig{}, err
	}
	if s == nil || s.settingRepo == nil {
		return ServerAutoRetryConfig{}, infraerrors.ServiceUnavailable("SERVER_AUTO_RETRY_SETTINGS_UNAVAILABLE", "Settings repository not available")
	}
	cfg = cloneServerAutoRetryConfig(cfg)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return ServerAutoRetryConfig{}, fmt.Errorf("encode server auto retry config: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.serverAutoRetryMu.Lock()
	defer s.serverAutoRetryMu.Unlock()
	if err := s.settingRepo.Set(ctx, SettingKeyServerAutoRetryConfig, string(raw)); err != nil {
		return ServerAutoRetryConfig{}, fmt.Errorf("save server auto retry config: %w", err)
	}
	s.serverAutoRetryCache.Store(&cachedServerAutoRetryConfig{config: cfg, expiresAt: time.Now().Add(serverAutoRetryCacheTTL)})
	return cloneServerAutoRetryConfig(cfg), nil
}
