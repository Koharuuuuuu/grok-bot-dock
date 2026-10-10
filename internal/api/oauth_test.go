package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
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

type cimdRoundTripper func(*http.Request) (*http.Response, error)

func (f cimdRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type oauthResponseRecorder struct {
	next   http.RoundTripper
	status int
	header http.Header
}

func (r *oauthResponseRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.next.RoundTrip(req)
	if err == nil {
		r.status = resp.StatusCode
		r.header = resp.Header.Clone()
	}
	return resp, err
}

func TestCIMDClientIDAndPublicAddressValidation(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"https://client.example/oauth/metadata.json", true},
		{"https://client.example:8443/client.json", true},
		{"https://client.example/", true},
		{"http://client.example/client.json", false},
		{"https://client.example", false},
		{"https://client.example/a/../client.json", false},
		{"https://user@client.example/client.json", false},
		{"https://client.example/client.json?token=secret", false},
		{"https://client.example/client.json#fragment", false},
		{" https://client.example/client.json", false},
		{strings.Repeat("a", 2049), false},
	} {
		if got := validCIMDClientID(tc.value); got != tc.want {
			t.Errorf("validCIMDClientID(%q)=%v want %v", tc.value, got, tc.want)
		}
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"8.8.8.8", true}, {"2606:4700:4700::1111", true},
		{"127.0.0.1", false}, {"10.0.0.1", false}, {"169.254.169.254", false},
		{"100.64.0.1", false}, {"192.0.2.10", false}, {"198.18.0.1", false},
		{"::ffff:127.0.0.1", false}, {"fd00::1", false}, {"fe80::1", false},
		{"2001:db8::1", false}, {"2002::1", false}, {"3fff::1", false},
	} {
		ip := netip.MustParseAddr(tc.value)
		if got := isPublicMetadataIP(ip); got != tc.want {
			t.Errorf("isPublicMetadataIP(%q)=%v want %v", tc.value, got, tc.want)
		}
	}
	called := false
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	dial := func(context.Context, string, string) (net.Conn, error) {
		called = true
		return nil, nil
	}
	if _, err := dialCIMDHost(context.Background(), "tcp", "client.example:443", lookup, dial); err == nil || called {
		t.Fatalf("mixed public/private DNS answer was not blocked before dialing: called=%v err=%v", called, err)
	}
	var dialAddress string
	lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	dial = func(_ context.Context, _, address string) (net.Conn, error) {
		dialAddress = address
		return nil, errors.New("captured dial target")
	}
	_, _ = dialCIMDHost(context.Background(), "tcp", "client.example:443", lookup, dial)
	if dialAddress != "8.8.8.8:443" {
		t.Fatalf("dial used unresolved hostname instead of checked IP: %q", dialAddress)
	}
}

func TestCIMDMetadataValidationAndFetchLimits(t *testing.T) {
	const clientID = "https://client.example/oauth/metadata.json"
	valid := `{"client_id":"https://client.example/oauth/metadata.json","client_name":"Example Agent","redirect_uris":["http://127.0.0.1:4747/callback"],"token_endpoint_auth_method":"none","grant_types":["authorization_code"],"response_types":["code"]}`
	response := func(status int, contentType, cacheControl, body string) *http.Response {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}, "Cache-Control": []string{cacheControl}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
	}
	client := func(resp *http.Response) *http.Client {
		return &http.Client{Transport: cimdRoundTripper(func(*http.Request) (*http.Response, error) { return resp, nil })}
	}
	doc, lifetime, err := fetchCIMDMetadataWithHTTPClient(context.Background(), clientID, client(response(200, "application/json", "public, max-age=600", valid)))
	if err != nil || doc.ClientName != "Example Agent" || len(doc.RedirectURIs) != 1 || lifetime != cimdMaxCacheLifetime {
		t.Fatalf("valid CIMD fetch doc=%#v lifetime=%s err=%v", doc, lifetime, err)
	}
	if parseCIMDCacheLifetime(http.Header{"Cache-Control": []string{"max-age=120, no-store"}}) != 0 {
		t.Fatal("no-store directive did not override max-age")
	}
	for _, tc := range []struct {
		name string
		resp *http.Response
	}{
		{"redirect", response(http.StatusFound, "application/json", "", valid)},
		{"wrong content type", response(http.StatusOK, "text/html", "", valid)},
		{"wrong client id", response(http.StatusOK, "application/json", "", strings.Replace(valid, clientID, "https://other.example/client.json", 1))},
		{"shared secret method", response(http.StatusOK, "application/json", "", strings.Replace(valid, `"none"`, `"client_secret_basic"`, 1))},
		{"client secret null", response(http.StatusOK, "application/json", "", strings.Replace(valid, `,"token_endpoint_auth_method"`, `,"client_secret":null,"token_endpoint_auth_method"`, 1))},
		{"invalid redirect", response(http.StatusOK, "application/json", "", strings.Replace(valid, "http://127.0.0.1:4747/callback", "http://attacker.example/callback", 1))},
		{"oversized", response(http.StatusOK, "application/json", "", strings.Repeat("x", cimdMaxDocumentBytes+1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := fetchCIMDMetadataWithHTTPClient(context.Background(), clientID, client(tc.resp)); err == nil {
				t.Fatal("invalid metadata response was accepted")
			}
		})
	}
	redirectClient := cimdHTTPClient(time.Second)
	if err := redirectClient.CheckRedirect(&http.Request{}, []*http.Request{{}}); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CIMD HTTP client follows redirects: %v", err)
	}
}

func TestValidatePublicBaseURL(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"https://dock.example", true}, {"https://dock.example/", true}, {"http://localhost:18765", true}, {"http://127.0.0.1:18765", true}, {"http://[::1]:18765", true},
		{"http://dock.example", false}, {"http://[::2]", false}, {"https://dock.example/path", false}, {"https://user@dock.example", false}, {"https://dock.example?issuer=x", false}, {"https://dock.example#fragment", false}, {"//dock.example", false}, {"", false},
	} {
		if got := ValidatePublicBaseURL(tc.value); got != tc.want {
			t.Errorf("ValidatePublicBaseURL(%q)=%v want %v", tc.value, got, tc.want)
		}
	}
}

func TestOAuthRedirectURIValidationAndExactMatching(t *testing.T) {
	for _, tc := range []struct {
		uri  string
		want bool
	}{
		{"https://client.example/callback", true},
		{"https://client.example/callback/", true},
		{"https://client.example/callback?tenant=a%2Fb", true},
		{"https://client.example/callback/%2F", true},
		{"http://localhost:3000/callback", true},
		{"http://[::1]:3000/callback", true},
		{"http://client.example/callback", false},
		{"https://user@client.example/callback", false},
		{"https://client.example/callback#fragment", false},
		{"https:opaque-callback", false},
		{" https://client.example/callback", false},
		{"https://client.example:bad/callback", false},
		{"", false},
	} {
		t.Run(tc.uri, func(t *testing.T) {
			if got := validOAuthRedirectURI(tc.uri); got != tc.want {
				t.Errorf("validOAuthRedirectURI(%q)=%v want %v", tc.uri, got, tc.want)
			}
		})
	}
	registered := "https://client.example/callback/%2F?tenant=a%2Fb"
	if !containsExact([]string{registered}, registered) {
		t.Fatal("registered redirect URI did not exactly match itself")
	}
	for _, variant := range []string{
		"https://client.example/callback/%2f?tenant=a%2Fb",
		"https://client.example/callback/%2F?tenant=a/b",
		"https://client.example/callback/%2F/?tenant=a%2Fb",
	} {
		if containsExact([]string{registered}, variant) {
			t.Errorf("redirect URI was normalized instead of exact-matched: %q", variant)
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
		{"unsupported response type", func(q url.Values) { q.Set("response_type", "token") }, http.StatusFound, "unsupported_response_type"},
		{"missing response type", func(q url.Values) { q.Del("response_type") }, http.StatusFound, "invalid_request"},
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

func TestOAuthTokenAcceptsOmittedTokenRequestRedirectURI(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/oauth-token-optional-redirect.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const base = "https://oauth.example"
	const clientID = "public-client"
	const redirect = "http://127.0.0.1:4781/callback"
	const verifier = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	if err := db.RegisterOAuthClient(context.Background(), clientID, "Public client", []string{redirect}, ""); err != nil {
		t.Fatal(err)
	}
	challengeSum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeSum[:])
	if _, err := db.CreateOAuthCode(context.Background(), oauthHash("optional-redirect-code"), store.OAuthCode{
		ClientID: clientID, RedirectURI: redirect, Challenge: challenge, Resource: base + "/mcp",
		Scopes: []string{oauthReadScope}, ExpiresAt: time.Now().Add(time.Minute),
	}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	h := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: base}).Handler()
	form := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {"optional-redirect-code"},
		"resource": {base + "/mcp"}, "code_verifier": {verifier},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var response struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.AccessToken == "" || response.TokenType != "Bearer" || response.Scope != oauthReadScope {
		t.Fatalf("OAuth 2.1 token exchange without redirect_uri failed: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestOAuthAuthorizeAllowsOmittedSingleRedirectAndRejectsAmbiguousOrOversizedState(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/oauth.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const base = "https://oauth.example"
	if err := db.RegisterOAuthClient(context.Background(), "single", "Single", []string{"https://client.example/callback"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.RegisterOAuthClient(context.Background(), "multiple", "Multiple", []string{"https://client.example/a", "https://client.example/b"}, ""); err != nil {
		t.Fatal(err)
	}
	h := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: base}).Handler()
	login := request(h, http.MethodPost, "/api/v1/auth/login", `{"token":"owner-secret"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("owner login: %d %s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	sum := sha256.Sum256([]byte(strings.Repeat("v", 43)))
	baseQuery := url.Values{"response_type": {"code"}, "client_id": {"single"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "resource": {base + "/mcp"}, "scope": {oauthReadScope}, "state": {"safe-state"}}
	query := baseQuery.Encode()
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query, nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "https://client.example/callback") {
		t.Fatalf("single registered redirect should be inferred: status=%d body=%s", w.Code, w.Body.String())
	}
	explicitEmpty := url.Values{}
	for key, values := range baseQuery {
		explicitEmpty[key] = append([]string(nil), values...)
	}
	explicitEmpty["redirect_uri"] = []string{""}
	req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+explicitEmpty.Encode(), nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "https://client.example/callback") {
		t.Fatalf("empty redirect_uri must be treated as omitted when exactly one is registered: status=%d body=%s", w.Code, w.Body.String())
	}

	ambiguous := url.Values{}
	for key, values := range baseQuery {
		ambiguous[key] = append([]string(nil), values...)
	}
	ambiguous.Set("client_id", "multiple")
	req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+ambiguous.Encode(), nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" {
		t.Fatalf("ambiguous omitted redirect must fail locally: status=%d location=%q", w.Code, w.Header().Get("Location"))
	}

	oversized := url.Values{}
	for key, values := range baseQuery {
		oversized[key] = append([]string(nil), values...)
	}
	oversized.Set("state", strings.Repeat("x", 513))
	req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+oversized.Encode(), nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" || strings.Contains(w.Body.String(), strings.Repeat("x", 32)) {
		t.Fatalf("oversized state must not be reflected: status=%d location=%q body=%s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
}

func TestOAuthEndpointsRejectRepeatedParametersAndMixedClientAuthentication(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/oauth-ambiguous-params.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RegisterOAuthClient(context.Background(), "public-client", "Public", []string{"https://client.example/callback"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.RegisterOAuthClient(context.Background(), "confidential-client", "Confidential", []string{"https://client.example/callback"}, oauthHash("client-secret")); err != nil {
		t.Fatal(err)
	}
	h := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: "https://oauth.example"}).Handler()
	assertInvalidRequest := func(name string, req *http.Request) {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		var response map[string]string
		if w.Code != http.StatusBadRequest || json.Unmarshal(w.Body.Bytes(), &response) != nil || response["error"] != "invalid_request" {
			t.Errorf("%s response status=%d body=%s; want HTTP 400 invalid_request", name, w.Code, w.Body.String())
		}
	}
	formRequest := func(path string, form url.Values) *http.Request {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req
	}
	assertInvalidRequest("duplicate authorization client_id", httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=public-client&client_id=public-client", nil))
	assertInvalidRequest("duplicate token code", formRequest("/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {"public-client"}, "code": {"one", "two"},
		"resource": {"https://oauth.example/mcp"}, "code_verifier": {strings.Repeat("v", 43)},
	}))
	conflictingToken := formRequest("/oauth/token", url.Values{"client_id": {"confidential-client"}, "client_secret": {"client-secret"}, "grant_type": {"authorization_code"}})
	conflictingToken.SetBasicAuth("confidential-client", "client-secret")
	assertInvalidRequest("token basic and post authentication", conflictingToken)
	assertInvalidRequest("duplicate revoke token", formRequest("/oauth/revoke", url.Values{
		"client_id": {"public-client"}, "token": {"one", "two"},
	}))
	conflictingRevoke := formRequest("/oauth/revoke", url.Values{"client_id": {"confidential-client"}, "client_secret": {"client-secret"}, "token": {"opaque-token"}})
	conflictingRevoke.SetBasicAuth("confidential-client", "client-secret")
	assertInvalidRequest("revoke basic and post authentication", conflictingRevoke)
}

func TestOAuth2IgnoresRepeatedUnknownParametersAndChallengesFailedBasicAuth(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/oauth2-unknown-params.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RegisterOAuthClient(context.Background(), "confidential-client", "Confidential", []string{"https://client.example/callback"}, oauthHash("client-secret")); err != nil {
		t.Fatal(err)
	}
	h := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: "https://oauth.example"}).Handler()

	query := url.Values{"response_type": {"unsupported"}, "client_id": {"confidential-client"}, "extension_hint": {"one", "two"}}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil))
	if w.Code == http.StatusBadRequest && strings.Contains(w.Body.String(), "appear no more than once") {
		t.Fatalf("repeated unknown extension parameter must be ignored: status=%d body=%s", w.Code, w.Body.String())
	}

	tokenForm := url.Values{"grant_type": {"unsupported"}, "client_id": {"confidential-client"}, "client_secret": {"client-secret"}, "extension_hint": {"one", "two"}}
	tokenRequest := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, tokenRequest)
	var tokenResponse map[string]string
	if w.Code != http.StatusBadRequest || json.Unmarshal(w.Body.Bytes(), &tokenResponse) != nil || tokenResponse["error"] != "unsupported_grant_type" {
		t.Fatalf("repeated unknown token extension must be ignored and request processing must continue: status=%d body=%s", w.Code, w.Body.String())
	}

	for _, tc := range []struct {
		name, clientID, secret string
	}{
		{name: "wrong secret", clientID: "confidential-client", secret: "wrong"},
		{name: "unknown client", clientID: "missing-client", secret: "anything"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("grant_type=authorization_code"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.SetBasicAuth(tc.clientID, tc.secret)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			var response map[string]string
			if w.Code != http.StatusUnauthorized || !strings.HasPrefix(w.Header().Get("WWW-Authenticate"), "Basic ") || json.Unmarshal(w.Body.Bytes(), &response) != nil || response["error"] != "invalid_client" {
				t.Fatalf("Basic authentication failure must include a Basic challenge: status=%d challenge=%q body=%s", w.Code, w.Header().Get("WWW-Authenticate"), w.Body.String())
			}
		})
	}

	const metadataClientID = "https://client.example:8443/oauth/metadata.json"
	if err := db.RegisterOAuthClient(context.Background(), metadataClientID, "Metadata client", []string{"https://client.example/callback"}, oauthHash("secret: with spaces")); err != nil {
		t.Fatal(err)
	}
	encodedCredentials := url.QueryEscape(metadataClientID) + ":" + url.QueryEscape("secret: with spaces")
	request := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("grant_type=unsupported"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(encodedCredentials)))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, request)
	if w.Code != http.StatusBadRequest || json.Unmarshal(w.Body.Bytes(), &tokenResponse) != nil || tokenResponse["error"] != "unsupported_grant_type" {
		t.Fatalf("RFC 6749 form-encoded client_secret_basic credentials must authenticate: status=%d challenge=%q body=%s", w.Code, w.Header().Get("WWW-Authenticate"), w.Body.String())
	}

	badPost := url.Values{"grant_type": {"authorization_code"}, "client_id": {"confidential-client"}, "client_secret": {"wrong"}}
	postRequest := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(badPost.Encode()))
	postRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, postRequest)
	if w.Code != http.StatusBadRequest || w.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("failed client_secret_post must return HTTP 400 without an HTTP auth challenge: status=%d challenge=%q body=%s", w.Code, w.Header().Get("WWW-Authenticate"), w.Body.String())
	}

	unsupportedAuth := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("grant_type=authorization_code"))
	unsupportedAuth.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	unsupportedAuth.Header.Set("Authorization", "Bearer attempted-client-auth")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, unsupportedAuth)
	if w.Code != http.StatusUnauthorized || !strings.HasPrefix(w.Header().Get("WWW-Authenticate"), "Bearer ") {
		t.Fatalf("Authorization-header authentication failure must challenge the attempted scheme: status=%d challenge=%q body=%s", w.Code, w.Header().Get("WWW-Authenticate"), w.Body.String())
	}
}

func TestOAuthTokenDistinguishesMissingParametersFromUnsupportedOrInvalidValues(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/oauth-token-errors.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RegisterOAuthClient(context.Background(), "public-client", "Public", []string{"https://client.example/callback"}, ""); err != nil {
		t.Fatal(err)
	}
	h := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: "https://oauth.example"}).Handler()
	validVerifier := strings.Repeat("v", 43)
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{name: "missing grant type", form: url.Values{"client_id": {"public-client"}}, want: "invalid_request"},
		{name: "unsupported grant type", form: url.Values{"client_id": {"public-client"}, "grant_type": {"client_credentials"}}, want: "unsupported_grant_type"},
		{name: "missing code", form: url.Values{"client_id": {"public-client"}, "grant_type": {"authorization_code"}, "resource": {"https://oauth.example/mcp"}, "code_verifier": {validVerifier}}, want: "invalid_request"},
		{name: "missing verifier", form: url.Values{"client_id": {"public-client"}, "grant_type": {"authorization_code"}, "code": {"missing-code"}, "resource": {"https://oauth.example/mcp"}}, want: "invalid_request"},
		{name: "invalid verifier", form: url.Values{"client_id": {"public-client"}, "grant_type": {"authorization_code"}, "code": {"missing-code"}, "resource": {"https://oauth.example/mcp"}, "code_verifier": {"short"}}, want: "invalid_grant"},
		{name: "missing resource", form: url.Values{"client_id": {"public-client"}, "grant_type": {"authorization_code"}, "code": {"missing-code"}, "code_verifier": {validVerifier}}, want: "invalid_target"},
		{name: "wrong resource", form: url.Values{"client_id": {"public-client"}, "grant_type": {"authorization_code"}, "code": {"missing-code"}, "resource": {"https://other.example/mcp"}, "code_verifier": {validVerifier}}, want: "invalid_target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			var response map[string]string
			if w.Code != http.StatusBadRequest || json.Unmarshal(w.Body.Bytes(), &response) != nil || response["error"] != tc.want {
				t.Fatalf("status=%d error=%q body=%s; want HTTP 400 %s", w.Code, response["error"], w.Body.String(), tc.want)
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

func TestOAuthOwnerCanRotateConfidentialClientSecretOnce(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/oauth-rotate.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RegisterOAuthClient(context.Background(), "confidential-client", "Confidential", []string{"https://client.example/callback"}, oauthHash("old-secret")); err != nil {
		t.Fatal(err)
	}
	if err := db.RegisterOAuthClient(context.Background(), "public-client", "Public", []string{"https://public.example/callback"}, ""); err != nil {
		t.Fatal(err)
	}
	h := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: "https://oauth.example"}).Handler()
	login := request(h, http.MethodPost, "/api/v1/auth/login", `{"token":"owner-secret"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("owner login status=%d body=%s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	registration := httptest.NewRequest(http.MethodPost, "/api/v1/oauth/clients", strings.NewReader(`{"name":"New confidential","redirect_uris":["https://new-client.example/callback"],"confidential":true}`))
	registration.Header.Set("Content-Type", "application/json")
	registration.Header.Set("Origin", "http://example.com")
	registration.AddCookie(cookie)
	registered := httptest.NewRecorder()
	h.ServeHTTP(registered, registration)
	if registered.Code != http.StatusCreated || registered.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("register response status=%d cache-control=%q body=%s", registered.Code, registered.Header().Get("Cache-Control"), registered.Body.String())
	}
	rotate := httptest.NewRequest(http.MethodPost, "/api/v1/oauth/clients/confidential-client/rotate-secret", nil)
	rotate.AddCookie(cookie)
	rotate.Header.Set("Origin", "http://example.com")
	rotated := httptest.NewRecorder()
	h.ServeHTTP(rotated, rotate)
	if rotated.Code != http.StatusOK || rotated.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("rotate status=%d cache-control=%q body=%s", rotated.Code, rotated.Header().Get("Cache-Control"), rotated.Body.String())
	}
	var response struct {
		ClientID   string `json:"client_id"`
		Secret     string `json:"client_secret"`
		SecretOnce bool   `json:"client_secret_once"`
	}
	if err := json.Unmarshal(rotated.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ClientID != "confidential-client" || response.Secret == "" || !response.SecretOnce {
		t.Fatalf("rotation response did not return a one-time secret: %#v", response)
	}
	client, err := db.OAuthClient(context.Background(), response.ClientID)
	if err != nil || client.SecretHash != oauthHash(response.Secret) || client.SecretHash == response.Secret {
		t.Fatalf("rotated credential was not stored as a hash: hash=%q err=%v", client.SecretHash, err)
	}
	list := httptest.NewRequest(http.MethodGet, "/api/v1/oauth/clients", nil)
	list.AddCookie(cookie)
	listed := httptest.NewRecorder()
	h.ServeHTTP(listed, list)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), response.Secret) || strings.Contains(listed.Body.String(), client.SecretHash) {
		t.Fatalf("client list leaked secret material: status=%d body=%s", listed.Code, listed.Body.String())
	}
	public := httptest.NewRequest(http.MethodPost, "/api/v1/oauth/clients/public-client/rotate-secret", nil)
	public.AddCookie(cookie)
	public.Header.Set("Origin", "http://example.com")
	publicResponse := httptest.NewRecorder()
	h.ServeHTTP(publicResponse, public)
	if publicResponse.Code != http.StatusConflict {
		t.Fatalf("public client rotation status=%d want=409 body=%s", publicResponse.Code, publicResponse.Body.String())
	}
}

func TestOAuthPrincipalMatrixDoesNotCrossIntoREST(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/oauth-matrix.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, _, err := db.CreateReport(context.Background(), store.ReportInput{Source: "matrix", BotID: "bot", BotName: "Bot", ExecutedAt: time.Now().UTC(), Content: "matrix report"}); err != nil {
		t.Fatal(err)
	}
	const base = "https://oauth.example"
	if err := db.RegisterOAuthClient(context.Background(), "matrix-client", "Matrix", []string{"https://client.example/cb"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateOAuthCode(context.Background(), "matrix-code", store.OAuthCode{ClientID: "matrix-client", RedirectURI: "https://client.example/cb", Challenge: "challenge", Resource: base + "/mcp", Scopes: []string{oauthReadScope}, ExpiresAt: time.Now().Add(time.Minute)}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExchangeOAuthCode(context.Background(), "matrix-code", store.OAuthCodeBinding{ClientID: "matrix-client", RedirectURI: "https://client.example/cb", Resource: base + "/mcp"}, oauthHash("matrix-oauth-token"), time.Now().Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	h := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: base}).Handler()
	login := request(h, http.MethodPost, "/api/v1/auth/login", `{"token":"owner-secret"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("owner login: status=%d body=%s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	cases := []struct {
		name   string
		bearer string
		cookie *http.Cookie
		want   int
	}{
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "browser cookie", cookie: cookie, want: http.StatusOK},
		{name: "legacy bearer", bearer: "owner-secret", want: http.StatusOK},
		{name: "OAuth bearer cannot access REST", bearer: "matrix-oauth-token", want: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/reports", nil)
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("GET /api/v1/reports status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestOAuthPrincipalPermissionMatrixAcrossMCPTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := store.Open(t.TempDir() + "/oauth-principal-matrix.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const base = "https://oauth.example"
	resource := base + "/mcp"
	redirect := "https://client.example/callback"
	for _, scope := range []struct {
		id     string
		scopes []string
	}{
		{"read-client", []string{oauthReadScope}},
		{"write-client", []string{oauthWriteScope}},
		{"both-client", []string{oauthReadScope, oauthWriteScope}},
	} {
		if err := db.RegisterOAuthClient(ctx, scope.id, scope.id, []string{redirect}, ""); err != nil {
			t.Fatal(err)
		}
		codeHash := oauthHash("code-" + scope.id)
		token := "token-" + scope.id
		challenge := "matrix-challenge-" + scope.id
		code := store.OAuthCode{ClientID: scope.id, RedirectURI: redirect, Challenge: challenge, Resource: resource, Scopes: scope.scopes, ExpiresAt: time.Now().Add(time.Minute)}
		if _, err := db.CreateOAuthCode(ctx, codeHash, code, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExchangeOAuthCode(ctx, codeHash, store.OAuthCodeBinding{ClientID: scope.id, RedirectURI: redirect, Resource: resource}, oauthHash(token), time.Now().Add(15*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := db.CreateReport(ctx, store.ReportInput{Source: "matrix", BotID: "seed", BotName: "Seed", ExecutedAt: time.Now().UTC(), Content: "oauth matrix searchable marker"}); err != nil {
		t.Fatal(err)
	}
	h := NewWithOAuth(db, events.New(), "legacy-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: base}).Handler()
	remote := httptest.NewServer(h)
	t.Cleanup(remote.Close)
	login := request(h, http.MethodPost, "/api/v1/auth/login", `{"token":"legacy-secret"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("owner login: %d %s", login.Code, login.Body.String())
	}
	unauthenticated := []struct {
		name   string
		token  string
		cookie *http.Cookie
	}{
		{name: "anonymous"},
		{name: "browser cookie cannot authenticate MCP", cookie: login.Result().Cookies()[0]},
		{name: "invalid bearer", token: "invalid-token"},
	}
	for _, tc := range unauthenticated {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, remote.URL+"/mcp", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("unauthorized principal got HTTP %d", resp.StatusCode)
			}
		})
	}
	principals := []struct {
		name       string
		token      string
		canSearch  bool
		canPublish bool
	}{
		{name: "legacy bearer", token: "legacy-secret", canSearch: true, canPublish: true},
		{name: "OAuth read", token: "token-read-client", canSearch: true},
		{name: "OAuth write", token: "token-write-client", canPublish: true},
		{name: "OAuth read and write", token: "token-both-client", canSearch: true, canPublish: true},
	}
	for _, principal := range principals {
		t.Run(principal.name, func(t *testing.T) {
			recorder := &oauthResponseRecorder{next: http.DefaultTransport}
			httpClient := &http.Client{Transport: bearerRoundTripper{next: recorder, token: principal.token}}
			mcpClient := mcp.NewClient(&mcp.Implementation{Name: "principal-matrix", Version: "1"}, nil)
			session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: remote.URL + "/mcp", HTTPClient: httpClient}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
			if err != nil {
				t.Fatalf("initialize: %v", err)
			}
			defer session.Close()
			listed, err := session.ListTools(ctx, nil)
			if err != nil || len(listed.Tools) != 2 {
				t.Fatalf("tools/list=%#v err=%v", listed, err)
			}
			searched, searchErr := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "oauth matrix searchable marker"}})
			if principal.canSearch {
				if searchErr != nil || searched.IsError || len(searched.Content) == 0 {
					t.Fatalf("allowed search failed: result=%#v err=%v", searched, searchErr)
				}
			} else if searchErr == nil && !searched.IsError {
				t.Fatal("write-only token searched report history")
			} else if recorder.status != http.StatusForbidden || !strings.Contains(recorder.header.Get("WWW-Authenticate"), `error="insufficient_scope"`) || !strings.Contains(recorder.header.Get("WWW-Authenticate"), oauthReadScope) {
				t.Fatalf("read denial status=%d challenge=%q err=%v", recorder.status, recorder.header.Get("WWW-Authenticate"), searchErr)
			}
			published, publishErr := session.CallTool(ctx, &mcp.CallToolParams{Name: "publish_report", Arguments: map[string]any{
				"source": "matrix", "bot_id": strings.ReplaceAll(principal.name, " ", "-"), "executed_at": "2026-10-10T10:00:00Z", "external_id": "matrix-" + strings.ReplaceAll(principal.name, " ", "-"), "content": "principal matrix publish " + principal.name,
			}})
			if principal.canPublish {
				if publishErr != nil || published.IsError {
					t.Fatalf("allowed publish failed: result=%#v err=%v", published, publishErr)
				}
			} else if publishErr == nil && !published.IsError {
				t.Fatal("read-only token published a report")
			} else if recorder.status != http.StatusForbidden || !strings.Contains(recorder.header.Get("WWW-Authenticate"), `error="insufficient_scope"`) || !strings.Contains(recorder.header.Get("WWW-Authenticate"), oauthWriteScope) {
				t.Fatalf("write denial status=%d challenge=%q err=%v", recorder.status, recorder.header.Get("WWW-Authenticate"), publishErr)
			}
		})
	}
}

func TestCIMDAuthorizationDiscoversConsentsAndIssuesPublicClientToken(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/cimd-flow.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const base = "https://oauth.example"
	const clientID = "https://client.example/oauth/metadata.json"
	metadata := cimdDocument{
		ClientID: clientID, ClientName: "CIMD Example", RedirectURIs: []string{"http://127.0.0.1:4765/callback", "http://localhost:4765/callback"},
		TokenEndpointAuthMethod: "none", GrantTypes: []string{"authorization_code"}, ResponseTypes: []string{"code"},
	}
	server := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: base})
	fetchCount := 0
	server.fetchCIMD = func(_ context.Context, id string) (cimdDocument, time.Duration, error) {
		fetchCount++
		if id != clientID {
			return cimdDocument{}, 0, errors.New("unexpected metadata URL")
		}
		return metadata, time.Minute, nil
	}
	h := server.Handler()
	login := request(h, http.MethodPost, "/api/v1/auth/login", `{"token":"owner-secret"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("owner login: status=%d body=%s", login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	newAuthorizeRequest := func(state, challenge string) *httptest.ResponseRecorder {
		query := url.Values{
			"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {metadata.RedirectURIs[0]},
			"code_challenge_method": {"S256"}, "code_challenge": {challenge},
			"resource": {base + "/mcp"}, "scope": {oauthReadScope}, "state": {state},
		}
		req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+query.Encode(), nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	deniedPage := newAuthorizeRequest("denied", base64.RawURLEncoding.EncodeToString(make([]byte, sha256.Size)))
	if deniedPage.Code != http.StatusOK || !strings.Contains(deniedPage.Body.String(), "客户端身份域名：<strong>client.example</strong>") || !strings.Contains(deniedPage.Body.String(), "CIMD Example") || strings.Contains(deniedPage.Body.String(), `name="scope" value="reports:write"`) {
		t.Fatalf("CIMD consent did not disclose identity and requested permission: status=%d body=%s", deniedPage.Code, deniedPage.Body.String())
	}
	if _, err := db.OAuthClient(context.Background(), clientID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unconsented CIMD client was persisted: %v", err)
	}
	deniedForm := url.Values{"csrf": {extractOAuthCSRF(t, deniedPage.Body.String())}, "decision": {"deny"}}
	denied := httptest.NewRequest(http.MethodPost, "/oauth/consent", strings.NewReader(deniedForm.Encode()))
	denied.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	denied.Header.Set("Origin", base)
	denied.AddCookie(cookie)
	deniedW := httptest.NewRecorder()
	h.ServeHTTP(deniedW, denied)
	if deniedW.Code != http.StatusFound {
		t.Fatalf("deny CIMD consent status=%d body=%s", deniedW.Code, deniedW.Body.String())
	}
	if _, err := db.OAuthClient(context.Background(), clientID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("denied CIMD client was persisted: %v", err)
	}

	verifier := strings.Repeat("c", 43)
	challengeSum := sha256.Sum256([]byte(verifier))
	page := newAuthorizeRequest("approved", base64.RawURLEncoding.EncodeToString(challengeSum[:]))
	if page.Code != http.StatusOK {
		t.Fatalf("authorize page: status=%d body=%s", page.Code, page.Body.String())
	}
	csrf := extractOAuthCSRF(t, page.Body.String())
	approvedForm := url.Values{"csrf": {csrf}, "decision": {"approve"}, "scope": {oauthReadScope}}
	approved := httptest.NewRequest(http.MethodPost, "/oauth/consent", strings.NewReader(approvedForm.Encode()))
	approved.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	approved.Header.Set("Origin", base)
	approved.AddCookie(cookie)
	approvedW := httptest.NewRecorder()
	h.ServeHTTP(approvedW, approved)
	callback, err := url.Parse(approvedW.Header().Get("Location"))
	if err != nil || approvedW.Code != http.StatusFound {
		t.Fatalf("consent: status=%d location=%q err=%v", approvedW.Code, approvedW.Header().Get("Location"), err)
	}
	client, err := db.OAuthClient(context.Background(), clientID)
	if err != nil || client.Registration != "cimd" || client.Confidential || len(client.Redirects) != 2 {
		t.Fatalf("consented CIMD client=%#v err=%v", client, err)
	}
	form := url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {callback.Query().Get("code")},
		"redirect_uri": {metadata.RedirectURIs[0]}, "resource": {base + "/mcp"}, "code_verifier": {verifier},
	}
	tokenReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenW := httptest.NewRecorder()
	h.ServeHTTP(tokenW, tokenReq)
	var result struct {
		AccessToken string `json:"access_token"`
	}
	if tokenW.Code != http.StatusOK || json.Unmarshal(tokenW.Body.Bytes(), &result) != nil || result.AccessToken == "" {
		t.Fatalf("public CIMD token exchange failed: status=%d body=%s", tokenW.Code, tokenW.Body.String())
	}
	if fetchCount != 1 {
		t.Fatalf("metadata fetch count=%d want a single fetch within the cache lifetime", fetchCount)
	}
	remote := httptest.NewServer(h)
	t.Cleanup(remote.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "cimd-test", Version: "1"}, nil)
	httpClient := &http.Client{Transport: bearerRoundTripper{next: http.DefaultTransport, token: result.AccessToken}}
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: remote.URL + "/mcp", HTTPClient: httpClient}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatalf("CIMD token MCP initialize: %v", err)
	}
	defer session.Close()
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatalf("CIMD token tools/list: %v", err)
	}
}

func TestOwnerLoginDoesNotAdoptPreexistingSessionIdentifier(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/session-fixation.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := NewWithOAuth(db, events.New(), "owner-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: "https://oauth.example"}).Handler()
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"token":"owner-secret"}`))
	loginRequest.AddCookie(&http.Cookie{Name: sessionCookie, Value: "attacker-chosen-session"})
	loginResponse := httptest.NewRecorder()
	h.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	issued := loginResponse.Result().Cookies()[0]
	if issued.Value == "" || issued.Value == "attacker-chosen-session" {
		t.Fatalf("login reused preexisting session identifier: %#v", issued)
	}
	for _, tc := range []struct {
		name          string
		cookie        string
		authenticated bool
	}{
		{name: "preexisting identifier", cookie: "attacker-chosen-session", authenticated: false},
		{name: "newly issued identifier", cookie: issued.Value, authenticated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: tc.cookie})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			var body struct {
				Authenticated bool `json:"authenticated"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Authenticated != tc.authenticated {
				t.Fatalf("session status=%d body=%s err=%v", w.Code, w.Body.String(), err)
			}
		})
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
		if path == "/.well-known/oauth-authorization-server" && !strings.Contains(string(raw), `"client_id_metadata_document_supported":true`) {
			t.Fatalf("AS metadata must advertise CIMD support: %s", raw)
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
	query := url.Values{"response_type": {"code"}, "client_id": {"client-test"}, "code_challenge_method": {"S256"}, "code_challenge": {challenge}, "resource": {base + "/mcp"}, "scope": {oauthReadScope}, "state": {"s-test"}}
	get := httptest.NewRequest("GET", "/oauth/authorize?"+query.Encode(), nil)
	get.AddCookie(cookie)
	page := httptest.NewRecorder()
	h.ServeHTTP(page, get)
	if page.Code != 200 {
		t.Fatalf("authorize page: %d %s", page.Code, page.Body.String())
	}
	for _, visible := range []string{"Test Client", "client-test", redirect, base + "/mcp", "reports:read", "整个单所有者报告库", "source / bot_id", "允许", "拒绝"} {
		if !strings.Contains(page.Body.String(), visible) {
			t.Errorf("consent page omitted required visible detail %q", visible)
		}
	}
	if strings.Contains(page.Body.String(), `name="scope" value="reports:write"`) {
		t.Fatal("read-only consent page displayed an unrequested write scope")
	}
	if strings.Contains(page.Body.String(), `name="scope" value="reports:read" checked`) || strings.Contains(page.Body.String(), `name="scope" value="reports:write" checked`) {
		t.Fatal("consent scope was checked by default")
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
	ok := exchange(verifier, base+"/mcp", "")
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
	if !strings.Contains(page.Body.String(), `name="scope" value="reports:write"`) || strings.Contains(page.Body.String(), `name="scope" value="reports:read"`) {
		t.Fatalf("write-only consent page did not display only the requested scope: %s", page.Body.String())
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
