package validation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromEnvDedicatedValidatesCandidateTargetAndResetPolicy(t *testing.T) {
	dir := t.TempDir()
	manifest := []byte(`{"candidate_id":"C1","g_code_status":"PASS"}`)
	manifestPath := filepath.Join(dir, "candidate.json")
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(manifest)
	cfg := Config{
		Mode: ModeDedicated, TargetID: "clone-1",
		Candidate:      Candidate{ManifestPath: manifestPath, ManifestSHA256: hex.EncodeToString(sum[:]), CandidateID: "C1", GCodeStatus: "PASS"},
		Database:       Endpoint{Host: "db.test", Port: 5432, Database: "sub2api_validation", User: "validator", DSNEnv: "TEST_DSN"},
		Redis:          RedisEndpoint{Host: "redis.test", Port: 6379, DB: 3},
		AllowedTargets: []AllowedTarget{{TargetID: "clone-1", DBHost: "db.test", DBPort: 5432, Database: "sub2api_validation", DBUser: "validator", RedisHost: "redis.test", RedisPort: 6379, RedisDB: 3, AllowReset: true, ApprovedClone: "clone-1"}},
		FixturePolicy:  FixturePolicy{AllowReset: true, ApprovedClone: "clone-1"}, AllowAutoMigrate: true,
	}
	data, _ := json.Marshal(cfg)
	configPath := filepath.Join(dir, "validation.json")
	_ = os.WriteFile(configPath, data, 0o600)
	env := map[string]string{ModeEnv: ModeDedicated, ConfigEnv: configPath, "TEST_DSN": "postgres://validator@db.test:5432/sub2api_validation"}
	got, err := LoadFromEnv(envLookup(env), os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetID != "clone-1" {
		t.Fatalf("unexpected target %q", got.TargetID)
	}
}

func TestLoadFromEnvDedicatedRejectsDSNTargetMismatch(t *testing.T) {
	dir := t.TempDir()
	manifest := []byte(`{"candidate_id":"C1","g_code_status":"PASS"}`)
	manifestPath := filepath.Join(dir, "candidate.json")
	_ = os.WriteFile(manifestPath, manifest, 0o600)
	sum := sha256.Sum256(manifest)
	cfg := Config{Mode: ModeDedicated, TargetID: "clone-1", Candidate: Candidate{ManifestPath: manifestPath, ManifestSHA256: hex.EncodeToString(sum[:]), CandidateID: "C1", GCodeStatus: "PASS"}, Database: Endpoint{Host: "db.test", Port: 5432, Database: "sub2api_validation", User: "validator", DSNEnv: "TEST_DSN"}, Redis: RedisEndpoint{Host: "redis.test", Port: 6379, DB: 3}, AllowedTargets: []AllowedTarget{{TargetID: "clone-1", DBHost: "db.test", DBPort: 5432, Database: "sub2api_validation", DBUser: "validator", RedisHost: "redis.test", RedisPort: 6379, RedisDB: 3, AllowReset: true, ApprovedClone: "clone-1"}}, FixturePolicy: FixturePolicy{AllowReset: true, ApprovedClone: "clone-1"}, AllowAutoMigrate: true}
	data, _ := json.Marshal(cfg)
	configPath := filepath.Join(dir, "validation.json")
	_ = os.WriteFile(configPath, data, 0o600)
	env := map[string]string{ModeEnv: ModeDedicated, ConfigEnv: configPath, "TEST_DSN": "postgres://validator@other-host:5432/sub2api_validation"}
	if _, err := LoadFromEnv(envLookup(env), os.ReadFile); err == nil {
		t.Fatal("expected DSN target mismatch")
	}
}

func TestLoadFromEnvDedicatedRejectsTargetMismatchAndMissingCandidate(t *testing.T) {
	env := map[string]string{ModeEnv: ModeDedicated, ConfigEnv: "missing.json"}
	if _, err := LoadFromEnv(envLookup(env), os.ReadFile); err == nil {
		t.Fatal("expected missing config error")
	}
}

func envLookup(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadDedicatedIntegrationConfigRequiresExplicitExecutionOptIn(t *testing.T) {
	dir := t.TempDir()
	manifest := []byte(`{"candidate_id":"C1","g_code_status":"PASS"}`)
	manifestPath := filepath.Join(dir, "candidate.json")
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(manifest)
	cfg := Config{
		Mode: ModeDedicated, TargetID: "clone-1",
		Candidate:      Candidate{ManifestPath: manifestPath, ManifestSHA256: hex.EncodeToString(sum[:]), CandidateID: "C1", GCodeStatus: "PASS"},
		Database:       Endpoint{Host: "db.test", Port: 5432, Database: "sub2api_validation", User: "validator", DSNEnv: "TEST_DSN"},
		Redis:          RedisEndpoint{Host: "redis.test", Port: 6379, DB: 3},
		AllowedTargets: []AllowedTarget{{TargetID: "clone-1", DBHost: "db.test", DBPort: 5432, Database: "sub2api_validation", DBUser: "validator", RedisHost: "redis.test", RedisPort: 6379, RedisDB: 3, AllowReset: true, ApprovedClone: "clone-1"}},
		FixturePolicy:  FixturePolicy{AllowReset: true, ApprovedClone: "clone-1"}, AllowAutoMigrate: true,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "validation.json")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ModeEnv, ModeDedicated)
	t.Setenv(ConfigEnv, configPath)
	t.Setenv("TEST_DSN", "postgres://validator@db.test:5432/sub2api_validation")
	t.Setenv(DBExecutionEnv, "")
	if _, err := LoadDedicatedIntegrationConfig(); err == nil || !strings.Contains(err.Error(), DBExecutionEnv) {
		t.Fatalf("expected explicit execution opt-in rejection, got %v", err)
	}
}

func TestRequireAppBaseURLRejectsMissingRelativeAndNonHTTP(t *testing.T) {
	for _, value := range []string{"", "localhost:8080", "file:///tmp/app"} {
		value := value
		t.Run(value, func(t *testing.T) {
			if _, err := RequireAppBaseURL(approvedAppFixtureConfig(), func(string) string { return value }); err == nil {
				t.Fatalf("expected BASE_URL %q to be rejected", value)
			}
		})
	}
}

func TestRequireAppBaseURLAcceptsExplicitHTTPSTarget(t *testing.T) {
	got, err := RequireAppBaseURL(approvedAppFixtureConfig(), func(string) string { return "https://app.test.example/" })
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://app.test.example" {
		t.Fatalf("unexpected normalized BASE_URL %q", got)
	}
}

func approvedAppFixtureConfig() Config {
	fixtures := map[string]string{"upstream": "http://upstream.fixture.test", "payment": "http://payment.fixture.test", "mail": "http://mail.fixture.test", "callback": "http://callback.fixture.test"}
	approved := map[string]string{}
	for k, v := range fixtures {
		approved[k] = v
	}
	return Config{Mode: ModeDedicated, TargetID: "app-clone", Application: ApplicationTarget{TargetID: "app-clone", BaseURL: "https://app.test.example", ExternalMode: "controlled-fixtures", BackgroundJobsDisabled: true, FixtureURLs: fixtures}, AllowedTargets: []AllowedTarget{{TargetID: "app-clone", AppBaseURL: "https://app.test.example", FixtureURLs: approved}}}
}

func TestRequireAppBaseURLRejectsUnapprovedTargetsAndExternalEffects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
		url    string
	}{
		{"wrong application identity", func(c *Config) { c.Application.TargetID = "other" }, ""},
		{"unapproved application", func(c *Config) { c.AllowedTargets = nil }, ""},
		{"wrong approved URL", func(c *Config) { c.AllowedTargets[0].AppBaseURL = "https://other.test" }, ""},
		{"duplicate approval", func(c *Config) { c.AllowedTargets = append(c.AllowedTargets, c.AllowedTargets[0]) }, ""},
		{"real upstream mode", func(c *Config) { c.Application.ExternalMode = "real" }, ""},
		{"background tasks active", func(c *Config) { c.Application.BackgroundJobsDisabled = false }, ""},
		{"missing payment fixture", func(c *Config) { delete(c.Application.FixtureURLs, "payment") }, ""},
		{"wrong callback fixture", func(c *Config) { c.Application.FixtureURLs["callback"] = "https://unapproved.test" }, ""},
		{"default localhost", nil, "http://localhost:8080"},
		{"URL credentials", nil, "https://user:secret@app.test.example"},
		{"URL query", nil, "https://app.test.example?token=value"},
		{"URL fragment", nil, "https://app.test.example#other"},
		{"invalid port", nil, "https://app.test.example:99999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := approvedAppFixtureConfig()
			if tc.mutate != nil {
				tc.mutate(&cfg)
			}
			raw := tc.url
			if raw == "" {
				raw = "https://app.test.example"
			}
			if _, err := RequireAppBaseURL(cfg, envLookup(map[string]string{"BASE_URL": raw})); err == nil {
				t.Fatal("unsafe application target accepted")
			}
		})
	}
}
