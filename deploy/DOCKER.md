# Personal_Sub2 Container Image

Personal_Sub2 is a personally developed and maintained edition based on the official `v0.1.178` codebase and incorporating official updates through `v0.1.183`.

## Quick Start

Use the maintained local-directory Compose configuration rather than an ad-hoc `docker run` command:

```bash
cd deploy
cp .env.example .env
mkdir -p data postgres_data redis_data
# Edit .env before starting; POSTGRES_PASSWORD and the host binding are the important values.
docker compose -f docker-compose.local.yml up -d
```

The local Compose configuration uses `postgres:18-alpine` and `redis:8-alpine`, persists application/database/Redis data under `deploy/`, and includes the independent `intent-classifier` service. Check status and logs with:

```bash
docker compose -f docker-compose.local.yml ps
docker compose -f docker-compose.local.yml logs -f sub2api
```

## Docker Compose

The maintained file is [`deploy/docker-compose.local.yml`](./docker-compose.local.yml). It keeps the Personal_Sub2 application image and the independent `intent-classifier` service together. The classifier model directory is mounted read-only, lifecycle state uses a persistent volume, and the classifier port is exposed only inside the Compose network. See [`INTENT_CLASSIFIER.md`](./INTENT_CLASSIFIER.md) for model validation, activation, readiness, and rollback commands.

### Pinning the application image

All four Compose files use the same `SUB2API_IMAGE` override for the `sub2api` service:

```yaml
image: ${SUB2API_IMAGE:-ghcr.io/general-brash/personal_sub2:latest}
```

When `SUB2API_IMAGE` is unset, the default remains `ghcr.io/general-brash/personal_sub2:latest` for compatibility with existing deployments. For a reproducible deployment, set it in `deploy/.env` to a release tag or an immutable digest:

```dotenv
SUB2API_IMAGE=ghcr.io/general-brash/personal_sub2:v0.1.183-P1
# Or pin an immutable registry digest:
SUB2API_IMAGE=ghcr.io/general-brash/personal_sub2@sha256:<digest>
```

Then inspect the resolved image before starting the stack:

```bash
docker compose -f docker-compose.local.yml config
docker compose -f docker-compose.local.yml pull
docker compose -f docker-compose.local.yml up -d
```

`docker-compose.dev.yml` also uses this value as the local build image name, so it can be set to a local tag such as `personal_sub2:dev` when building from source. The independent classifier image remains controlled by `INTENT_CLASSIFIER_IMAGE`.

## Environment Variables

`docker-compose.local.yml` reads `deploy/.env` for Compose interpolation. The variables below are the actual operator inputs for this file; they are not interchangeable with every environment variable visible in the application code.

### Values configurable from `deploy/.env`

| Variable | Effect in `docker-compose.local.yml` |
|----------|---------------------------------------|
| `BIND_HOST` | Host-side address for the published Sub2API port. Defaults to `0.0.0.0`. |
| `SUB2API_IMAGE` | Main `sub2api` image used by all four Compose files. Defaults to `ghcr.io/general-brash/personal_sub2:latest`; set a release tag or digest to pin the deployment. |
| `SERVER_PORT` | Host-side port in `${SERVER_PORT:-8080}:8080`. Defaults to `8080`; the container-side port remains `8080`. |
| `SERVER_MODE` | Application mode passed to the container, normally `release` or `debug`. |
| `POSTGRES_USER` | PostgreSQL initialization user and the value mapped to the application's `DATABASE_USER`. |
| `POSTGRES_PASSWORD` | Required PostgreSQL initialization password and the value mapped to `DATABASE_PASSWORD`. |
| `POSTGRES_DB` | PostgreSQL initialization database and the value mapped to `DATABASE_DBNAME`. |
| `REDIS_USERNAME` | Optional Redis username passed to the application. |
| `REDIS_PASSWORD` | Optional Redis password passed to Redis and the application. |
| `REDIS_DB` | Redis database number passed to the application. |
| `SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS` | Comma-separated upstream API hostnames used when the URL allowlist is enabled. The template lists the application defaults; adjust it for the required upstreams, and wildcard hosts such as `*.openai.azure.com` are supported. |
| `DATABASE_MAX_OPEN_CONNS` | Application PostgreSQL pool limit. |
| `DATABASE_MAX_IDLE_CONNS` | Application PostgreSQL idle-pool limit. |
| `DATABASE_CONN_MAX_LIFETIME_MINUTES` | Application PostgreSQL connection lifetime. |
| `DATABASE_CONN_MAX_IDLE_TIME_MINUTES` | Application PostgreSQL idle connection lifetime. |
| `INTENT_CLASSIFIER_*` | Classifier image, model directory, tokens, limits, and logging settings used by the independent classifier service. |
| `GATEWAY_*` | Gateway limits, scheduling, timeout, and stream settings exposed by this Compose file. |

The complete list of interpolation variables is maintained in [`deploy/.env.example`](./.env.example). In particular, `POSTGRES_USER`, `POSTGRES_PASSWORD`, and `POSTGRES_DB` configure the PostgreSQL service; `POSTGRES_MAX_CONNECTIONS`, `POSTGRES_SHARED_BUFFERS`, `POSTGRES_EFFECTIVE_CACHE_SIZE`, and `POSTGRES_MAINTENANCE_WORK_MEM` are documented database tuning values but are not automatically injected as PostgreSQL server flags by this local Compose file.

### Standalone external database and Redis

`docker-compose.standalone.yml` does not start PostgreSQL or Redis. It requires
these external connection inputs before Compose can resolve the file:

- `DATABASE_HOST`, `DATABASE_PASSWORD`, and `REDIS_HOST` are required and must
  be non-empty.
- `DATABASE_PORT` and `REDIS_PORT` default to `5432` and `6379` in
  `deploy/.env.example`; change them when the external services use different
  ports.
- `DATABASE_USER`, `DATABASE_DBNAME`, `DATABASE_SSLMODE`, Redis credentials,
  and `REDIS_ENABLE_TLS` use the standalone Compose defaults unless overridden.

The corresponding commented inputs are included in [`deploy/.env.example`](./.env.example).
Uncomment and set them before using the standalone file, for example:

```dotenv
DATABASE_HOST=db.example.internal
DATABASE_PORT=5432
DATABASE_USER=sub2api
DATABASE_PASSWORD=replace-with-the-database-password
DATABASE_DBNAME=sub2api
DATABASE_SSLMODE=require
REDIS_HOST=redis.example.internal
REDIS_PORT=6379
REDIS_USERNAME=
REDIS_PASSWORD=replace-with-the-redis-password
REDIS_ENABLE_TLS=false
```

### Container-internal values deliberately fixed by this file

The local Compose network uses service names and fixed container ports. These values are not overridden by putting the same names in `deploy/.env`:

| Container environment | Value in the local Compose file | Reason |
|-----------------------|--------------------------------|--------|
| `SERVER_HOST` | `0.0.0.0` | The application listens on all interfaces inside its container. |
| `SERVER_PORT` | `8080` | The application listens on the container port; the host-side port is controlled separately by the `.env` `SERVER_PORT` used in the `ports` mapping. |
| `DATABASE_HOST` | `postgres` | The PostgreSQL Compose service name. |
| `DATABASE_PORT` | `5432` | The PostgreSQL container port. |
| `DATABASE_SSLMODE` | `disable` | The local network uses the Compose PostgreSQL service directly. |
| `REDIS_HOST` | `redis` | The Redis Compose service name. |
| `REDIS_PORT` | `6379` | The Redis container port. |
| `DATABASE_USER` / `DATABASE_PASSWORD` / `DATABASE_DBNAME` | Derived from `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | The application and PostgreSQL service must use the same initialization credentials. |

For this local file, `DATABASE_HOST`, `DATABASE_PORT`, `DATABASE_USER`, `DATABASE_PASSWORD`, `DATABASE_DBNAME`, `DATABASE_SSLMODE`, `REDIS_HOST`, `REDIS_PORT`, and `SERVER_HOST` are application-container values, not independent `.env` connection endpoints. `DATABASE_PORT=5432` in `.env.example` is retained for direct-application or other deployment scenarios; this file intentionally keeps the in-network database port at `5432`.

### Host mapping versus direct application configuration

`BIND_HOST` and the `.env` value `SERVER_PORT` control only the host-side port mapping:

```text
${BIND_HOST:-0.0.0.0}:${SERVER_PORT:-8080} -> container 8080
```

For example, `BIND_HOST=127.0.0.1` and `SERVER_PORT=18080` expose the application at `127.0.0.1:18080`, while the application still receives `SERVER_HOST=0.0.0.0` and `SERVER_PORT=8080` inside the container. Do not set `SERVER_HOST` or `DATABASE_HOST` in `.env` expecting them to change this Compose topology.

If the Go application is run directly outside Compose, configure its `DATABASE_*`, `REDIS_*`, and `SERVER_*` values according to the direct-application configuration documentation instead of using the local Compose assumptions above.

## Supported Architectures

- `linux/amd64`
- `linux/arm64`

## Tags

- `latest` - Latest personal-edition image
- `v0.1.183-P1` - Personal release image
- `sha-<commit>` - Image built from a specific commit

## Links

- [GitHub Repository](https://github.com/General-Brash/Personal_Sub2)
- [Documentation](https://github.com/General-Brash/Personal_Sub2#readme)