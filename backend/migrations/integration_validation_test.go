//go:build integration

package migrations_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	validation "github.com/Wei-Shaw/sub2api/internal/repository/validation"
	"github.com/Wei-Shaw/sub2api/internal/testutil/integrationenv"
)

var migrationValidationConfig validation.Config

func TestMain(m *testing.M) {
	cfg, cleanup, err := integrationenv.Load(context.Background(), false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration integration target rejected: %v\n", err)
		os.Exit(2)
	}
	migrationValidationConfig = cfg
	code := m.Run()
	if err := cleanup(); err != nil {
		fmt.Fprintf(os.Stderr, "migration CI fixture cleanup failed: %v\n", err)
		code = 1
	}
	os.Exit(code)
}
