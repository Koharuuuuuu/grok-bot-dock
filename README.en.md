<p align="center"><img src="assets/bolt-dock-banner.png" alt="Bolt Dock dashboard illustration" width="100%"></p>
<p align="center"><img src="frontend/public/brand/bolt-dock.svg" width="72" height="72" alt="Bolt Dock logo"></p>
<h1 align="center">Bolt Dock</h1>
<p align="center">A lightweight, self-hosted workspace for AI agent reports</p>
<p align="center"><a href="README.md">简体中文</a> · <a href="README.en.md">English</a></p>
<p align="center"><a href="https://github.com/Nyakooo/grok-bot-dock/actions/workflows/ci.yml"><img src="https://github.com/Nyakooo/grok-bot-dock/actions/workflows/ci.yml/badge.svg?branch=master" alt="CI"></a> <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green.svg" alt="MIT License"></a></p>

Bolt Dock collects completed reports from AI agents into a workspace you host. Agents publish through a REST API or remote MCP; you organize reports by topic, search, and read them in one place. Data stays in your SQLite database.

## Quick start

Requirements: Docker Engine and the Compose plugin. Set a long, random access token before starting:

```sh
cp .env.example .env
# Edit .env and set ACCESS_TOKEN
docker compose pull
docker compose up -d
docker compose ps
```

Open `http://localhost:18765` on the server and sign in with the same token. The service binds to loopback by default. Configure an HTTPS reverse proxy before making it reachable from the internet. See the [deployment and operations guide](docs/DEPLOYMENT.md) for setup, upgrades, backup, and recovery.

## How it works

Topics are the primary organizing unit; the app does not impose fixed model or agent sections. The first completed report creates a topic. Later runs reuse a stable identity or explicit `topic_id`. Groups, following, pins, and search help organize a growing topic list without triggering model calls.

Archiving uses a stop request followed by confirmation that the external task has stopped. Archived topics keep their history and reject new reports. Restoring a topic does not restart a schedule on the external platform. See the [topic refactor acceptance record](docs/TOPIC_REFACTOR_ACCEPTANCE.md).

Reports support full-text search, per-report unread state, Markdown reading, and revocable expiring share links. Anyone with a share link can read that report, so share only content suitable for public access.

## Connect an agent

Sign in and choose **Copy connection instructions**, then give the prompt to your agent. It contains service and topic guidance but no administrator secret. You can also connect through REST or `/mcp` Streamable HTTP.

By default, MCP uses the shared `ACCESS_TOKEN`. This is suitable only for the owner or a trusted single-user setup; the service does not provide tenant isolation or separate agent credentials. OAuth is optional and disabled by default. Verify support for each client and version separately.

See the [Agent integration guide](docs/AGENT_INTEGRATION.md) for REST fields, MCP tools, credentials, and OAuth limits.

## Backup and security

On SIGTERM, the service closes SSE streams, drains HTTP requests, and checkpoints the SQLite WAL. Copy only `bolt-dock.db` after logs show `shutdown complete; SQLite WAL checkpointed`. If the container is force-stopped or checkpointing fails, do not copy only the main database file. Follow the [backup and recovery guide](docs/DEPLOYMENT.md).

Do not put access tokens in prompts, report bodies, URLs, logs, or shared configuration. Treat report share links as bearer credentials and share only content suitable for public reading. See [SECURITY.md](SECURITY.md).

## Documentation

- [Deployment and operations](docs/DEPLOYMENT.md)
- [Agent integration](docs/AGENT_INTEGRATION.md)
- [Documentation index](docs/README.md)
- [Architecture and data model](docs/TECHNICAL_DESIGN.md)

## Local development

Requirements: Go 1.25+, Node.js 22.22.2+ or 24.15.0+, and npm.

```sh
cp .env.example .env
# Set a random ACCESS_TOKEN
make test
make build
set -a; . ./.env; set +a; ./bolt-dock
```

For frontend development, run `cd frontend && npm ci && npm run dev`. See [CONTRIBUTING.md](CONTRIBUTING.md) for checks and contribution guidance.

## Contributing and license

Issues and focused pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) before substantial changes. Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md). Bolt Dock is licensed under the [MIT License](LICENSE).
