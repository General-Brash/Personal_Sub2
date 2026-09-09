//go:build integration

package securityaudit

import (
	"context"
	"fmt"
	"os"
	"testing"

	validation "github.com/Wei-Shaw/sub2api/internal/repository/validation"
	"github.com/Wei-Shaw/sub2api/internal/testutil/integrationenv"
)

var promptAuditValidationConfig validation.Config

func TestMain(m *testing.M) {
	cfg, cleanup, err := integrationenv.Load(context.Background(), true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prompt audit integration target rejected: %v\n", err)
		os.Exit(2)
	}
	promptAuditValidationConfig = cfg
	code := m.Run()
	if err := cleanup(); err != nil {
		fmt.Fprintf(os.Stderr, "prompt audit CI fixture cleanup failed: %v\n", err)
		code = 1
	}
	os.Exit(code)
}

func promptAuditRedisAddress() string { return validation.RedisAddress(promptAuditValidationConfig) }
func promptAuditPostgresDSN() string  { return validation.DatabaseDSN(promptAuditValidationConfig) }
