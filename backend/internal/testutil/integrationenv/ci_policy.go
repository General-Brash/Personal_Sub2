// Package integrationenv provisions only disposable, test-owned CI resources.
package integrationenv

import (
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/repository/validation"
)

func validateOwnedCIEnvironment(environ func(string) string) error {
	required := map[string]string{
		"CI":                      "true",
		"GITHUB_ACTIONS":          "true",
		"RUNNER_ENVIRONMENT":      "github-hosted",
		validation.ModeEnv:        validation.ModeCIContainer,
		validation.LegacyAllowEnv: validation.DBExecutionValue,
	}
	for key, expected := range required {
		if strings.TrimSpace(environ(key)) != expected {
			return fmt.Errorf("test-owned CI containers require %s=%s", key, expected)
		}
	}
	return nil
}
