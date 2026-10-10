# P8 OAuth acceptance record

Updated: 2026-10-10
Base SHA: `4023848d7878bc94038eaf2588bc410c51364dad`
Working branch: `feature/p8-mcp-oauth`
Local implementation commit: `cb48a70d8bb4f3254dc3c96963812b87e2b3afe5`

Latest `origin/master` check: `4023848d7878bc94038eaf2588bc410c51364dad` (2026-10-10); branch is based on current remote master.

## OAuth 2.1 scope statement

**This is not a claim of complete OAuth 2.1 implementation or full specification conformance.** As of 2026-10-10, the IETF Datatracker lists `draft-ietf-oauth-v2-1-16` as an Active Internet-Draft with no intended RFC status set ([current Datatracker record](https://datatracker.ietf.org/doc/draft-ietf-oauth-v2-1/)). Bolt Dock implements a scoped Authorization Code + PKCE S256 authorization profile and the MCP discovery/resource-binding requirements listed in the P8 plan. P8 deliberately omits refresh tokens, CIMD, and DCR; third-party OAuth interoperability and the remaining security matrix are still unverified. The service must therefore be described as implementing this limited OAuth profile, not “fully OAuth 2.1 compliant.”

## Gate status

| Gate | Status | Evidence / remaining work |
| --- | --- | --- |
| G0 Source audit and frozen baseline | **PASS** | [`P8_BASELINE_AUDIT.md`](./P8_BASELINE_AUDIT.md); source tree audited, base tests passed, no production access. |
| G1 Protocol and compatibility contract | **PASS (local core)** | MCP discovery endpoints, configured issuer/resource, exact registered callback matching, PKCE S256 and owner-pre-registered client flow are tested. Redirect encoding, trailing slash, HTTPS/loopback, and base URL path/query/fragment cases now have a local edge matrix; real client-specific configuration remains unverified. |
| G2 Auth core | **PASS (isolated core)** | Wrong/empty/plain PKCE, invalid client/redirect/resource, one-time/concurrent code exchange, expired-token rejection, read/write scope enforcement, immediate revoke, client disable and SQLite restart/backup-restore are covered by local tests. Production issuer/host/proxy configuration and revoke-versus-in-flight-call behavior remain open. |
| G3 API/UI integration | **PASS (local core)** | Legacy Bearer/REST regression and OAuth-only MCP access are covered; consent/CSRF/origin/deny and owner client/grant/secret-rotation flows tested; rotation UI passes Vitest/build. Prior local browser login/client registration/theme smoke had zero console errors. Rotation UI browser interaction and keyboard-only/real mobile device checks remain unverified. |
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
| 2026-10-10 | Redirect URI edge coverage, secret rotation, legacy migration preservation and REST auth matrix | `internal/api/oauth.go`, `internal/api/oauth_test.go`, `internal/store/oauth.go`, `internal/store/oauth_test.go`, `frontend/src/App.vue`, `docs/P8_OAUTH_CLIENTS.md`, `docs/P8_OAUTH_THREAT_MODEL.md` | `go test ./...` PASS; `go vet ./...` PASS; `go test -race ./internal/api ./internal/store` PASS; Vitest 14/14 PASS; Vite production build PASS. Added tests for redirect URL exact matching/encoding/trailing slash/HTTPS/loopback; owner-only secret rotation with digest-only persistence and public/disabled/missing client rejection; v1→OAuth migration preserving bot/report/FTS counts, external_id idempotency and unique index; REST authorization behavior for anonymous, browser cookie, legacy Bearer and OAuth Bearer. | New rotation UI compiles and unit suite passes; its browser interaction and keyboard/mobile checks remain unverified. Existing access tokens retain original expiry/revocation semantics when client secret is rotated. |
| 2026-10-10 | Build and isolated container smoke for secret-rotation build | `internal/webui/dist/*`, Docker image `bolt-dock:p8-oauth-rotation` | `make test` PASS; `make build` PASS; Docker arm64 image build PASS; isolated container `HEALTHCHECK healthy`, `/healthz` 200, `/mcp` returns 401 with configured OAuth metadata challenge, Protected Resource Metadata matches configured issuer/resource. `docker stats --no-stream` observed 0.00% CPU and 7.195 MiB memory during idle sample. Container removed after smoke. | Resource sample is a short idle local Docker Desktop observation, not production sizing evidence. A later `make build` rerun overwrote the ignored local binary; it was reconstructed from P7 commit `17659428` and remains ignored/unstaged, but its original bytes could not be recovered. No remote image push. |
| 2026-10-10 | Local browser OAuth Secret rotation smoke | `frontend/src/App.vue`, OAuth owner API | Playwright CLI opened local OAuth-enabled instance, logged in, registered a confidential client, rotated its secret from the management list, and verified the one-time result appeared with the same client ID; console reported 0 errors and 0 warnings. Temporary DB/server/browser were closed and removed after the check. | Desktop Chromium local only. Keyboard-only consent, real mobile device, and clipboard-permission behavior remain unverified. |
| 2026-10-10 | Final requested-scope consent fix and verification | `internal/api/oauth.go`, `internal/api/oauth_test.go`, `docs/P8_OAUTH_THREAT_MODEL.md` | Read-only consent now renders only `reports:read`; write-only renders only `reports:write`, both remain unchecked by default. `make test` PASS (Go suite and Vitest 14/14); `go vet ./...` PASS; `go test -race ./...` PASS; `go build -o /tmp/bolt-dock-p8-verified-20261010 ./cmd/server` PASS; `npm run build` PASS; Docker ARM64 build `bolt-dock:p8-final-20261010` PASS. Isolated container reported `/healthz` 200 and `healthy`, `/mcp` 401 with configured metadata challenge, Protected Resource Metadata 200 with configured issuer/resource. Container removed after smoke. | Checks ran locally on 2026-10-10. Production proxy/TLS, third-party OAuth and actual device/browser accessibility remain unverified. Ignored local `bolt-dock` is a reconstruction from P7 commit `17659428`, not the overwritten original byte-for-byte. No remote image push or GitHub write was attempted. |

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
- The detailed execution plan intentionally leaves unverified threat-matrix items unchecked, including revoke/call race behavior, full anonymous/Cookie permission matrix across every MCP tool, legacy Codex CLI live regression, and production/container restart/resource measurements. Session-fixation regression and requested-scope-only consent rendering now have local unit coverage. The REST portion of the principal matrix and redirect URL encoding/trailing-slash behavior also have local unit coverage. Refresh rotation and CIMD/DCR tests are not applicable because those features are not implemented.
- Local API tests do verify the app ignores a hostile Host and `X-Forwarded-*` on MCP challenges and AS metadata; this does not verify a real reverse proxy's forwarding, TLS, or path-prefix configuration.
