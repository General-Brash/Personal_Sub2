# Personal_Sub2 Container Image

Personal_Sub2 is a personally developed and maintained edition based on the `1.6.0` codebase.

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

- `latest` - Latest personal-edition image
- `v0.1.6-Pn` - Personal release image
- `sha-<commit>` - Image built from a specific commit

## Links

- [GitHub Repository](https://github.com/General-Brash/Personal_Sub2)
- [Documentation](https://github.com/General-Brash/Personal_Sub2#readme)
