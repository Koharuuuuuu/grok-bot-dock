package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Nyakooo/grok-bot-dock/internal/store"
)

const (
	oauthReadScope  = "reports:read"
	oauthWriteScope = "reports:write"
	oauthAccessTTL  = 15 * time.Minute
	oauthGrantTTL   = 30 * 24 * time.Hour
)

type OAuthConfig struct {
	Enabled       bool
	PublicBaseURL string
}

type oauthPrincipalKey struct{}
type oauthPrincipal struct{ scopes map[string]bool }
type oauthState struct {
	value   string
	present bool
}
type pendingConsent struct {
	Request oauthRequest
	Session string
	Expires time.Time
}
type oauthRequest struct {
	ClientID        string
	ClientName      string
	Registration    string
	ClientRedirects []string
	RedirectURI     string
	Challenge       string
	Resource        string
	Scopes          []string
	State           oauthState
}

func (s *Server) oauthBase() string     { return strings.TrimRight(s.oauth.PublicBaseURL, "/") }
func (s *Server) oauthResource() string { return s.oauthBase() + "/mcp" }

func (s *Server) validOAuthConfig() bool {
	return ValidatePublicBaseURL(s.oauth.PublicBaseURL)
}

func ValidateOAuthConfig(config OAuthConfig) bool {
	return !config.Enabled || ValidatePublicBaseURL(config.PublicBaseURL)
}

func ValidatePublicBaseURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}

func validOAuthRedirectURI(raw string) bool {
	if raw == "" || raw != strings.TrimSpace(raw) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Opaque != "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}

func randomOAuthValue(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func oauthHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Server) protectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.validOAuthConfig() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{"resource": s.oauthResource(), "authorization_servers": []string{s.oauthBase()}, "scopes_supported": []string{oauthReadScope, oauthWriteScope}, "bearer_methods_supported": []string{"header"}})
}
func (s *Server) authorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.validOAuthConfig() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer": s.oauthBase(), "authorization_endpoint": s.oauthBase() + "/oauth/authorize", "token_endpoint": s.oauthBase() + "/oauth/token", "revocation_endpoint": s.oauthBase() + "/oauth/revoke",
		"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code"}, "code_challenge_methods_supported": []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"}, "client_id_metadata_document_supported": true, "scopes_supported": []string{oauthReadScope, oauthWriteScope}, "authorization_response_iss_parameter_supported": true,
	})
}

func (s *Server) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	if !s.validOAuthConfig() {
		http.NotFound(w, r)
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || hasRepeatedOAuthParams(q, "response_type", "client_id", "redirect_uri", "scope", "state", "code_challenge", "code_challenge_method", "resource") {
		oauthError(w, r, "invalid_request", "authorization parameters must be valid and appear no more than once")
		return
	}
	state := oauthState{value: q.Get("state"), present: q.Has("state")}
	clientID := q.Get("client_id")
	if allowed, wait := s.allowRate("oauth-authorize-ip:"+oauthPeerIP(r), 30, time.Now()); !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, int(wait.Seconds()))))
		writeError(w, 429, "rate_limited", "too many authorization requests")
		return
	}
	client, err := s.resolveOAuthAuthorizationClient(r.Context(), clientID)
	redirect := q.Get("redirect_uri")
	if err != nil || client.DisabledAt != "" {
		oauthError(w, r, "invalid_request", "invalid client or redirect_uri")
		return
	}
	if redirect == "" && len(client.Redirects) == 1 {
		redirect = client.Redirects[0]
	}
	if !containsExact(client.Redirects, redirect) {
		oauthError(w, r, "invalid_request", "invalid client or redirect_uri")
		return
	}
	if len(q.Get("state")) > 512 {
		// Do not reflect an oversized client-controlled state value in Location.
		oauthError(w, r, "invalid_request", "state exceeds the maximum length")
		return
	}
	responseType := q.Get("response_type")
	if responseType == "" {
		s.oauthRedirectError(w, redirect, state, "invalid_request")
		return
	}
	if responseType != "code" {
		s.oauthRedirectError(w, redirect, state, "unsupported_response_type")
		return
	}
	if q.Get("code_challenge_method") != "S256" || !validChallenge(q.Get("code_challenge")) {
		s.oauthRedirectError(w, redirect, state, "invalid_request")
		return
	}
	resource := q.Get("resource")
	if resource == "" || resource != s.oauthResource() {
		s.oauthRedirectError(w, redirect, state, "invalid_target")
		return
	}
	scopes, ok := requestedOAuthScopes(q.Get("scope"))
	if !ok {
		s.oauthRedirectError(w, redirect, state, "invalid_scope")
		return
	}
	request := oauthRequest{ClientID: client.ID, ClientName: client.Name, Registration: client.Registration, ClientRedirects: append([]string(nil), client.Redirects...), RedirectURI: redirect, Challenge: q.Get("code_challenge"), Resource: resource, Scopes: scopes, State: state}
	cookie, _ := r.Cookie(sessionCookie)
	session := ""
	if cookie != nil && s.validSession(cookie.Value, time.Now()) {
		session = cookie.Value
	}
	if session == "" {
		s.renderOAuthLogin(w, r)
		return
	}
	if allowed, wait := s.allowRate("oauth-authorize:"+session, 60, time.Now()); !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, int(wait.Seconds()))))
		writeError(w, 429, "rate_limited", "too many authorization requests")
		return
	}
	csrf, err := randomOAuthValue(32)
	if err != nil {
		writeError(w, 500, "internal_error", "could not prepare consent")
		return
	}
	s.consentMu.Lock()
	now := time.Now()
	for key, p := range s.consents {
		if !now.Before(p.Expires) {
			delete(s.consents, key)
		}
	}
	s.consents[oauthHash(csrf)] = pendingConsent{Request: request, Session: session, Expires: now.Add(10 * time.Minute)}
	s.consentMu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	clientIDHost := ""
	if clientIDURL, parseErr := url.Parse(request.ClientID); parseErr == nil && clientIDURL.Scheme == "https" {
		clientIDHost = clientIDURL.Hostname()
	}
	data := struct {
		CSRF, Name, ClientID, ClientIDHost, Redirect, Resource string
		Read, Write, CIMD                                      bool
	}{CSRF: csrf, Name: request.ClientName, ClientID: request.ClientID, ClientIDHost: clientIDHost, Redirect: request.RedirectURI, Resource: request.Resource, Read: containsExact(scopes, oauthReadScope), Write: containsExact(scopes, oauthWriteScope), CIMD: request.Registration == "cimd"}
	_ = oauthConsentTemplate.Execute(w, data)
}

func (s *Server) renderOAuthLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Bolt Dock 授权登录</title><body style="font:16px system-ui;max-width:38rem;margin:4rem auto;padding:1rem"><h1>登录 Bolt Dock</h1><p>输入服务管理员访问密钥以审核 MCP 客户端授权请求。密钥只发送到当前服务。</p><form id="f"><label>管理员访问密钥<br><input id="t" type="password" autocomplete="current-password" required style="font:inherit;width:100%;padding:.6rem"></label><p><button style="font:inherit;padding:.6rem">登录并继续</button></p><p id="e" role="alert"></p></form><script>document.querySelector('#f').addEventListener('submit',async e=>{e.preventDefault();const x=document.querySelector('#e');try{const r=await fetch('/api/v1/auth/login',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({token:document.querySelector('#t').value})});if(!r.ok)throw Error(r.status===401?'访问密钥无效':'登录失败');location.reload()}catch(err){x.textContent=err.message}})</script></body></html>`)
}

var oauthConsentTemplate = template.Must(template.New("consent").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>授权 MCP 客户端</title><body style="font:16px system-ui;max-width:42rem;margin:4rem auto;padding:1rem"><h1>授权 MCP 客户端</h1><p><strong>{{.Name}}</strong>（{{.ClientID}}）请求连接 Bolt Dock。</p>{{if .CIMD}}<p>客户端身份域名：<strong>{{.ClientIDHost}}</strong>（由该站点提供客户端信息）</p>{{end}}<p>回调地址：<code>{{.Redirect}}</code></p><p>资源：<code>{{.Resource}}</code></p><p>读取权限可搜索整个单所有者报告库；写入权限可发布报告并声明 source / bot_id。</p><form method="post" action="/oauth/consent"><input type="hidden" name="csrf" value="{{.CSRF}}">{{if .Read}}<label><input type="checkbox" name="scope" value="reports:read"> 读取报告（reports:read）</label><br>{{end}}{{if .Write}}<label><input type="checkbox" name="scope" value="reports:write"> 发布报告（reports:write）</label>{{end}}<p><button name="decision" value="approve">允许</button> <button name="decision" value="deny">拒绝</button></p></form></body></html>`))

func (s *Server) oauthConsent(w http.ResponseWriter, r *http.Request) {
	if !s.validOAuthConfig() {
		http.NotFound(w, r)
		return
	}
	if !s.originIsPublicBase(r) {
		writeError(w, 403, "invalid_origin", "consent must be submitted from this service origin")
		return
	}
	cookie, _ := r.Cookie(sessionCookie)
	if cookie == nil || !s.validSession(cookie.Value, time.Now()) {
		writeError(w, 401, "login_required", "owner login required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		writeError(w, 400, "invalid_request", "invalid consent form")
		return
	}
	key := oauthHash(r.Form.Get("csrf"))
	s.consentMu.Lock()
	pending, ok := s.consents[key]
	if ok {
		delete(s.consents, key)
	}
	s.consentMu.Unlock()
	if !ok || pending.Session != cookie.Value || !time.Now().Before(pending.Expires) {
		writeError(w, 400, "invalid_request", "consent request expired; restart authorization")
		return
	}
	req := pending.Request
	if r.Form.Get("decision") != "approve" {
		if r.Form.Get("decision") == "deny" {
			_ = s.store.OAuthAudit(r.Context(), "authorize", req.ClientID, "denied")
			s.oauthRedirectError(w, req.RedirectURI, req.State, "access_denied")
			return
		}
		writeError(w, 400, "invalid_request", "invalid consent decision")
		return
	}
	selected := r.Form["scope"]
	for _, scope := range selected {
		if !containsExact(req.Scopes, scope) {
			writeError(w, 400, "invalid_scope", "scope was not requested")
			return
		}
	}
	if len(selected) == 0 {
		s.oauthRedirectError(w, req.RedirectURI, req.State, "access_denied")
		return
	}
	if req.Registration == "cimd" {
		if err := s.store.UpsertOAuthMetadataClient(r.Context(), req.ClientID, req.ClientName, req.ClientRedirects); err != nil {
			if errors.Is(err, store.ErrOAuthDisabled) || errors.Is(err, store.ErrOAuthClientTypeConflict) {
				writeError(w, http.StatusBadRequest, "invalid_client", "client metadata could not be accepted")
			} else {
				serverError(w, err)
			}
			return
		}
	}
	code, err := randomOAuthValue(32)
	if err != nil {
		writeError(w, 500, "internal_error", "could not create authorization code")
		return
	}
	_, err = s.store.CreateOAuthCode(r.Context(), oauthHash(code), store.OAuthCode{
		ClientID: req.ClientID, RedirectURI: req.RedirectURI, Challenge: req.Challenge,
		Resource: req.Resource, Scopes: selected, ExpiresAt: time.Now().Add(5 * time.Minute),
	}, time.Now().Add(oauthGrantTTL))
	if err != nil {
		serverError(w, err)
		return
	}
	_ = s.store.OAuthAudit(r.Context(), "authorize", req.ClientID, "approved")
	redirectOAuth(w, req.RedirectURI, req.State, map[string]string{"code": code, "iss": s.oauthBase()})
}

func (s *Server) oauthToken(w http.ResponseWriter, r *http.Request) {
	if !s.validOAuthConfig() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		oauthJSONError(w, 405, "invalid_request")
		return
	}
	if err := parseOAuthForm(w, r, "grant_type", "code", "redirect_uri", "resource", "code_verifier", "client_id", "client_secret"); err != nil {
		oauthJSONError(w, 400, "invalid_request")
		return
	}
	if hasOAuthClientAuthConflict(r) {
		oauthJSONError(w, 400, "invalid_request")
		return
	}
	clientID, secret := oauthClientCredentials(r)
	if allowed, wait := s.allowRate("oauth-token-ip:"+oauthPeerIP(r), 120, time.Now()); !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, int(wait.Seconds()))))
		oauthJSONError(w, 429, "temporarily_unavailable")
		return
	}
	_, status, ok := s.authenticateOAuthClient(r, clientID, secret)
	if !ok {
		status = writeOAuthClientAuthChallenge(w, r, status)
		oauthJSONError(w, status, "invalid_client")
		return
	}
	if allowed, wait := s.allowRate(oauthRateKey(r, clientID), 60, time.Now()); !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, int(wait.Seconds()))))
		oauthJSONError(w, 429, "temporarily_unavailable")
		return
	}
	grantType := r.Form.Get("grant_type")
	if grantType == "" {
		oauthJSONError(w, 400, "invalid_request")
		return
	}
	if grantType != "authorization_code" {
		oauthJSONError(w, 400, "unsupported_grant_type")
		return
	}
	redirectURI, code, resource := r.Form.Get("redirect_uri"), r.Form.Get("code"), r.Form.Get("resource")
	if code == "" {
		oauthJSONError(w, 400, "invalid_request")
		return
	}
	if resource == "" || resource != s.oauthResource() {
		oauthJSONError(w, 400, "invalid_target")
		return
	}
	verifier := r.Form.Get("code_verifier")
	if verifier == "" {
		oauthJSONError(w, 400, "invalid_request")
		return
	}
	if len(verifier) < 43 || len(verifier) > 128 || !unreservedVerifier(verifier) {
		oauthJSONError(w, 400, "invalid_grant")
		return
	}
	challengeSum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeSum[:])
	// Read the stored challenge through a new, non-consuming verification helper.
	binding := store.OAuthCodeBinding{ClientID: clientID, RedirectURI: redirectURI, Resource: resource}
	if !s.store.CheckOAuthCode(r.Context(), oauthHash(code), binding, challenge) {
		oauthJSONError(w, 400, "invalid_grant")
		return
	}
	token, err := randomOAuthValue(32)
	if err != nil {
		oauthJSONError(w, 500, "server_error")
		return
	}
	scopes, err := s.store.ExchangeOAuthCode(r.Context(), oauthHash(code), binding, oauthHash(token), time.Now().Add(oauthAccessTTL))
	if err != nil {
		oauthJSONError(w, 400, "invalid_grant")
		return
	}
	_ = s.store.OAuthAudit(r.Context(), "token", clientID, "issued")
	writeJSON(w, 200, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": int(oauthAccessTTL.Seconds()), "scope": strings.Join(scopes, " ")})
}

func (s *Server) oauthRevoke(w http.ResponseWriter, r *http.Request) {
	if !s.validOAuthConfig() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := parseOAuthForm(w, r, "token", "token_type_hint", "client_id", "client_secret"); err != nil {
		oauthJSONError(w, 400, "invalid_request")
		return
	}
	if hasOAuthClientAuthConflict(r) {
		oauthJSONError(w, 400, "invalid_request")
		return
	}
	clientID, secret := oauthClientCredentials(r)
	if allowed, wait := s.allowRate("oauth-revoke-ip:"+oauthPeerIP(r), 120, time.Now()); !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, int(wait.Seconds()))))
		oauthJSONError(w, 429, "temporarily_unavailable")
		return
	}
	_, status, ok := s.authenticateOAuthClient(r, clientID, secret)
	if !ok {
		status = writeOAuthClientAuthChallenge(w, r, status)
		oauthJSONError(w, status, "invalid_client")
		return
	}
	if allowed, wait := s.allowRate("oauth-revoke-client:"+oauthPeerIP(r)+":"+clientID, 60, time.Now()); !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, int(wait.Seconds()))))
		oauthJSONError(w, 429, "temporarily_unavailable")
		return
	}
	token := r.Form.Get("token")
	if token == "" {
		oauthJSONError(w, 400, "invalid_request")
		return
	}
	if err := s.store.RevokeOAuthToken(r.Context(), oauthHash(token), clientID); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{})
}

func (s *Server) authenticateOAuthClient(r *http.Request, clientID, secret string) (store.OAuthClient, int, bool) {
	client, err := s.store.OAuthClient(r.Context(), clientID)
	if err != nil || client.DisabledAt != "" {
		return store.OAuthClient{}, http.StatusBadRequest, false
	}
	if client.SecretHash != "" {
		if secret == "" || subtle.ConstantTimeCompare([]byte(oauthHash(secret)), []byte(client.SecretHash)) != 1 {
			return store.OAuthClient{}, http.StatusUnauthorized, false
		}
	} else if secret != "" {
		return store.OAuthClient{}, http.StatusUnauthorized, false
	}
	return client, 0, true
}

func oauthRateKey(r *http.Request, clientID string) string {
	return "oauth-token-client:" + oauthPeerIP(r) + ":" + clientID
}

func oauthPeerIP(r *http.Request) string {
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	}
	return ip
}

func (s *Server) originIsPublicBase(r *http.Request) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	base, _ := url.Parse(s.oauthBase())
	return err == nil && origin.Scheme == base.Scheme && strings.EqualFold(origin.Host, base.Host) && origin.Path == "" && origin.RawQuery == "" && origin.Fragment == ""
}
func oauthClientCredentials(r *http.Request) (string, string) {
	id, secret := r.Form.Get("client_id"), r.Form.Get("client_secret")
	if username, password, ok := r.BasicAuth(); ok {
		// OAuth client_secret_basic applies form encoding to both values before
		// placing them in the HTTP Basic username/password fields.
		formID, idErr := url.QueryUnescape(username)
		formSecret, secretErr := url.QueryUnescape(password)
		if idErr != nil || secretErr != nil {
			return "", ""
		}
		return formID, formSecret
	}
	return id, secret
}
func oauthJSONError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code})
}
func oauthError(w http.ResponseWriter, r *http.Request, code, description string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 400, map[string]string{"error": code, "error_description": description})
}

func parseOAuthForm(w http.ResponseWriter, r *http.Request, parameters ...string) error {
	if r.URL.RawQuery != "" {
		return errors.New("OAuth credentials and parameters must not appear in URL query")
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return errors.New("OAuth form content type required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		return err
	}
	if hasRepeatedOAuthParams(r.Form, parameters...) {
		return errors.New("OAuth parameters must not appear more than once")
	}
	return nil
}

func hasRepeatedOAuthParams(values url.Values, parameters ...string) bool {
	for _, parameter := range parameters {
		if len(values[parameter]) > 1 {
			return true
		}
	}
	return false
}

func writeOAuthClientAuthChallenge(w http.ResponseWriter, r *http.Request, status int) int {
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) == 0 || !validOAuthAuthScheme(fields[0]) {
		if status == http.StatusUnauthorized {
			return http.StatusBadRequest
		}
		return status
	}
	w.Header().Set("WWW-Authenticate", fields[0]+` realm="oauth"`)
	return http.StatusUnauthorized
}

func validOAuthAuthScheme(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range []byte(value) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			continue
		}
		return false
	}
	return true
}

func hasOAuthClientAuthConflict(r *http.Request) bool {
	_, _, usesBasic := r.BasicAuth()
	return usesBasic && (r.Form.Has("client_id") || r.Form.Has("client_secret"))
}
func (s *Server) oauthRedirectError(w http.ResponseWriter, redirect string, state oauthState, code string) {
	params := map[string]string{"error": code, "iss": s.oauthBase()}
	redirectOAuth(w, redirect, state, params)
}
func redirectOAuth(w http.ResponseWriter, redirect string, state oauthState, params map[string]string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	u, err := url.Parse(redirect)
	if err != nil {
		writeError(w, 400, "invalid_request", "invalid redirect")
		return
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	if state.present {
		q.Set("state", state.value)
	}
	u.RawQuery = q.Encode()
	w.Header().Set("Location", u.String())
	w.WriteHeader(http.StatusFound)
}

func requestedOAuthScopes(raw string) ([]string, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	scopes := strings.Fields(raw)
	if len(scopes) == 0 {
		return nil, false
	}
	seen := map[string]bool{}
	out := []string{}
	for _, scope := range scopes {
		if (scope != oauthReadScope && scope != oauthWriteScope) || seen[scope] {
			return nil, false
		}
		seen[scope] = true
		out = append(out, scope)
	}
	return out, true
}
func containsExact(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func validChallenge(value string) bool { return len(value) == 43 && unreservedVerifier(value) }
func unreservedVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.' || r == '_' || r == '~') {
			return false
		}
	}
	return true
}

func (s *Server) allowOAuthContext(ctx context.Context, scope string) bool {
	principal, _ := ctx.Value(oauthPrincipalKey{}).(oauthPrincipal)
	return principal.scopes == nil || principal.scopes[scope]
}
func (s *Server) oauthBearer(r *http.Request) (oauthPrincipal, bool) {
	if !s.oauth.Enabled {
		return oauthPrincipal{}, false
	}
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		return oauthPrincipal{}, false
	}
	if token == s.token {
		return oauthPrincipal{}, false
	}
	scopes, err := s.store.OAuthToken(r.Context(), oauthHash(token), s.oauthResource())
	if err != nil {
		return oauthPrincipal{}, false
	}
	principal := oauthPrincipal{scopes: map[string]bool{}}
	for _, scope := range scopes {
		principal.scopes[scope] = true
	}
	return principal, true
}

func bearerToken(header string) (string, bool) {
	fields := strings.Fields(header)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return "", false
	}
	return fields[1], true
}

func (s *Server) registerOAuthClient(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.oauth.Enabled {
		writeError(w, 404, "not_found", "OAuth is disabled")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in struct {
		ClientID     string   `json:"client_id"`
		Name         string   `json:"name"`
		RedirectURIs []string `json:"redirect_uris"`
		Confidential bool     `json:"confidential"`
	}
	if dec.Decode(&in) != nil || strings.TrimSpace(in.Name) == "" || len(in.Name) > 120 || len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > 10 {
		writeError(w, 400, "invalid_client", "name and 1-10 redirect_uris are required")
		return
	}
	if in.ClientID != "" && !validCIMDClientID(in.ClientID) {
		writeError(w, 400, "invalid_client", "custom client_id must be an HTTPS metadata URL")
		return
	}
	if in.ClientID != "" && in.Confidential {
		writeError(w, 400, "invalid_client", "custom metadata URL client IDs must be public clients")
		return
	}
	if in.ClientID != "" {
		if _, lookupErr := s.store.OAuthClient(r.Context(), in.ClientID); lookupErr == nil {
			writeError(w, 409, "client_already_registered", "client_id is already registered")
			return
		} else if !errors.Is(lookupErr, sql.ErrNoRows) {
			serverError(w, lookupErr)
			return
		}
	}
	for _, raw := range in.RedirectURIs {
		if !validOAuthRedirectURI(raw) {
			writeError(w, 400, "invalid_redirect_uri", "redirect URIs must be HTTPS, except loopback clients")
			return
		}
	}
	var err error
	id := in.ClientID
	if id == "" {
		id, err = randomOAuthValue(24)
		if err != nil {
			serverError(w, err)
			return
		}
		id = "bd_" + id
	}
	secret := ""
	hash := ""
	if in.Confidential {
		secret, err = randomOAuthValue(32)
		if err != nil {
			serverError(w, err)
			return
		}
		hash = oauthHash(secret)
	}
	if err = s.store.RegisterOAuthClient(r.Context(), id, strings.TrimSpace(in.Name), in.RedirectURIs, hash); err != nil {
		serverError(w, err)
		return
	}
	_ = s.store.OAuthAudit(r.Context(), "client_created", id, "success")
	writeJSON(w, 201, map[string]any{"client_id": id, "client_secret": secret, "client_secret_once": in.Confidential})
}

func (s *Server) rotateOAuthClientSecret(w http.ResponseWriter, r *http.Request, id string) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.oauth.Enabled {
		writeError(w, 404, "not_found", "OAuth is disabled")
		return
	}
	secret, err := randomOAuthValue(32)
	if err != nil {
		serverError(w, err)
		return
	}
	if err = s.store.RotateOAuthClientSecret(r.Context(), id, oauthHash(secret)); err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, 404, "not_found", "client not found")
		case errors.Is(err, store.ErrOAuthPublicClient), errors.Is(err, store.ErrOAuthDisabled):
			writeError(w, 409, "invalid_client_state", err.Error())
		default:
			serverError(w, err)
		}
		return
	}
	_ = s.store.OAuthAudit(r.Context(), "client_secret_rotated", id, "success")
	writeJSON(w, http.StatusOK, map[string]any{"client_id": id, "client_secret": secret, "client_secret_once": true})
}

func (s *Server) ownerOAuthAPI(w http.ResponseWriter, r *http.Request) {
	if id, _ := r.Context().Value(sessionContextKey{}).(string); id == "" {
		writeError(w, 403, "session_required", "OAuth management requires a browser session")
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/oauth/clients":
		items, err := s.store.OAuthClients(r.Context())
		if err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/oauth/clients":
		s.registerOAuthClient(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rotate-secret") && strings.HasPrefix(r.URL.Path, "/api/v1/oauth/clients/"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/oauth/clients/"), "/rotate-secret")
		if id == "" || strings.Contains(id, "/") {
			writeError(w, 404, "not_found", "client not found")
			return
		}
		s.rotateOAuthClientSecret(w, r, id)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/oauth/clients/enable":
		var in struct {
			ClientID string `json:"client_id"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil || in.ClientID == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "client_id is required")
			return
		}
		if err := s.store.EnableOAuthClient(r.Context(), in.ClientID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, 404, "not_found", "client not found")
				return
			}
			serverError(w, err)
			return
		}
		_ = s.store.OAuthAudit(r.Context(), "client_enabled", in.ClientID, "success")
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/oauth/grants":
		items, err := s.store.OAuthGrants(r.Context())
		if err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/oauth/clients/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/oauth/clients/")
		if err := s.store.DisableOAuthClient(r.Context(), id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, 404, "not_found", "client not found")
				return
			}
			serverError(w, err)
			return
		}
		_ = s.store.OAuthAudit(r.Context(), "client_disabled", id, "success")
		writeJSON(w, 200, map[string]bool{"ok": true})
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/oauth/grants/"):
		var id int64
		if _, err := fmt.Sscan(strings.TrimPrefix(r.URL.Path, "/api/v1/oauth/grants/"), &id); err != nil || id < 1 {
			writeError(w, 400, "invalid_id", "invalid grant id")
			return
		}
		if err := s.store.RevokeOAuthGrant(r.Context(), id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, 404, "not_found", "grant not found")
				return
			}
			serverError(w, err)
			return
		}
		_ = s.store.OAuthAudit(r.Context(), "grant_revoked", "", "success")
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		writeError(w, 404, "not_found", "route not found")
	}
}
