//go:build integration

package migrations_test

import (
	"fmt"
	"os"
	"testing"

	validation "github.com/Wei-Shaw/sub2api/internal/repository/validation"
)

var migrationValidationConfig validation.Config

func TestMain(m *testing.M) {
	cfg, err := validation.LoadDedicatedIntegrationConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration integration target rejected: %v\n", err)
		os.Exit(2)
	}
	migrationValidationConfig = cfg
	os.Exit(m.Run())
}
