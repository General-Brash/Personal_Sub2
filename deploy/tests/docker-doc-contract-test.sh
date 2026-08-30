#!/bin/bash

set -euo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "${TEST_DIR}/.." && pwd)"
DOC="${DEPLOY_DIR}/DOCKER.md"
ENV_EXAMPLE="${DEPLOY_DIR}/.env.example"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

[[ -f "${DOC}" ]] || fail 'deploy/DOCKER.md is missing'
[[ -f "${ENV_EXAMPLE}" ]] || fail 'deploy/.env.example is missing'

for expected in \
    '# DATABASE_HOST=your-postgres-host' \
    '# DATABASE_PASSWORD=change_this_secure_password' \
    '# REDIS_HOST=your-redis-host'; do
    grep -Fq -- "${expected}" "${ENV_EXAMPLE}" \
        || fail "deploy/.env.example is missing ${expected}"
done

if grep -Eq '(^|[^A-Za-z0-9_])(DATABASE_URL|REDIS_URL)([^A-Za-z0-9_]|$)' "${DOC}"; then
    fail 'deploy/DOCKER.md must not document unsupported DATABASE_URL or REDIS_URL variables'
fi

# Guard against the old table that incorrectly described container-internal
# endpoints as independent .env inputs.
for forbidden in \
    '| `DATABASE_HOST` | PostgreSQL host name |' \
    '| `REDIS_HOST` | Redis host name |' \
    '| `SERVER_HOST` | Application bind address |'; do
    grep -Fq -- "${forbidden}" "${DOC}" \
        && fail "deploy/DOCKER.md still presents ${forbidden} as a direct input"
done

for expected in \
    'docker-compose.local.yml' \
    'BIND_HOST' \
    'SERVER_PORT' \
    'host-side' \
    'container-side' \
    'POSTGRES_USER' \
    'POSTGRES_PASSWORD' \
    'POSTGRES_DB' \
    'DATABASE_HOST' \
    'DATABASE_HOST` | `postgres' \
    'DATABASE_PORT` | `5432' \
    'DATABASE_SSLMODE` | `disable' \
    'REDIS_HOST` | `redis' \
    'REDIS_PORT` | `6379' \
    'SERVER_HOST` | `0.0.0.0' \
    'POSTGRES_MAX_CONNECTIONS' \
    'not automatically injected' \
    '${BIND_HOST:-0.0.0.0}:${SERVER_PORT:-8080} -> container 8080' \
    'postgres:18-alpine' \
    'redis:8-alpine' \
    'docker-compose.standalone.yml' \
    'Standalone external database and Redis' \
    'DATABASE_HOST`, `DATABASE_PASSWORD`, and `REDIS_HOST`' \
    'corresponding commented inputs'; do
    grep -Fq -- "${expected}" "${DOC}" \
        || fail "deploy/DOCKER.md is missing ${expected}"
done

printf 'docker documentation contract test passed.\n'