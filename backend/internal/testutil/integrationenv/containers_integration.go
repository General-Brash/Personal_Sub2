//go:build integration

package integrationenv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository/validation"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// Load keeps the dedicated gate unchanged. The CI alternative creates its own
// containers and derives endpoints from their runtime handles; it never accepts
// a caller-supplied database/Redis address or claims a G_CODE candidate PASS.
func Load(ctx context.Context, withRedis bool) (validation.Config, func() error, error) {
	noop := func() error { return nil }
	cfg, err := validation.LoadFromProcess()
	if err != nil {
		return validation.Config{}, noop, err
	}
	if cfg.Mode == validation.ModeDedicated {
		cfg, err = validation.LoadDedicatedIntegrationConfig()
		return cfg, noop, err
	}
	if err := validateOwnedCIEnvironment(os.Getenv); err != nil {
		return validation.Config{}, noop, err
	}
	var pg *tcpostgres.PostgresContainer
	var redis *tcredis.RedisContainer
	dsnEnv := ""
	cleanup := func() error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var cleanupErr error
		if redis != nil {
			cleanupErr = errors.Join(cleanupErr, redis.Terminate(cleanupCtx))
		}
		if pg != nil {
			cleanupErr = errors.Join(cleanupErr, pg.Terminate(cleanupCtx))
		}
		if dsnEnv != "" {
			cleanupErr = errors.Join(cleanupErr, os.Unsetenv(dsnEnv))
		}
		return cleanupErr
	}
	ready := false
	defer func() {
		if !ready {
			_ = cleanup()
		}
	}()
	pg, err = tcpostgres.Run(ctx, "postgres:18.1-alpine3.23",
		tcpostgres.WithDatabase("sub2api_ci"), tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"), tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return validation.Config{}, noop, fmt.Errorf("start test-owned PostgreSQL: %w", err)
	}
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	if err != nil {
		return validation.Config{}, noop, fmt.Errorf("resolve test-owned PostgreSQL: %w", err)
	}
	endpoint, err := url.Parse(dsn)
	if err != nil {
		return validation.Config{}, noop, errors.New("invalid test-owned PostgreSQL endpoint")
	}
	if !isLoopbackHost(endpoint.Hostname()) {
		return validation.Config{}, noop, errors.New("test-owned PostgreSQL must use a loopback Docker endpoint")
	}
	port, err := strconv.Atoi(endpoint.Port())
	if err != nil {
		return validation.Config{}, noop, errors.New("invalid test-owned PostgreSQL port")
	}
	candidateDSNEnv := "SUB2API_CI_DATABASE_" + strings.ToUpper(pg.GetContainerID())
	if _, exists := os.LookupEnv(candidateDSNEnv); exists {
		return validation.Config{}, noop, errors.New("test-owned DSN environment collision")
	}
	if err := os.Setenv(candidateDSNEnv, dsn); err != nil {
		return validation.Config{}, noop, err
	}
	dsnEnv = candidateDSNEnv
	cfg = validation.Config{
		Mode:             validation.ModeCIContainer,
		TargetID:         "ci-container-" + pg.GetContainerID(),
		Database:         validation.Endpoint{Host: endpoint.Hostname(), Port: port, Database: "sub2api_ci", User: "postgres", DSNEnv: dsnEnv},
		AllowAutoMigrate: true,
		FixturePolicy:    validation.FixturePolicy{AllowReset: true, ApprovedClone: pg.GetContainerID()},
	}
	if withRedis {
		redis, err = tcredis.Run(ctx, "redis:8.4-alpine")
		if err != nil {
			return validation.Config{}, noop, fmt.Errorf("start test-owned Redis: %w", err)
		}
		host, err := redis.Host(ctx)
		if err != nil {
			return validation.Config{}, noop, err
		}
		if !isLoopbackHost(host) {
			return validation.Config{}, noop, errors.New("test-owned Redis must use a loopback Docker endpoint")
		}
		port, err := redis.MappedPort(ctx, "6379/tcp")
		if err != nil {
			return validation.Config{}, noop, err
		}
		cfg.Redis = validation.RedisEndpoint{Host: host, Port: port.Int(), DB: 0}
	}
	ready = true
	return cfg, cleanup, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
