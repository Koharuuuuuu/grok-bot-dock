# P8 OAuth acceptance record

Updated: 2026-10-10
Base SHA: `4023848d7878bc94038eaf2588bc410c51364dad`
Working branch: `feature/p8-mcp-oauth`
Local implementation commit: `cb48a70d8bb4f3254dc3c96963812b87e2b3afe5`

Latest `origin/master` check: `4023848d7878bc94038eaf2588bc410c51364dad` (2026-10-10); branch is based on current remote master.

## Gate status

| Gate | Status | Evidence / remaining work |
| --- | --- | --- |
| G0 Source audit and frozen baseline | **PASS** | [`P8_BASELINE_AUDIT.md`](./P8_BASELINE_AUDIT.md); source tree audited, base tests passed, no production access. |
| G1 Protocol and compatibility contract | **PASS (local core)** | MCP discovery endpoints, configured issuer/resource, exact registered callback matching, PKCE S256 and owner-pre-registered client flow implemented and tested. URL encoding/trailing-slash edge matrix and real client-specific configuration remain unverified. |
| G2 Auth core | **PASS (isolated core)** | Wrong/empty/plain PKCE, invalid client/redirect/resource, one-time/concurrent code exchange, expired-token rejection, read/write scope enforcement, immediate revoke, client disable and SQLite restart/backup-restore are covered by local tests. Broader issuer/host attack matrix remains open in Phase 5. |
| G3 API/UI integration | **PASS (local core)** | Legacy Bearer/REST regression and OAuth-only MCP access are covered; consent/CSRF/origin/deny and owner client/grant flows tested; local browser login/client registration/theme smoke had zero console errors. Keyboard-only and real mobile device checks remain unverified. |
| G4 Release readiness | **BLOCKED (external)** | Go/UI builds, race suite, vet, Docker build and isolated container health/discovery/401 smoke passed. Production reverse proxy/TLS, production backup and actual third-party OAuth remain unavailable and unverified. |

## Baseline

- `go test ./...`: PASS on base SHA `4023848d7878bc94038eaf2588bc410c51364dad`.
- `cd frontend && npm ci && npm test && npm run build`: PASS on base SHA; Vitest 13/13 and Vite build passed.
- Test data was local; no production database, external client account, or real report was used.
- `.playwright-cli/` existed as untracked user data at task start and is preserved.

## Implementation evidence log

| Date | Gate / change | Files | Commands and results | Risks / blockers |
| --- | --- | --- | --- | --- |
| 2026-10-10 | G0 source audit | `docs/P8_BASELINE_AUDIT.md`, `docs/P8_OAUTH_ACCEPTANCE.md` | Baseline commands above; source and SDK audit | Production reverse-proxy config unavailable; Grok OAuth flow unknown; real Codex/Cursor/Claude OAuth not tested. |
| 2026-10-10 | Local implementation evidence across gates (gate results are individually scoped above) | OAuth server/store/API, Vue MCP management, docs and generated web bundle (see branch diff) | `go test -race ./...` PASS; `go vet ./...` PASS; `git diff --check` PASS; frontend Vitest 14/14 PASS; `npm run build` PASS; `make test` PASS; `go build -o /tmp/bolt-dock-p8-acceptance ./cmd/server` PASS; `docker build --tag bolt-dock:p8-local-acceptance .` PASS; isolated container `/healthz` 200, `/mcp` 401 with metadata challenge, protected-resource metadata correct; cleanup completed. Store tests include expired code/token, closed-file isolated backup/restore. | No external product account or production access. Dockerfile targets linux/arm64; this smoke is not a multi-architecture release test. G4 remains blocked for external production/client evidence. |
| 2026-10-10 | Review fixes and expanded security coverage | `internal/api/oauth.go`, OAuth tests, P8 plan/acceptance/migration docs | Token and revoke endpoints now use peer-IP ceilings plus per-client buckets (independent of source port); token handler test verifies 429 at 61 requests and isolation between two registered clients. Authorization redirects set `Cache-Control: no-store` and `Pragma: no-cache`; tests cover host/forwarded-header spoof resistance, invalid client/authorize and token redirect, empty/plain/wrong PKCE, cross-origin/CSRF/deny, expired code/token, read-only cannot publish, write-only can publish but cannot search, resource mismatch, code replay and immediate revoke. | Peer IP is the socket peer; production proxy topology is unknown, so deployments must preserve a stable trusted peer boundary or configure a reviewed proxy design before enabling OAuth. |

## Client compatibility evidence

| Client | Protocol path | Live product test | Status |
| --- | --- | --- | --- |
| Grok | Pending | Requires identifying exact product surface and account access | **BLOCKED / unknown pending external verification** |
| Codex | P7 Bearer baseline documented | P8 OAuth not run | **OAuth unverified** |
| Cursor | Pending | Requires exact product/version and account | **Unverified** |
| Claude | Pending | Requires account and OAuth consent | **BLOCKED pending external verification** |
| MCP SDK independent local test client | MCP 2025-11-25 initialize, tools/list, search, denied publish, revoke | Automated isolated SQLite test, no real third-party account | **PASS (local protocol only)** |

## Explicitly not accepted as complete

- Production TLS, reverse proxy, `PUBLIC_BASE_URL`, and public OAuth enablement.
- Live Grok or Claude authorization, client-specific redirect URI/credential instructions, or background scheduling.
- Multi-user or per-source data isolation; P8 remains a single-owner system.
- Push, PR creation/merge, or production deployment. The plan requires new explicit authorization for those remote actions.

## Current delivery boundary

- No commit has been pushed, no PR has been created, and no remote merge/deploy was attempted. The plan explicitly requires separate authorization for these remote actions.
- This report accepts local implementation and isolation evidence only. It does not accept live Grok/Claude/Cursor OAuth, public reverse-proxy behavior, production restore, or GHCR publication.
- Local browser check: owner login, OAuth client registration/listing, narrow mobile viewport and dark-theme interaction; browser console reported zero errors/warnings. Keyboard-only consent accessibility and real mobile device behavior remain unverified.
- The detailed execution plan intentionally leaves unverified threat-matrix items unchecked, including hostile Host/forwarded-header deployment tests, full redirect URL encoding/trailing-slash matrix, session-fixation and revoke/call race tests, full anonymous/Cookie permission matrix, legacy Codex CLI live regression, and production/container restart/resource measurements. Refresh rotation and CIMD/DCR tests are not applicable because those features are not implemented.
- Local API tests do verify the app ignores a hostile Host and `X-Forwarded-*` on MCP challenges and AS metadata; this does not verify a real reverse proxy's forwarding, TLS, or path-prefix configuration.
