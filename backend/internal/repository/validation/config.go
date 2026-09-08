package validation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	ModeDedicated    = "dedicated"
	ModeCIContainer  = "ci-container"
	ModeEnv          = "SUB2API_VALIDATION_MODE"
	ConfigEnv        = "SUB2API_VALIDATION_CONFIG"
	LegacyAllowEnv   = "SUB2API_ALLOW_LEGACY_CI_CONTAINERS"
	DBExecutionEnv   = "SUB2API_ALLOW_DB_EXECUTION"
	DBExecutionValue = "ALLOW"
)

type Config struct {
	Mode             string            `json:"mode"`
	TargetID         string            `json:"target_id"`
	Candidate        Candidate         `json:"candidate"`
	Database         Endpoint          `json:"database"`
	Redis            RedisEndpoint     `json:"redis"`
	AllowedTargets   []AllowedTarget   `json:"allowed_targets"`
	FixturePolicy    FixturePolicy     `json:"fixture_policy"`
	AllowAutoMigrate bool              `json:"allow_auto_migrate"`
	Application      ApplicationTarget `json:"application"`
}

// ApplicationTarget binds E2E traffic to the target approved for this candidate.
// Fixture URLs are declarations checked against the approved allowlist, not proof
// that a running application is isolated; B0 must also verify its actual config.
type ApplicationTarget struct {
	TargetID               string            `json:"target_id"`
	BaseURL                string            `json:"base_url"`
	ExternalMode           string            `json:"external_mode"`
	BackgroundJobsDisabled bool              `json:"background_jobs_disabled"`
	FixtureURLs            map[string]string `json:"fixture_urls"`
}

type Candidate struct {
	ManifestPath   string `json:"manifest_path"`
	ManifestSHA256 string `json:"manifest_sha256"`
	CandidateID    string `json:"candidate_id"`
	GCodeStatus    string `json:"g_code_status"`
}

type CandidateManifest struct {
	CandidateID string `json:"candidate_id"`
	GCodeStatus string `json:"g_code_status"`
}

type Endpoint struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	DSNEnv   string `json:"dsn_env"`
}

type RedisEndpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	DB   int    `json:"db"`
}

type AllowedTarget struct {
	TargetID      string            `json:"target_id"`
	DBHost        string            `json:"db_host"`
	DBPort        int               `json:"db_port"`
	Database      string            `json:"database"`
	DBUser        string            `json:"db_user"`
	RedisHost     string            `json:"redis_host"`
	RedisPort     int               `json:"redis_port"`
	RedisDB       int               `json:"redis_db"`
	AllowReset    bool              `json:"allow_reset"`
	ApprovedClone string            `json:"approved_clone"`
	AppBaseURL    string            `json:"app_base_url"`
	FixtureURLs   map[string]string `json:"fixture_urls"`
}

type FixturePolicy struct {
	AllowReset    bool   `json:"allow_reset"`
	ApprovedClone string `json:"approved_clone"`
}

func LoadFromEnv(environ func(string) string, readFile func(string) ([]byte, error)) (Config, error) {
	mode := strings.TrimSpace(environ(ModeEnv))
	if mode == "" {
		return Config{}, errors.New(ModeEnv + " is required")
	}
	if mode == ModeCIContainer {
		if strings.TrimSpace(environ("CI")) != "true" || strings.TrimSpace(environ(LegacyAllowEnv)) != DBExecutionValue {
			return Config{}, errors.New("ci-container mode requires CI=true and explicit " + LegacyAllowEnv + "=" + DBExecutionValue)
		}
		return Config{Mode: mode}, nil
	}
	if mode != ModeDedicated {
		return Config{}, fmt.Errorf("unsupported validation mode %q", mode)
	}

	configPath := strings.TrimSpace(environ(ConfigEnv))
	if configPath == "" {
		return Config{}, errors.New(ConfigEnv + " is required in dedicated mode")
	}
	data, err := readFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("read validation config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode validation config: %w", err)
	}
	if cfg.Mode != mode {
		return Config{}, fmt.Errorf("config mode %q does not match %q", cfg.Mode, mode)
	}
	if err := ValidateDedicated(cfg, environ, readFile); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func LoadFromProcess() (Config, error) {
	return LoadFromEnv(os.Getenv, os.ReadFile)
}

func ValidateDedicated(cfg Config, environ func(string) string, readFile func(string) ([]byte, error)) error {
	if cfg.TargetID == "" {
		return errors.New("dedicated target_id is required")
	}
	if cfg.Candidate.ManifestPath == "" || cfg.Candidate.ManifestSHA256 == "" || cfg.Candidate.CandidateID == "" || cfg.Candidate.GCodeStatus != "PASS" {
		return errors.New("dedicated candidate G_CODE manifest, hash, candidate_id and PASS status are required")
	}
	manifest, err := readFile(cfg.Candidate.ManifestPath)
	if err != nil {
		return fmt.Errorf("read candidate manifest: %w", err)
	}
	sum := sha256.Sum256(manifest)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), cfg.Candidate.ManifestSHA256) {
		return errors.New("candidate manifest sha256 mismatch")
	}
	var candidate CandidateManifest
	if err := json.Unmarshal(manifest, &candidate); err != nil {
		return fmt.Errorf("decode candidate manifest: %w", err)
	}
	if candidate.CandidateID != cfg.Candidate.CandidateID || candidate.GCodeStatus != "PASS" {
		return errors.New("candidate manifest is not the requested PASS G_CODE candidate")
	}
	if cfg.Database.Host == "" || cfg.Database.Port <= 0 || cfg.Database.Database == "" || cfg.Database.User == "" || cfg.Database.DSNEnv == "" || strings.TrimSpace(environ(cfg.Database.DSNEnv)) == "" {
		return errors.New("dedicated database host/port/database/user and non-empty DSN env are required")
	}
	if err := validateDatabaseDSN(environ(cfg.Database.DSNEnv), cfg.Database); err != nil {
		return err
	}
	if cfg.Redis.Host == "" || cfg.Redis.Port <= 0 || cfg.Redis.DB < 0 {
		return errors.New("dedicated redis host/port/db are required")
	}
	var allowed *AllowedTarget
	for i := range cfg.AllowedTargets {
		if cfg.AllowedTargets[i].TargetID == cfg.TargetID {
			allowed = &cfg.AllowedTargets[i]
			break
		}
	}
	if allowed == nil {
		return errors.New("dedicated target is not in allowed_targets")
	}
	if allowed.DBHost != cfg.Database.Host || allowed.DBPort != cfg.Database.Port || allowed.Database != cfg.Database.Database || allowed.DBUser != cfg.Database.User || allowed.RedisHost != cfg.Redis.Host || allowed.RedisPort != cfg.Redis.Port || allowed.RedisDB != cfg.Redis.DB {
		return errors.New("dedicated DB/Redis target does not exactly match allowed_targets")
	}
	if !cfg.FixturePolicy.AllowReset || !allowed.AllowReset || cfg.FixturePolicy.ApprovedClone == "" || cfg.FixturePolicy.ApprovedClone != allowed.ApprovedClone {
		return errors.New("destructive fixtures require allow_reset=true and exact approved_clone")
	}
	if !cfg.AllowAutoMigrate {
		return errors.New("dedicated mode must explicitly allow automatic migration")
	}
	return nil
}

func validateDatabaseDSN(raw string, expected Endpoint) error {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return errors.New("dedicated database DSN is not a valid URL")
		}
		port := 5432
		if u.Port() != "" {
			port, err = strconv.Atoi(u.Port())
			if err != nil {
				return errors.New("dedicated database DSN port is invalid")
			}
		}
		user := ""
		if u.User != nil {
			user = u.User.Username()
		}
		if user != expected.User || u.Hostname() != expected.Host || port != expected.Port || strings.TrimPrefix(u.Path, "/") != expected.Database {
			return errors.New("dedicated database DSN host/port/database/user does not match configured target")
		}
		return nil
	}
	fields := map[string]string{}
	for _, token := range strings.Fields(raw) {
		parts := strings.SplitN(token, "=", 2)
		if len(parts) == 2 {
			fields[parts[0]] = strings.Trim(parts[1], "'\"")
		}
	}
	port := 5432
	if fields["port"] != "" {
		var err error
		port, err = strconv.Atoi(fields["port"])
		if err != nil {
			return errors.New("dedicated database DSN port is invalid")
		}
	}
	if fields["host"] != expected.Host || port != expected.Port || fields["dbname"] != expected.Database || fields["user"] != expected.User {
		return errors.New("dedicated database DSN host/port/database/user does not match configured target")
	}
	return nil
}

func RequireFixtureReset(cfg Config) error {
	if cfg.Mode != ModeDedicated {
		return nil
	}
	if !cfg.FixturePolicy.AllowReset || cfg.FixturePolicy.ApprovedClone == "" {
		return errors.New("fixture reset requires allow_reset=true and approved_clone")
	}
	return nil
}

func ResolveManifestPath(baseDir, configured string) string {
	if filepath.IsAbs(configured) {
		return configured
	}
	return filepath.Join(baseDir, configured)
}

// LoadDedicatedIntegrationConfig is the shared gate for tests that can reach a
// real PostgreSQL/Redis/application endpoint. It deliberately rejects legacy
// container mode and requires an explicit execution opt-in.
func LoadDedicatedIntegrationConfig() (Config, error) {
	cfg, err := LoadFromProcess()
	if err != nil {
		return Config{}, err
	}
	if err := RequireDBExecution(cfg, os.Getenv); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func RequireDBExecution(cfg Config, environ func(string) string) error {
	if cfg.Mode != ModeDedicated {
		return errors.New("real integration tests require dedicated validation mode")
	}
	if strings.TrimSpace(environ(DBExecutionEnv)) != DBExecutionValue {
		return errors.New("real integration tests require explicit " + DBExecutionEnv + "=" + DBExecutionValue)
	}
	return nil
}

func DatabaseDSN(cfg Config) string {
	return strings.TrimSpace(os.Getenv(cfg.Database.DSNEnv))
}

func RedisAddress(cfg Config) string {
	return net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port))
}

func RequireAppBaseURL(cfg Config, environ func(string) string) (string, error) {
	if cfg.Mode != ModeDedicated || cfg.Application.TargetID == "" || cfg.Application.TargetID != cfg.TargetID {
		return "", errors.New("application target_id must exactly match the dedicated target")
	}
	raw, err := canonicalAppURL(environ("BASE_URL"))
	if err != nil {
		return "", err
	}
	configured, err := canonicalAppURL(cfg.Application.BaseURL)
	if err != nil || configured != raw {
		return "", errors.New("BASE_URL does not match configured application target")
	}
	var allowed *AllowedTarget
	for i := range cfg.AllowedTargets {
		if cfg.AllowedTargets[i].TargetID == cfg.TargetID {
			if allowed != nil {
				return "", errors.New("duplicate approved application target")
			}
			allowed = &cfg.AllowedTargets[i]
		}
	}
	if allowed == nil {
		return "", errors.New("application target is not approved")
	}
	approved, err := canonicalAppURL(allowed.AppBaseURL)
	if err != nil || approved != raw {
		return "", errors.New("application URL does not match approved target")
	}
	if cfg.Application.ExternalMode != "controlled-fixtures" || !cfg.Application.BackgroundJobsDisabled {
		return "", errors.New("application E2E requires controlled fixtures and disabled background jobs")
	}
	for _, kind := range []string{"upstream", "payment", "mail", "callback"} {
		fixture, err := canonicalAppURL(cfg.Application.FixtureURLs[kind])
		approvedFixture, approvedErr := canonicalAppURL(allowed.FixtureURLs[kind])
		if err != nil || approvedErr != nil || fixture != approvedFixture || fixture == raw {
			return "", fmt.Errorf("%s fixture endpoint is missing or does not match approved target", kind)
		}
	}
	return raw, nil
}

func canonicalAppURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("application/fixture URL must be explicit absolute http(s) without credentials, query or fragment")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", errors.New("application/fixture URL port is invalid")
		}
	}
	return strings.TrimRight(raw, "/"), nil
}
