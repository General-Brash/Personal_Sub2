# Personal_Sub2 0.2.0-P1 Container Image

Personal_Sub2 is the Personal edition integrating the official `v0.2.0` gateway baseline while retaining Personal billing, deployment, and intent-classifier capabilities.

## Quick Start

```bash
docker run -d \
  --name sub2api \
  -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@host:5432/sub2api" \
  -e REDIS_URL="redis://host:6379" \
  ghcr.io/general-brash/personal_sub2:latest
```

## Docker Compose

The repository Compose files also include the internal intent classifier used for keyword-triggered secondary review. Its model directory is mounted read-only, its lifecycle state uses a persistent volume, and port `8080` is not published to the host. See [`INTENT_CLASSIFIER.md`](./INTENT_CLASSIFIER.md) for model validation, activation, readiness, and rollback commands.

```yaml
version: '3.8'

services:
  sub2api:
    image: ghcr.io/general-brash/personal_sub2:latest
    ports:
      - "8080:8080"
    environment:
      - DATABASE_URL=postgres://postgres:postgres@db:5432/sub2api?sslmode=disable
      - REDIS_URL=redis://redis:6379
    depends_on:
      - db
      - redis

  db:
    image: postgres:15-alpine
    environment:
      - POSTGRES_USER=postgres
      - POSTGRES_PASSWORD=postgres
      - POSTGRES_DB=sub2api
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    volumes:
      - redis_data:/data

volumes:
  postgres_data:
  redis_data:
```

## Startup and Database Recovery

Personal_Sub2 applies database migrations during application startup. PostgreSQL can
remain in its recovery/startup phase briefly after a host or Docker daemon
restart. The application retries transient PostgreSQL startup and connection
errors with bounded exponential backoff, then starts automatically when the
database becomes ready. Authentication errors, migration checksum mismatches,
SQL errors, and incompatible data fail immediately.

The Compose deployment checks PostgreSQL readiness with both `pg_isready` and a
simple SQL query. `depends_on: condition: service_healthy` orders a fresh
Compose start, while application-level retries cover recovery of existing
containers after a host restart.

## Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `DATABASE_URL` | PostgreSQL connection string | Yes | - |
| `REDIS_URL` | Redis connection string | Yes | - |
| `PORT` | Server port | No | `8080` |
| `GIN_MODE` | Gin framework mode (`debug`/`release`) | No | `release` |

## Supported Architectures

- `linux/amd64`
- `linux/arm64`

## Tags

- `latest` - Latest stable Personal_Sub2 image
- `0.2.0-P1` - Normalized Personal release image tag
- `x.y` / `x` - Rolling minor/major aliases when published
- `sha-<commit>` - Image built from a specific commit

## Links

- [GitHub Repository](https://github.com/General-Brash/Personal_Sub2)
- [Documentation](https://github.com/General-Brash/Personal_Sub2#readme)
