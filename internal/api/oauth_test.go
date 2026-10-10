package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Nyakooo/grok-bot-dock/internal/events"
	"github.com/Nyakooo/grok-bot-dock/internal/store"
	"github.com/Nyakooo/grok-bot-dock/internal/webui"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestValidatePublicBaseURL(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"https://dock.example", true}, {"http://localhost:18765", true}, {"http://127.0.0.1:18765", true},
		{"http://dock.example", false}, {"https://dock.example/path", false}, {"https://user@dock.example", false}, {"https://dock.example?issuer=x", false}, {"", false},
	} {
		if got := ValidatePublicBaseURL(tc.value); got != tc.want {
			t.Errorf("ValidatePublicBaseURL(%q)=%v want %v", tc.value, got, tc.want)
		}
	}
}

func TestOAuthRedirectIsNotCachedAndRateLimitUsesPeerIP(t *testing.T) {
	w := httptest.NewRecorder()
	redirectOAuth(w, "https://client.example/callback", "state", map[string]string{"code": "one-time"})
	if w.Code != http.StatusFound || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("authorization redirect headers/status: %d %#v", w.Code, w.Header())
	}
	first := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	first.RemoteAddr = "192.0.2.10:50001"
	second := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	second.RemoteAddr = "192.0.2.10:50002"
	if a, b := oauthRateKey(first, "client-a"), oauthRateKey(second, "client-a"); a != b {
		t.Fatalf("peer port unexpectedly changes rate bucket: %q != %q", a, b)
	}
	if a, b := oauthRateKey(first, "client-a"), oauthRateKey(second, "client-b"); a == b {
		t.Fatalf("distinct clients share rate bucket: %q", a)
	}
}

func TestOAuthAuthorizationRejectsUnknownClientRedirectAndPKCE(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/oauth-invalid.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const base = "https://oauth.example"
	h := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: base}).Handler()
	if err = db.RegisterOAuthClient(context.Background(), "known-client", "Known", []string{"https://client.example/callback"}, ""); err != nil {
		t.Fatal(err)
	}
	validChallenge := base64.RawURLEncoding.EncodeToString(make([]byte, sha256.Size))
	baseQuery := url.Values{"response_type": {"code"}, "client_id": {"known-client"}, "redirect_uri": {"https://client.example/callback"}, "code_challenge_method": {"S256"}, "code_challenge": {validChallenge}, "resource": {base + "/mcp"}, "scope": {oauthReadScope}, "state": {"test-state"}}
	cases := []struct {
		name       string
		mutate     func(url.Values)
		wantStatus int
		wantError  string
	}{
		{"unknown client", func(q url.Values) { q.Set("client_id", "missing") }, http.StatusBadRequest, ""},
		{"unregistered redirect", func(q url.Values) { q.Set("redirect_uri", "https://attacker.example/callback") }, http.StatusBadRequest, ""},
		{"plain PKCE", func(q url.Values) { q.Set("code_challenge_method", "plain") }, http.StatusFound, "invalid_request"},
		{"empty PKCE", func(q url.Values) { q.Set("code_challenge", "") }, http.StatusFound, "invalid_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			query := url.Values{}
			for k, values := range baseQuery {
				query[k] = append([]string(nil), values...)
			}
			tc.mutate(query)
			req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantError != "" {
				location, err := url.Parse(w.Header().Get("Location"))
				if err != nil || location.Query().Get("error") != tc.wantError {
					t.Fatalf("redirect error=%v location=%q", err, w.Header().Get("Location"))
				}
			}
		})
	}
}

func TestOAuthTokenHandlerRateLimitsByPeerAndClient(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/oauth-rate.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: "https://oauth.example"}).Handler()
	for _, id := range []string{"client-a", "client-b"} {
		if err = db.RegisterOAuthClient(context.Background(), id, id, []string{"https://client.example/callback"}, ""); err != nil {
			t.Fatal(err)
		}
	}
	post := func(clientID string, port int) *httptest.ResponseRecorder {
		form := url.Values{"client_id": {clientID}, "grant_type": {"unsupported"}}
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "192.0.2.10:" + strconv.Itoa(port)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	for i := 0; i < 60; i++ {
		if got := post("client-a", 50000+i); got.Code != http.StatusBadRequest {
			t.Fatalf("client-a request %d was unexpectedly rate limited: %d", i+1, got.Code)
		}
	}
	if got := post("client-a", 60000); got.Code != http.StatusTooManyRequests {
		t.Fatalf("61st client-a request status=%d want=429 body=%s", got.Code, got.Body.String())
	}
	if got := post("client-b", 60001); got.Code != http.StatusBadRequest {
		t.Fatalf("different registered client shared client-a bucket: %d %s", got.Code, got.Body.String())
	}
}

func TestOAuthDiscoveryPKCEScopesAndRevocation(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/oauth.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const base = "https://oauth.example"
	server := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: base})
	h := server.Handler()
	remote := httptest.NewServer(h)
	t.Cleanup(remote.Close)
	challengeReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	challengeReq.Host = "attacker.example"
	challengeReq.Header.Set("X-Forwarded-Host", "attacker.example")
	challengeReq.Header.Set("X-Forwarded-Proto", "http")
	challengeW := httptest.NewRecorder()
	h.ServeHTTP(challengeW, challengeReq)
	if challengeW.Code != http.StatusUnauthorized || !strings.Contains(challengeW.Header().Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource/mcp") {
		t.Fatalf("missing OAuth resource discovery challenge: status=%d header=%q", challengeW.Code, challengeW.Header().Get("WWW-Authenticate"))
	}
	if !strings.Contains(challengeW.Header().Get("WWW-Authenticate"), "https://oauth.example/.well-known/oauth-protected-resource/mcp") {
		t.Fatalf("challenge trusted attacker-controlled Host/forwarded headers: %q", challengeW.Header().Get("WWW-Authenticate"))
	}
	if err := db.RegisterOAuthClient(context.Background(), "client-test", "Test Client", []string{"http://127.0.0.1/callback"}, ""); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"/.well-known/oauth-protected-resource/mcp": "\"resource\":\"https://oauth.example/mcp\"", "/.well-known/oauth-authorization-server": "\"code_challenge_methods_supported\":[\"S256\"]"} {
		resp, err := http.Get(remote.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(raw), want) {
			t.Fatalf("discovery %s: status=%d body=%s", path, resp.StatusCode, raw)
		}
	}
	spoofedMetadata, err := http.NewRequest(http.MethodGet, remote.URL+"/.well-known/oauth-authorization-server", nil)
	if err != nil {
		t.Fatal(err)
	}
	spoofedMetadata.Host = "attacker.example"
	spoofedMetadata.Header.Set("X-Forwarded-Host", "attacker.example")
	spoofedMetadata.Header.Set("X-Forwarded-Proto", "http")
	spoofedResponse, err := http.DefaultClient.Do(spoofedMetadata)
	if err != nil {
		t.Fatal(err)
	}
	spoofedBody, _ := io.ReadAll(spoofedResponse.Body)
	spoofedResponse.Body.Close()
	if spoofedResponse.StatusCode != http.StatusOK || !strings.Contains(string(spoofedBody), `"issuer":"https://oauth.example"`) {
		t.Fatalf("metadata trusted attacker-controlled Host/forwarded headers: status=%d body=%s", spoofedResponse.StatusCode, spoofedBody)
	}
	login := request(h, http.MethodPost, "/api/v1/auth/login", `{"token":"owner-secret"}`, "")
	if login.Code != 200 {
		t.Fatalf("owner login: %d %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	redirect := "http://127.0.0.1/callback"
	query := url.Values{"response_type": {"code"}, "client_id": {"client-test"}, "redirect_uri": {redirect}, "code_challenge_method": {"S256"}, "code_challenge": {challenge}, "resource": {base + "/mcp"}, "scope": {oauthReadScope}, "state": {"s-test"}}
	get := httptest.NewRequest("GET", "/oauth/authorize?"+query.Encode(), nil)
	get.AddCookie(cookie)
	page := httptest.NewRecorder()
	h.ServeHTTP(page, get)
	if page.Code != 200 {
		t.Fatalf("authorize page: %d %s", page.Code, page.Body.String())
	}
	csrf := extractOAuthCSRF(t, page.Body.String())
	badOrigin := httptest.NewRequest("POST", "/oauth/consent", strings.NewReader(url.Values{"csrf": {csrf}, "decision": {"approve"}, "scope": {oauthReadScope}}.Encode()))
	badOrigin.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badOrigin.Header.Set("Origin", "https://attacker.example")
	badOrigin.AddCookie(cookie)
	badOriginW := httptest.NewRecorder()
	h.ServeHTTP(badOriginW, badOrigin)
	if badOriginW.Code != http.StatusForbidden {
		t.Fatalf("cross-origin consent accepted: status=%d", badOriginW.Code)
	}
	badCSRF := httptest.NewRequest("POST", "/oauth/consent", strings.NewReader(url.Values{"csrf": {"wrong"}, "decision": {"approve"}, "scope": {oauthReadScope}}.Encode()))
	badCSRF.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badCSRF.Header.Set("Origin", base)
	badCSRF.AddCookie(cookie)
	badCSRFW := httptest.NewRecorder()
	h.ServeHTTP(badCSRFW, badCSRF)
	if badCSRFW.Code != http.StatusBadRequest {
		t.Fatalf("invalid CSRF token accepted: status=%d", badCSRFW.Code)
	}
	consent := httptest.NewRequest("POST", "/oauth/consent", strings.NewReader(url.Values{"csrf": {csrf}, "decision": {"approve"}, "scope": {oauthReadScope}}.Encode()))
	consent.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	consent.Header.Set("Origin", base)
	consent.AddCookie(cookie)
	consentW := httptest.NewRecorder()
	h.ServeHTTP(consentW, consent)
	if consentW.Code != http.StatusFound {
		t.Fatalf("consent: %d %s", consentW.Code, consentW.Body.String())
	}
	cb, err := url.Parse(consentW.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := cb.Query().Get("code")
	if code == "" || cb.Query().Get("state") != "s-test" || cb.Query().Get("iss") != base {
		t.Fatalf("authorization response: %s", consentW.Header().Get("Location"))
	}
	exchange := func(v, tokenResource, tokenRedirect string) *httptest.ResponseRecorder {
		form := url.Values{"grant_type": {"authorization_code"}, "client_id": {"client-test"}, "code": {code}, "redirect_uri": {tokenRedirect}, "resource": {tokenResource}, "code_verifier": {v}}
		req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	bad := exchange(strings.Repeat("x", 43), base+"/mcp", redirect)
	if bad.Code != 400 {
		t.Fatalf("wrong PKCE verifier status=%d body=%s", bad.Code, bad.Body.String())
	}
	badResource := exchange(verifier, "https://wrong.example/mcp", redirect)
	if badResource.Code != 400 {
		t.Fatalf("wrong resource status=%d", badResource.Code)
	}
	badRedirect := exchange(verifier, base+"/mcp", "http://127.0.0.1/attacker")
	if badRedirect.Code != http.StatusBadRequest {
		t.Fatalf("wrong redirect_uri status=%d body=%s", badRedirect.Code, badRedirect.Body.String())
	}
	ok := exchange(verifier, base+"/mcp", redirect)
	if ok.Code != 200 {
		t.Fatalf("token exchange: %d %s", ok.Code, ok.Body.String())
	}
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
	}
	if err := json.Unmarshal(ok.Body.Bytes(), &tokenResponse); err != nil || tokenResponse.AccessToken == "" || tokenResponse.Scope != oauthReadScope {
		t.Fatalf("token response: %#v err=%v", tokenResponse, err)
	}
	replay := exchange(verifier, base+"/mcp", redirect)
	if replay.Code != 400 {
		t.Fatalf("authorization code replay status=%d", replay.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "oauth-test", Version: "1"}, nil)
	httpClient := &http.Client{Transport: bearerRoundTripper{next: http.DefaultTransport, token: tokenResponse.AccessToken}}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: remote.URL + "/mcp", HTTPClient: httpClient}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatalf("MCP OAuth initialize: %v", err)
	}
	defer session.Close()
	if _, err = session.ListTools(ctx, nil); err != nil {
		t.Fatalf("OAuth tools/list: %v", err)
	}
	_, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "publish_report", Arguments: map[string]any{"executed_at": "2026-10-10T10:00:00Z", "content": "not allowed"}})
	if err == nil {
		t.Fatal("reports:read token unexpectedly published a report")
	}
	if got := request(h, http.MethodGet, "/api/v1/reports", "", tokenResponse.AccessToken); got.Code != http.StatusUnauthorized {
		t.Fatalf("OAuth token reached REST API: %d", got.Code)
	}
	read, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "no match"}})
	if err != nil || read.IsError {
		t.Fatalf("read-only search failed: result=%#v err=%v", read, err)
	}
	revokeForm := url.Values{"client_id": {"client-test"}, "token": {tokenResponse.AccessToken}}
	revokeReq := httptest.NewRequest("POST", "/oauth/revoke", strings.NewReader(revokeForm.Encode()))
	revokeReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	revoked := httptest.NewRecorder()
	h.ServeHTTP(revoked, revokeReq)
	if revoked.Code != 200 {
		t.Fatalf("revoke: %d %s", revoked.Code, revoked.Body.String())
	}
	if _, err = session.ListTools(ctx, nil); err == nil {
		t.Fatal("revoked access token remained valid")
	}
	query.Set("state", "denied-state")
	deniedAuthorize := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
	deniedAuthorize.AddCookie(cookie)
	deniedPage := httptest.NewRecorder()
	h.ServeHTTP(deniedPage, deniedAuthorize)
	if deniedPage.Code != http.StatusOK {
		t.Fatalf("denial authorize page: %d", deniedPage.Code)
	}
	deniedForm := url.Values{"csrf": {extractOAuthCSRF(t, deniedPage.Body.String())}, "decision": {"deny"}}
	deniedReq := httptest.NewRequest(http.MethodPost, "/oauth/consent", strings.NewReader(deniedForm.Encode()))
	deniedReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deniedReq.Header.Set("Origin", base)
	deniedReq.AddCookie(cookie)
	deniedW := httptest.NewRecorder()
	h.ServeHTTP(deniedW, deniedReq)
	deniedURL, err := url.Parse(deniedW.Header().Get("Location"))
	if err != nil || deniedW.Code != http.StatusFound || deniedURL.Query().Get("error") != "access_denied" {
		t.Fatalf("consent denial did not return access_denied: status=%d location=%q err=%v", deniedW.Code, deniedW.Header().Get("Location"), err)
	}
}

func TestOAuthWriteOnlyScopeCanPublishButCannotSearch(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/oauth-write.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const base = "https://oauth.example"
	h := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: base}).Handler()
	remote := httptest.NewServer(h)
	t.Cleanup(remote.Close)
	if err = db.RegisterOAuthClient(context.Background(), "write-client", "Write Client", []string{"http://127.0.0.1/callback"}, ""); err != nil {
		t.Fatal(err)
	}
	login := request(h, http.MethodPost, "/api/v1/auth/login", `{"token":"owner-secret"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("owner login: %d %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	verifier := strings.Repeat("w", 43)
	sum := sha256.Sum256([]byte(verifier))
	redirect := "http://127.0.0.1/callback"
	query := url.Values{"response_type": {"code"}, "client_id": {"write-client"}, "redirect_uri": {redirect}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "resource": {base + "/mcp"}, "scope": {oauthWriteScope}, "state": {"write-state"}}
	authorize := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
	authorize.AddCookie(cookie)
	page := httptest.NewRecorder()
	h.ServeHTTP(page, authorize)
	if page.Code != http.StatusOK {
		t.Fatalf("authorize page: %d %s", page.Code, page.Body.String())
	}
	consentForm := url.Values{"csrf": {extractOAuthCSRF(t, page.Body.String())}, "decision": {"approve"}, "scope": {oauthWriteScope}}
	consent := httptest.NewRequest(http.MethodPost, "/oauth/consent", strings.NewReader(consentForm.Encode()))
	consent.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	consent.Header.Set("Origin", base)
	consent.AddCookie(cookie)
	consentW := httptest.NewRecorder()
	h.ServeHTTP(consentW, consent)
	callback, err := url.Parse(consentW.Header().Get("Location"))
	if err != nil || consentW.Code != http.StatusFound {
		t.Fatalf("consent: status=%d location=%q err=%v", consentW.Code, consentW.Header().Get("Location"), err)
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {"write-client"}, "code": {callback.Query().Get("code")}, "redirect_uri": {redirect}, "resource": {base + "/mcp"}, "code_verifier": {verifier}}
	tokenReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenW := httptest.NewRecorder()
	h.ServeHTTP(tokenW, tokenReq)
	var result struct {
		AccessToken string `json:"access_token"`
	}
	if tokenW.Code != http.StatusOK || json.Unmarshal(tokenW.Body.Bytes(), &result) != nil || result.AccessToken == "" {
		t.Fatalf("token exchange: status=%d body=%s", tokenW.Code, tokenW.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "oauth-write-test", Version: "1"}, nil)
	httpClient := &http.Client{Transport: bearerRoundTripper{next: http.DefaultTransport, token: result.AccessToken}}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: remote.URL + "/mcp", HTTPClient: httpClient}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatalf("MCP OAuth initialize: %v", err)
	}
	defer session.Close()
	published, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "publish_report", Arguments: map[string]any{"source": "acceptance", "bot_id": "oauth-write-test", "executed_at": "2026-10-10T10:00:00Z", "content": "write-scope acceptance report"}})
	if err != nil || published.IsError {
		t.Fatalf("reports:write could not publish: result=%#v err=%v", published, err)
	}
	searched, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "write-scope acceptance report"}})
	if err == nil && !searched.IsError {
		t.Fatalf("reports:write unexpectedly searched reports: result=%#v err=%v", searched, err)
	}
}

func extractOAuthCSRF(t *testing.T, html string) string {
	t.Helper()
	const marker = `name="csrf" value="`
	start := strings.Index(html, marker)
	if start < 0 {
		t.Fatalf("CSRF input absent from authorization page: %s", html)
	}
	start += len(marker)
	end := strings.IndexByte(html[start:], '"')
	if end < 0 {
		t.Fatalf("malformed CSRF input: %s", html)
	}
	return html[start : start+end]
}
