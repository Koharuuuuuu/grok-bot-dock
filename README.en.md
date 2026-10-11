<p align="center"><img src="assets/bolt-dock-banner.png" alt="Bolt Dock dashboard illustration" width="100%"></p>
<p align="center"><img src="frontend/public/brand/bolt-dock.svg" width="72" height="72" alt="Bolt Dock logo"></p>
<h1 align="center">Bolt Dock</h1>
<p align="center">A lightweight, self-hosted workspace for AI agent reports</p>
<p align="center"><a href="README.md">简体中文</a> · <a href="README.en.md">English</a></p>
<p align="center"><a href="https://github.com/Nyakooo/grok-bot-dock/actions/workflows/ci.yml"><img src="https://github.com/Nyakooo/grok-bot-dock/actions/workflows/ci.yml/badge.svg?branch=master" alt="CI"></a> <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green.svg" alt="MIT License"></a></p>

Bolt Dock collects, searches, and displays completed reports from AI agents. Agents can publish Markdown through a REST API or remote MCP server. Your data stays in a SQLite database that you host.

> The product is called Bolt Dock. The GitHub repository is still named `grok-bot-dock`.

## Features

- Automatically groups reports by source and bot, with a responsive dashboard, history, filters, and full-text search (including Chinese).
- Renders Markdown with raw HTML disabled and sanitized output.
- Streams updates to open dashboards with Server-Sent Events (SSE).
- Stores data in SQLite with WAL and FTS5 search.
- Runs as a single Go service with the Vue web app embedded in the container.
- Offers both REST publishing and a remote, stateless Streamable HTTP MCP endpoint.

## Quick start with Docker Compose

Requirements: Docker Engine with the Compose plugin. Create a private random access token before starting the service:

```sh
cp .env.example .env
# Edit .env and set a long, randomly generated ACCESS_TOKEN
docker compose pull
docker compose up -d
docker compose ps
```

Open `http://localhost:18765` on the host and sign in with the same `ACCESS_TOKEN`. The service binds to `127.0.0.1` by default and keeps its database in the `bolt-dock-data` Compose volume. For a public deployment, put it behind an HTTPS reverse proxy. See [Docker deployment](README.md#docker-部署拉取-ghcr-镜像) and [backup and restore](README.md#备份与恢复) for details.

## Local development

Requirements: Go 1.25+, Node.js 22.22.2+ or 24.15.0+, and npm.

```sh
cp .env.example .env
# Set a long, randomly generated ACCESS_TOKEN in .env
make test
make build
set -a; . ./.env; set +a; ./bolt-dock
```

The service listens on `http://localhost:18765` by default and stores data under `DATA_DIR`. For frontend development, run `cd frontend && npm ci && npm run dev`; Vite proxies `/api` to the local service.

## Publishing reports

Send a JSON report to `POST /api/v1/reports` with `Authorization: Bearer $ACCESS_TOKEN`. At minimum, provide an RFC3339 `executed_at` timestamp and non-empty Markdown `content`. Stable source and bot identity lets Bolt Dock group reports consistently; retries with the same `(source, bot_id, external_id)` are idempotent.

```sh
curl -X POST "https://YOUR_DOMAIN/api/v1/reports" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary '{
    "source":"codex",
    "stable_task_id":"daily-summary",
    "bot_name":"Daily Summary",
    "executed_at":"2026-10-11T09:00:00+08:00",
    "external_id":"run-unique-id",
    "title":"Daily summary",
    "content":"# Daily summary\n\nCompleted report content."
  }'
```

Only publish completed, reviewed reports. Do not put the access token in prompts, report bodies, URLs, logs, or screenshots. The dashboard includes a copyable agent prompt template with additional reporting and credential guidance.

## Remote MCP

The Go service exposes `/mcp` over stateless Streamable HTTP. Current tools are `publish_report` and read-only `search_reports`. Configure the URL and credentials using the MCP client’s secure settings; never place a token in a URL or a shared configuration file.

By default, MCP reuses the administrator’s `ACCESS_TOKEN` as a shared Bearer credential. This is suitable only for the owner or trusted single-user access. The service does not provide tenant isolation. OAuth is optional and is a limited Authorization Code + PKCE S256 implementation; OAuth defaults to off. Real integrations with individual Grok, Codex, Claude, or Cursor clients must be verified separately. Protocol tests alone do not establish client compatibility.

See the detailed [MCP guide](README.md#远程-mcp-接入), [OAuth architecture](docs/P8_OAUTH_ARCHITECTURE.md), [threat model](docs/P8_OAUTH_THREAT_MODEL.md), and [acceptance record](docs/P8_OAUTH_ACCEPTANCE.md).

## Configuration and deployment

- `ACCESS_TOKEN` is required for API and dashboard access.
- `PORT` and `DATA_DIR` configure the service listener and database location.
- `MAX_REQUEST_BYTES` limits request size.
- `OAUTH_ENABLED` defaults to `false`. Enabling OAuth also requires a fixed canonical HTTPS `PUBLIC_BASE_URL`.
- The Docker Compose deployment binds to loopback. Use HTTPS and reverse-proxy rate limits before exposing it publicly.

For full API details, deployment options, reverse-proxy examples, OAuth constraints, and backup instructions, see the [Chinese reference README](README.md) and the technical documents under [`docs/`](docs/).

## Contributing

Issues and focused pull requests are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before making a substantial change. Run the checks relevant to your change and describe them in your pull request. Do not include real tokens, private reports, or database files.

## Security

Please report suspected vulnerabilities privately through GitHub's security advisory feature, if enabled. See [SECURITY.md](SECURITY.md) for deployment guidance and reporting details. Bolt Dock is designed for a single trusted owner and does not provide multi-tenant isolation.

## License

Bolt Dock is licensed under the [MIT License](LICENSE). Keep the copyright and license notice with copies or substantial portions of the software. The software is provided “as is,” without warranty.
