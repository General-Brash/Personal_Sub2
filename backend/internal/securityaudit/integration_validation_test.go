//go:build integration

package securityaudit

import (
	"fmt"
	"os"
	"testing"

	validation "github.com/Wei-Shaw/sub2api/internal/repository/validation"
)

var promptAuditValidationConfig validation.Config

func TestMain(m *testing.M) {
	cfg, err := validation.LoadDedicatedIntegrationConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "prompt audit integration target rejected: %v\n", err)
		os.Exit(2)
	}
	promptAuditValidationConfig = cfg
	os.Exit(m.Run())
}

func promptAuditRedisAddress() string { return validation.RedisAddress(promptAuditValidationConfig) }
func promptAuditPostgresDSN() string  { return validation.DatabaseDSN(promptAuditValidationConfig) }
