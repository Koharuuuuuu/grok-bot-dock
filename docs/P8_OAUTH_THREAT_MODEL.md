# P8 OAuth threat model

Last reviewed: 2026-10-10
Scope: the single-owner OAuth authorization server and protected MCP endpoint in Bolt Dock. This is a code-and-local-test review; it does not attest to the production reverse proxy, public deployment, or third-party clients.

## Assets and trust boundaries

- The administrator's `ACCESS_TOKEN` and browser session authenticate owner operations. OAuth client secrets are separate credentials.
- OAuth access tokens grant only the consented report scopes for the configured MCP resource. They are opaque, high-entropy values; the database stores SHA-256 digests.
- OAuth is disabled unless explicitly configured. When enabled, issuer and resource derive from the configured `PUBLIC_BASE_URL`, not request `Host` or forwarded headers.
- Browser cookies authenticate owner UI/API calls; they are not accepted as MCP bearer credentials. OAuth bearer credentials are not accepted by REST management APIs.
- Manual client registration is owner-controlled. CIMD public clients are fetched and validated before consent; DCR remains disabled. CIMD URLs are untrusted input and cross the network-fetch boundary described below.

## Threat review

| Threat | Controls in code | Local evidence / remaining boundary |
|---|---|---|
| Authorization-code injection or replay | Client, exact redirect URI, resource, PKCE challenge, expiry, and single-use code are bound in SQLite; only S256 is accepted. | OAuth API tests cover invalid client/redirect, empty/plain/wrong PKCE, expiry and replay; store tests race concurrent exchanges. |
| CSRF, consent confusion, session fixation | Owner session required; consent uses one-time CSRF state and checks Origin; session cookie has HttpOnly/SameSite and login rotates the session identifier. Consent renders only the scopes requested by this authorization request, all unchecked. | Local tests cover cross-origin and bad-CSRF consent, session fixation, and requested-scope-only rendering. Production browser/proxy behavior still needs deployment verification. |
| Scope escalation | Consent records only approved known scopes; MCP tool handlers enforce `reports:read` and `reports:write` at call time. REST endpoints do not accept OAuth tokens. | `TestOAuthPrincipalPermissionMatrixAcrossMCPTools` covers anonymous, browser Cookie, invalid Bearer, legacy Bearer, OAuth read, OAuth write, and read+write principals against MCP tools; REST rejection tests exist. |
| Resource/audience confusion and issuer mix-up | Token is bound to the configured `/mcp` resource; discovery uses configured issuer; authorization response includes the issuer parameter. | Wrong-resource, spoofed Host/forwarded metadata, and discovery tests exist. Production proxy path and TLS handling remain unverified. |
| Redirect manipulation / open redirect | Client redirect must be pre-registered and exactly equal; HTTPS required except loopback; credentials and fragments are rejected. Error redirect is only used after the exact registered URI check. | URI validation and exact encoding/trailing-slash tests were added locally. Third-party callback behavior is unverified. |
| Client-secret disclosure | Secret is returned once; only its digest is stored; public clients have no secret; owner can rotate confidential-client secret. Registration and rotation responses set `Cache-Control: no-store`. | Store/API tests cover digest storage, public-client rejection, one-time rotation response, and no-store headers. A local scan found no test credentials in the static bundle; the app request logger records `URL.Path`, not the query string. CI artifacts and production logs still need the planned scan. |
| Brute force / resource exhaustion | OAuth authorization and credential endpoints have per-peer/per-client rate limits; request bodies are bounded where parsed. | Rate-limit tests cover peer-port aggregation and client isolation. Proxy peer identity in production must remain stable and trusted. |
| SSRF through client metadata | CIMD fetch requires HTTPS; disables proxies and redirects; resolves and rejects non-public/special-use IPs, pins the vetted address for the connection, bounds response size/time/concurrency, validates JSON and exact client_id, and checks redirect URIs. | `TestCIMDClientIDValidationAndPublicIP`, `TestCIMDMetadataValidationAndFetchLimits`, and end-to-end consent/token tests pass locally. DNS rebinding, redirect, private/special-use address, invalid metadata and size cases have deterministic tests. CIMD remains an evolving Internet-Draft; independent conformance review is still needed. |
| Revocation race with in-flight tool call | Token/grant revocation is checked during token authentication and client disable revokes related grants/tokens. | Immediate revoke is tested between calls. A call already executing concurrently with revocation is not cancelled; race semantics need explicit test/decision. |

## Residual acceptance work

The local migrations preserve Bot/report/FTS counts and external-ID idempotency, but production backup/restore remains unverified. Store-level token lookup/revoke concurrency is tested; cancellation of an MCP call already in flight is not implemented or verified. Token/secret CI-artifact/log scans, keyboard/mobile checks, and real Grok/Claude/Codex client verification remain. Reverse-proxy trust and HTTPS must be verified in the intended deployment environment. None of those external checks are claimed complete by this document.
