# Contributing to Bolt Dock

Thanks for considering a contribution. Bug reports, documentation improvements, and focused pull requests are welcome.

## Before you start

- Search existing issues and pull requests to avoid duplicate work.
- For a substantial feature or behavior change, open an issue first to agree on scope.
- Keep changes focused and explain the user problem they solve.
- Never include real access tokens, private reports, database files, or other sensitive data in issues, commits, or screenshots.

## Development

Requirements: Go 1.25+, Node.js 22.22.2+ or 24.15.0+, and npm.

```sh
cp .env.example .env
# Set a private, randomly generated ACCESS_TOKEN in .env
make test
make build
```

For frontend-only work:

```sh
cd frontend
npm ci
npm test
npm run build
```

Before opening a pull request, run the checks relevant to your change and describe what you ran. Include reproduction steps for fixes and screenshots for visible UI changes. Do not send test data to a deployed instance.

## Pull requests

- Describe the change and its motivation.
- Note configuration or migration impact.
- Keep secrets and generated local data out of the patch.
- Update the README or technical docs when behavior or setup changes.
