package integrationenv

import "testing"

func TestOwnedCIEnvironmentRequiresEveryOptIn(t *testing.T) {
	base := map[string]string{
		"CI": "true", "GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "github-hosted",
		"SUB2API_VALIDATION_MODE": "ci-container", "SUB2API_ALLOW_LEGACY_CI_CONTAINERS": "ALLOW",
	}
	if err := validateOwnedCIEnvironment(func(k string) string { return base[k] }); err != nil {
		t.Fatal(err)
	}
	for key := range base {
		t.Run(key, func(t *testing.T) {
			err := validateOwnedCIEnvironment(func(k string) string {
				if k == key {
					return ""
				}
				return base[k]
			})
			if err == nil {
				t.Fatalf("missing %s must fail closed", key)
			}
		})
	}
	if err := validateOwnedCIEnvironment(func(k string) string {
		if k == "RUNNER_ENVIRONMENT" {
			return "self-hosted"
		}
		return base[k]
	}); err == nil {
		t.Fatal("a self-hosted runner must use the dedicated, approved-target gate")
	}
}
