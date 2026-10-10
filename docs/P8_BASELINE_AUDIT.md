# P8 baseline audit

Audit date: 2026-10-10
Base commit: `4023848d7878bc94038eaf2588bc410c51364dad`
Branch: `feature/p8-mcp-oauth`

## G0 status

**Complete for the local source tree.** The audit did not access or change production, a deployed database, or third-party accounts. Production proxy configuration and real Grok/Claude client behavior remain externally unverifiable from this checkout.

## Existing request and identity paths

```text
ACCESS_TOKEN ── POST /api/v1/auth/login ──> in-memory 12h HttpOnly session
     ├── /api/*: legacy Bearer OR session cookie
     ├── POST /api/v1/reports: existing shared Bearer API
     └── /mcp: existing shared Bearer only
                          ├── publish_report ──> normalizeReportRequest ──> Store.CreateReport
                          └── search_reports ──> Store.SearchReports
```

- `internal/api/server.go`: `/api/` auth accepts the configured legacy token or a valid in-memory session. State-changing cookie-authenticated API requests require `sameOrigin`; logout invalidates the in-memory session and its SSE streams. Login rate limiting is in process and keyed by remote address.
- `requestIsHTTPS` trusts the first `X-Forwarded-Proto` value without validating the connecting proxy. That value controls the session cookie `Secure` flag and `sameOrigin` scheme comparison. Do not use request `Host` or forwarded headers to form OAuth issuer/resource URLs. Configure a fixed public base URL and require deployment proxy review before enabling OAuth publicly.
- `/mcp` is a stateless Go SDK Streamable HTTP handler. `mcpAuth` checks `Origin` and compares a shared Bearer token in constant time. Cookies are not an accepted MCP credential. Existing tools call the same report normalization and SQLite store paths as the REST API.
- There is no conventional user account, password database, or `user_id`. The only owner identity available for P8 is the single operator authenticated by `ACCESS_TOKEN`; model the subject as a fixed internal `owner`, and disclose that read scope spans the entire report library.
- Browser UI is a Vue single page app served by `internal/webui`; it contains the existing MCP access modal and authenticated report workspace. The MCP capability endpoint currently advertises only shared Bearer.

## Storage and service limits

- `internal/store/store.go` opens SQLite with WAL, `busy_timeout=5000`, foreign keys enabled, and one open connection. Existing logical schema is `schema_migrations` v1 plus `sources`, `bots`, `reports`, FTS5 `report_fts`, `report_fts_map`, and report FTS triggers. Existing code conditionally adds `bots.display_name` outside the recorded migration version.
- Reports reference bots with `ON DELETE CASCADE`; unique `(source, bot_id)` and partial `(bot_pk, external_id)` index support current identity and idempotency. OAuth changes must be additive and must not alter those tables or FTS behavior.
- The main process requires `ACCESS_TOKEN`, opens `/data/bolt-dock.db` by default, binds `0.0.0.0`, and exposes one port. `Dockerfile` uses one Alpine runtime container and a persistent `/data` volume. `docker-compose.yml` and `compose.local.yml` are the checked-in deployment examples; they do not establish the actual production proxy or public hostname.
- No backup/restore of a real production DB was performed. Migration and restart checks must use temporary local databases.

## SDK and protocol findings

### Protocol version re-check (2026-10-10)

The baseline below records the version reviewed at the start of P8. The current stable MCP authorization specification is now `2026-07-28`; current OAuth 2.1 is IETF draft-16 and remains a work in progress. Implementation review added coverage for OAuth 2.1's omitted `redirect_uri` cases and unique-parameter/client-authentication rules. P7 shared `ACCESS_TOKEN` acceptance on `/mcp` is intentionally retained for compatibility; because the MCP authorization profile requires resource-specific OAuth access tokens, this is an explicit exception and full MCP authorization conformance is not claimed. See the [acceptance record](./P8_OAUTH_ACCEPTANCE.md) for test evidence and limits.

- P7 pins `github.com/modelcontextprotocol/go-sdk v1.8.0`. Its `auth` package provides bearer-token resource-server middleware and Protected Resource Metadata helpers; `oauthex` provides metadata/client registration types. The SDK does not expose a complete local authorization-server implementation with owner consent, durable grant/code/token storage, revocation, and lifecycle management. P8 therefore needs a small application-owned AS around SQLite, while continuing to use the SDK MCP transport/tools.
- At the original audit date, the stable MCP authorization profile was `2025-11-25`; this historical baseline required OAuth-based discovery, a protected-resource metadata pointer, resource indicators on authorization and token requests, and exact resource binding. The current `2026-07-28` baseline is recorded above and linked below. RFC 9728 defines resource metadata, RFC 8414 AS metadata, RFC 7636 PKCE, RFC 8707 resource indicators, and RFC 7009 revocation.
- OAuth 2.1 remains an IETF Internet-Draft as of this audit, not a published RFC. The code should describe its actual security profile (Authorization Code + PKCE S256, no implicit/password grants) without claiming a published OAuth 2.1 RFC.
- The newer MCP draft deprecates open Dynamic Client Registration in favor of Client ID Metadata Documents and includes RFC 9207 authorization-response issuer mix-up defenses. P8's stable-profile implementation should not enable arbitrary registration or fetch arbitrary client metadata URLs without a separate SSRF/metadata review. Use owner-controlled pre-registration first.

## Baseline commands

Executed before P8 edits on the base commit:

| Command | Result |
| --- | --- |
| `go test ./...` | PASS |
| `cd frontend && npm ci && npm test && npm run build` | PASS; Vitest 13/13, Vite production build successful |

These are code baseline results only; they are not production or client-compatibility evidence.

## Client evidence known at audit time

| Client | Evidence | Status |
| --- | --- | --- |
| Grok | The initial search did not find product documentation; this was incomplete. The dated official-doc re-check below confirms custom MCP and OAuth capabilities but not registration/callback/resource details. | **Capability documented; live interoperability requires account test** |
| Codex | P7 recorded a specific Bearer-mode integration. No P8 OAuth integration has been performed in this audit. | Bearer baseline only; OAuth unverified |
| Cursor | Official docs describe remote MCP and OAuth in some product surfaces; exact desktop/version configuration and this server's OAuth behavior have not been tested. | Unverified |
| Claude | Official Anthropic documentation describes remote MCP integrations and OAuth, but no account login or live server authorization was performed. | Unverified |
| MCP Inspector | No P8 OAuth end-to-end has yet been run. | Pending |

### Grok documentation re-check (2026-10-10)

The historical finding above has been superseded for basic product capability: current official [Grok Connectors documentation](https://docs.x.ai/grok/connectors) documents a custom MCP connector with a public server URL and required authentication; [custom MCP tunneling guidance](https://docs.x.ai/grok/connectors/custom-mcp-tunneling) explicitly mentions OAuth or API-key authentication; [Grok CLI MCP documentation](https://docs.x.ai/build/features/mcp-servers) documents remote HTTP servers and automatic OAuth browser flow. These sources establish that both Grok.com Custom Connector and Grok CLI have relevant MCP/OAuth capability. They do not specify the exact client registration, redirect URI, resource/scope behavior, or prove interoperability with Bolt Dock. Those points still require a real versioned client flow and account.

## Required deployment decisions before public enablement

1. Set `PUBLIC_BASE_URL` to the single canonical HTTPS origin for the service. Derive issuer/resource/metadata from this configuration only, never request headers. Reject OAuth startup/enablement if absent or invalid; only permit HTTP loopback for isolated local tests.
2. Confirm the real reverse proxy forwards `/.well-known/*`, `/oauth/*`, and `/mcp` without rewriting issuer/resource paths or stripping `WWW-Authenticate`. The checkout cannot prove this configuration.
3. Review trusted-proxy behavior separately. The current app's forwarded-proto trust is broad; P8 must not widen that trust or use it for OAuth discovery.
4. Keep OAuth disabled by default. No production setting or database was touched for this implementation.

## References

- [MCP Authorization, current 2026-07-28](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)
- [RFC 9728: OAuth 2.0 Protected Resource Metadata](https://www.rfc-editor.org/rfc/rfc9728)
- [RFC 8414: OAuth 2.0 Authorization Server Metadata](https://www.rfc-editor.org/rfc/rfc8414)
- [RFC 7636: PKCE](https://www.rfc-editor.org/rfc/rfc7636)
- [RFC 8707: Resource Indicators](https://www.rfc-editor.org/rfc/rfc8707)
- [RFC 7009: Token Revocation](https://www.rfc-editor.org/rfc/rfc7009)
- [OAuth 2.1 current Internet-Draft](https://datatracker.ietf.org/doc/draft-ietf-oauth-v2-1/)
- [MCP authorization draft](https://modelcontextprotocol.io/specification/draft/basic/authorization)
- [Go MCP SDK v1.8.0 auth package](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk@v1.8.0/auth)
- [Go MCP SDK v1.8.0 oauthex package](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk@v1.8.0/oauthex)
- [Cursor MCP documentation](https://docs.cursor.com/context/model-context-protocol)
- [Anthropic remote MCP integrations](https://support.anthropic.com/en/articles/11503834-building-custom-integrations-via-remote-mcp-servers)
