# Intent Classifier Deployment

The intent classifier is an internal secondary-review service. Sub2API calls it only after a configured keyword matches. Model packages stay outside the image, while the active and previous version pointers are persisted separately for restart recovery and rollback.

## Network and Storage Contract

- Docker service root: `http://intent-classifier:8080`
- Release image: `ghcr.io/general-brash/personal_sub2-intent-classifier:v0.1.6-P1.2`
- Bare-metal service root: `http://127.0.0.1:18080`
- Model packages: `/models` in Docker or `/var/lib/sub2api/intent-models` with systemd; always read-only to the service
- Runtime state: `/state/active.json` in Docker or `/var/lib/sub2api/intent-classifier-state/active.json` with systemd; writable and persistent
- Health: `/health/live` is the container/process probe; `/health/ready` reports whether a model is active
- Admin API: loopback only and protected by `INTENT_CLASSIFIER_ADMIN_TOKEN`
- Inference timeout: `INTENT_CLASSIFIER_INFERENCE_TIMEOUT_MS=250`; keep it below the Secondary Review request timeout (default `300` ms)

Do not publish the classifier container port on the host. The Compose files use `expose` only.

## Docker Compose

Run these commands from `deploy/`. The same classifier service is included in all four Compose files; add `-f <compose-file>` when using a non-default file.

1. Configure secrets and create the model directory:

```bash
cp .env.example .env
chmod 600 .env
mkdir -p intent-models
openssl rand -hex 32
```

Set the generated value as `INTENT_CLASSIFIER_ADMIN_TOKEN`. For authenticated classification, generate a different value for `INTENT_CLASSIFIER_API_TOKEN` and save that same API token through the Secondary Review page. Do not reuse the admin token.

2. Upload the exported package directory to a temporary import path outside `intent-models/`:

```text
/srv/sub2api-model-import/cyber-intent-v20260720/
  model.onnx
  tokenizer.json
  preprocessing.json
  labels.json
  calibration.json
  manifest.json
  golden_cases.json
```

Use exactly one tokenizer layout: `tokenizer.json` at the package root, or `tokenizer/tokenizer.json`. The exact package schema is documented in `services/intent-classifier/MODEL_PACKAGE.md`. `SOURCE` must be a directory, not a zip file. The installer validates `manifest.model_version`, derives the destination name from that field, and fails if the final version directory already exists.

Pull the versioned release image, then run the offline installer with the import read-only and the model root temporarily writable:

```bash
SOURCE="$(cd /srv/sub2api-model-import/cyber-intent-v20260720 && pwd)"
MODELS_DIR="${INTENT_CLASSIFIER_MODEL_DIR:-./intent-models}"
test -d "$SOURCE" || { echo "model package directory not found" >&2; exit 1; }
mkdir -p "$MODELS_DIR"
MODELS_DIR="$(cd "$MODELS_DIR" && pwd)"
IMAGE="${INTENT_CLASSIFIER_IMAGE:-ghcr.io/general-brash/personal_sub2-intent-classifier:v0.1.6-P1.2}"
docker compose pull intent-classifier
docker run --rm \
  --user "$(id -u):$(id -g)" \
  --entrypoint intent-classifier \
  --volume "$SOURCE:/import/package:ro" \
  --volume "$MODELS_DIR:/models:rw" \
  "$IMAGE" install /import/package --models-dir /models
```

From the `deploy/` directory, PowerShell with Docker Desktop uses the image's non-root UID explicitly and writes to the same `intent-models` directory mounted by Compose:

```powershell
$Source = (Resolve-Path 'C:\model-import\cyber-intent-v20260720').Path
$Models = Join-Path (Get-Location) 'intent-models'
$Image = 'ghcr.io/general-brash/personal_sub2-intent-classifier:v0.1.6-P1.2'
New-Item -ItemType Directory -Force -Path $Models | Out-Null
$Models = (Resolve-Path $Models).Path
docker run --rm `
  --user 10001:10001 `
  --entrypoint intent-classifier `
  --volume "${Source}:/import/package:ro" `
  --volume "${Models}:/models:rw" `
  $Image install /import/package --models-dir /models
if ($LASTEXITCODE -ne 0) { throw 'Model installation failed' }
```

To build the classifier locally instead, set a local image name before invoking Compose:

```bash
export INTENT_CLASSIFIER_IMAGE=sub2api-intent-classifier:local
docker compose build intent-classifier
```

The classifier Docker build context contains only the service code and dependencies. Model packages remain outside the image and are mounted at `/models` at runtime.

Only this one-off installer receives a writable model mount. The long-running service remains `/models:ro`. Model labels use the calibrated score with a fixed `0.5` classification boundary; the configurable review and block thresholds remain independent Sub2API policy.

3. Validate the installed package offline:

```bash
VERSION=cyber-intent-v20260720
docker compose run --rm --no-deps --entrypoint intent-classifier \
  intent-classifier validate "$VERSION" --model-root /models
```

`validate` does not contact the running service and does not change `/state/active.json`.

4. Start the classifier and check process liveness:

```bash
docker compose up -d intent-classifier
docker compose exec -T intent-classifier python -c \
  "import urllib.request; print(urllib.request.urlopen('http://127.0.0.1:8080/health/live').read().decode())"
```

The container stays healthy when no model is active. In that state `/health/ready` returns `503` with `code: model_not_ready`, which is a business readiness state rather than a reason to restart the process.

5. Preload, activate, inspect, and roll back models:

```bash
docker compose exec -T intent-classifier \
  intent-classifier preload "$VERSION" --admin-url http://127.0.0.1:8080
docker compose exec -T intent-classifier \
  intent-classifier activate "$VERSION" --admin-url http://127.0.0.1:8080
docker compose exec -T intent-classifier \
  intent-classifier list --admin-url http://127.0.0.1:8080
docker compose exec -T intent-classifier \
  intent-classifier rollback --admin-url http://127.0.0.1:8080
```

After activation, verify readiness:

```bash
docker compose exec -T intent-classifier python -c \
  "import urllib.request; print(urllib.request.urlopen('http://127.0.0.1:8080/health/ready').read().decode())"
```

The lifecycle CLI returns `0` on success, `2` for arguments or offline package validation failures, `3` for rejected/conflicting admin operations, and `4` for network or service failures.

6. In the Sub2API admin interface, open Secondary Review and save:

- Service endpoint: `http://intent-classifier:8080`
- Access token: the `INTENT_CLASSIFIER_API_TOKEN` value, if configured
- Expected model version: the activated version
- Mode: start with `shadow`; switch to `enforce` only after reviewing shadow results

Keep the `intent_classifier_state` volume when upgrading or migrating. Removing it loses the active/previous pointers but does not delete the read-only model packages.

## systemd

The classifier uses port `18080` on bare metal because Sub2API normally uses `8080`.

1. Install the service into an isolated virtual environment:

```bash
sudo install -d -o sub2api -g sub2api /opt/sub2api/intent-classifier
sudo install -d -o sub2api -g sub2api -m 0750 /var/lib/sub2api/intent-models
sudo install -d -o sub2api -g sub2api -m 0750 /var/lib/sub2api/intent-classifier-state
sudo cp -a services/intent-classifier/. /opt/sub2api/intent-classifier/
sudo chown -R sub2api:sub2api /opt/sub2api/intent-classifier
sudo -u sub2api python3 -m venv /opt/sub2api/intent-classifier/.venv
sudo -u sub2api /opt/sub2api/intent-classifier/.venv/bin/pip install /opt/sub2api/intent-classifier
```

2. Install and edit the protected environment file and unit:

```bash
sudo install -d -m 0755 /etc/sub2api
sudo install -o root -g sub2api -m 0640 \
  deploy/intent-classifier.env.example /etc/sub2api/intent-classifier.env
sudo editor /etc/sub2api/intent-classifier.env
sudo install -m 0644 deploy/intent-classifier.service /etc/systemd/system/intent-classifier.service
sudo systemctl daemon-reload
```

Replace `INTENT_CLASSIFIER_ADMIN_TOKEN`; optionally set an independent `INTENT_CLASSIFIER_API_TOKEN`. Leave `INTENT_CLASSIFIER_ACTIVE_VERSION` empty to restore the persisted active version after restarts.

3. Upload an exported package directory to a temporary path, install it atomically, and validate it before activation:

```bash
SOURCE=/srv/sub2api-model-import/cyber-intent-v20260720
sudo test -d "$SOURCE"
sudo chown -R root:sub2api "$SOURCE"
sudo find "$SOURCE" -type d -exec chmod 0750 {} +
sudo find "$SOURCE" -type f -exec chmod 0640 {} +
sudo -u sub2api /opt/sub2api/intent-classifier/.venv/bin/intent-classifier \
  install "$SOURCE" --models-dir /var/lib/sub2api/intent-models

VERSION=cyber-intent-v20260720
sudo -u sub2api /opt/sub2api/intent-classifier/.venv/bin/intent-classifier \
  validate "$VERSION" --model-root /var/lib/sub2api/intent-models
```

The installer reads the version only from the validated manifest and refuses to overwrite an existing destination. The systemd service still sees the model root as read-only through `ReadOnlyPaths`.

4. Start the service, then preload and activate through its loopback admin API:

```bash
sudo systemctl enable --now intent-classifier
curl --fail --silent http://127.0.0.1:18080/health/live

sudo -u sub2api sh -c 'set -a; . /etc/sub2api/intent-classifier.env; set +a; \
  /opt/sub2api/intent-classifier/.venv/bin/intent-classifier preload cyber-intent-v20260720 \
  --admin-url http://127.0.0.1:18080'
sudo -u sub2api sh -c 'set -a; . /etc/sub2api/intent-classifier.env; set +a; \
  /opt/sub2api/intent-classifier/.venv/bin/intent-classifier activate cyber-intent-v20260720 \
  --admin-url http://127.0.0.1:18080'

curl --fail --silent http://127.0.0.1:18080/health/ready
```

Configure the Secondary Review service endpoint as `http://127.0.0.1:18080`. Rollback uses the same protected environment:

```bash
sudo -u sub2api sh -c 'set -a; . /etc/sub2api/intent-classifier.env; set +a; \
  /opt/sub2api/intent-classifier/.venv/bin/intent-classifier rollback \
  --admin-url http://127.0.0.1:18080'
```

Inspect service logs with `journalctl -u intent-classifier -f`. The service does not log request text, matched keywords, or authorization headers.
