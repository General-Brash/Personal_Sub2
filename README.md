# Personal_Sub2

Personal_Sub2 is a personally developed and independently maintained edition based on official Sub2API `v0.1.178`. This integration line is released as `v0.1.178-P1`.

English | [中文](README_CN.md) | [日本語](README_JA.md)

## Personal Edition Features

- **Temporary credit**: Tracks expiring grants, consumption, and available temporary credit separately from the permanent balance.
- **Daily check-in**: Issues configurable temporary-credit rewards and provides check-in status and history.
- **Bank workflows**: Supports temporary-credit advances and exchanges from permanent balance to temporary credit, with configurable limits, settlement rules, and ledger records.
- **Security-audit secondary review**: Improves ASCII keyword-boundary matching and can send matched content to the independent `intent-classifier` service. It supports `off`, `shadow`, and `enforce` modes, plus model-package validation, activation, and rollback.

Production model weights are not included. Before enabling model-backed secondary review, prepare and activate a package as described in [`MODEL_PACKAGE.md`](services/intent-classifier/MODEL_PACKAGE.md).

## Official v0.1.178 Integration

- **Upstream URL safety**: Validates client-controlled OpenAI Responses subpaths, Gemini model/action paths, and Grok video request IDs before forwarding, rejecting path fragments that could alter upstream URL structure.
- **Runtime pricing and billing**: Docker and GoReleaser image builds now carry fallback pricing resources at runtime; GPT-5.6 Luna/Terra rates and GLM-5.2 fallback pricing are corrected.
- **Container privilege hardening**: Every official Compose variant enables `no-new-privileges`, preventing application processes from acquiring additional privileges at runtime.
- **Proxy stream-circuit resilience**: OpenAI proxy disconnect quarantine now fails open when every candidate shares a quarantined proxy, collapses burst disconnects into one event, and provides an explicit disable switch.
- **Routing and scheduling correctness**: Composite groups expose their configured concrete model platforms while ordinary group isolation remains intact, and token refresh skips unschedulable accounts.
- **Protocol and UI fixes**: Generates standards-compliant SMTP messages, improves Anthropic classifier/count-token compatibility and Qwen3Guard auxiliary-field handling, and corrects subscription expiry labels and long plan-title display.

## Installation and Upgrade

The installation script targets Linux amd64/arm64 servers with PostgreSQL and Redis already running, and requires root privileges:

```bash
curl -sSL https://raw.githubusercontent.com/General-Brash/Personal_Sub2/main/deploy/install.sh | sudo bash
```

After installation, open `http://YOUR_SERVER_IP:8080` to complete initial setup. Useful commands:

```bash
# Check status and logs
sudo systemctl status sub2api
sudo journalctl -u sub2api -f

# Upgrade to the latest release from the personal repository
curl -sSL https://raw.githubusercontent.com/General-Brash/Personal_Sub2/main/deploy/install.sh | sudo bash -s -- upgrade
```

Version checks and upgrades are also available in the admin dashboard. Back up the database, configuration, and data directory before upgrading.

The personal-edition container image is published at:

```text
ghcr.io/general-brash/personal_sub2
```

See [`deploy/`](deploy/) for deployment files and runtime settings. For container deployments, ensure the application image explicitly points to the personal-edition image above to avoid mixing versions.

## Build from Source

Requirements: Go 1.26.5, Node.js 20+, pnpm 9, PostgreSQL, and Redis.

```bash
git clone https://github.com/General-Brash/Personal_Sub2.git
cd Personal_Sub2

cd frontend
pnpm install --frozen-lockfile
pnpm run build

cd ../backend
go build -tags embed -ldflags="-X main.Version=$(./scripts/resolve-version.sh)" -o sub2api ./cmd/server
./sub2api
```

On first start, open `http://localhost:8080` and use the setup wizard to configure the database, Redis, and administrator account.

## Development and Verification

```bash
# Backend tests
cd backend
make test-unit

# Frontend checks
cd ../frontend
pnpm run lint:check
pnpm run typecheck
pnpm run test:run
```

See [`DEV_GUIDE.md`](DEV_GUIDE.md) for additional repository development conventions.

## Security and Responsible Use

- Confirm that your use complies with applicable laws and the terms of every connected service.
- Use unique strong passwords and fixed secrets in production, and restrict network access to administration and database services.
- Never commit or disclose API keys, access tokens, database passwords, or sensitive values from `.env` and `config.yaml`.
- Back up and validate in a non-production environment before upgrades, migrations, or security-policy changes.
- This project is provided as-is; users are responsible for account, service, data, and compliance risks.

## License

This project is licensed under the [GNU Lesser General Public License v3.0](LICENSE) or later.
