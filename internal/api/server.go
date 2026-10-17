package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Nyakooo/grok-bot-dock/internal/events"
	"github.com/Nyakooo/grok-bot-dock/internal/store"
)

type Server struct {
	store            *store.Store
	hub              *events.Hub
	token            string
	maxBytes         int64
	static           embed.FS
	rateMu           sync.Mutex
	rate             map[string]rateWindow
	sessionMu        sync.Mutex
	sessions         map[string]time.Time
	streams          map[string]map[chan struct{}]struct{}
	oauth            OAuthConfig
	consentMu        sync.Mutex
	consents         map[string]pendingConsent
	cimdMu           sync.Mutex
	cimdCache        map[string]cimdCacheEntry
	cimdFetch        chan struct{}
	fetchCIMD        func(context.Context, string) (cimdDocument, time.Duration, error)
	shareCleanupOnce sync.Once
}

const (
	sessionCookie = "bolt_dock_session"
	sessionTTL    = 12 * time.Hour
)

type rateWindow struct {
	started time.Time
	count   int
}

var sourcePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func New(s *store.Store, h *events.Hub, token string, maxBytes int64, static embed.FS) *Server {
	return NewWithOAuth(s, h, token, maxBytes, static, OAuthConfig{})
}
func NewWithOAuth(s *store.Store, h *events.Hub, token string, maxBytes int64, static embed.FS, oauth OAuthConfig) *Server {
	return &Server{store: s, hub: h, token: token, maxBytes: maxBytes, static: static, rate: make(map[string]rateWindow), sessions: make(map[string]time.Time), streams: make(map[string]map[chan struct{}]struct{}), oauth: oauth, consents: make(map[string]pendingConsent), cimdCache: make(map[string]cimdCacheEntry), cimdFetch: make(chan struct{}, 8), fetchCIMD: fetchCIMDMetadata}
}

// ShutdownStreams closes active SSE subscriptions so HTTP graceful shutdown can finish.
func (s *Server) ShutdownStreams() { s.hub.Close() }

func (s *Server) Handler() http.Handler {
	s.shareCleanupOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if _, err := s.store.CleanupReportShares(ctx, time.Now().UTC(), 200); err != nil {
			log.Printf("report share startup cleanup failed: %v", err)
		}
		cancel()
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if _, err := s.store.CleanupReportShares(ctx, time.Now().UTC(), 200); err != nil {
					log.Printf("report share scheduled cleanup failed: %v", err)
				}
				cancel()
			}
		}()
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("GET /api/v1/auth/session", s.sessionStatus)
	mux.HandleFunc("GET /api/v1/public/shares/", s.publicShare)
	mux.Handle("/api/", s.auth(http.HandlerFunc(s.api)))
	mux.Handle("/mcp", s.newMCPHandler())
	if s.oauth.Enabled {
		mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.protectedResourceMetadata)
		mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.protectedResourceMetadata)
		mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.authorizationServerMetadata)
		mux.HandleFunc("GET /oauth/authorize", s.oauthAuthorize)
		mux.HandleFunc("POST /oauth/consent", s.oauthConsent)
		mux.HandleFunc("POST /oauth/token", s.oauthToken)
		mux.HandleFunc("POST /oauth/revoke", s.oauthRevoke)
	}
	mux.HandleFunc("/mcp/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "MCP endpoint is /mcp")
	})
	mux.HandleFunc("/", s.serveStatic)
	return logRequests(mux)
}
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		bearerOK := s.token != "" && len(got) == len(s.token) && subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
		cookie, _ := r.Cookie(sessionCookie)
		sessionOK := cookie != nil && s.validSession(cookie.Value, time.Now())
		if !bearerOK && !sessionOK {
			writeError(w, 401, "unauthorized", "valid bearer token required")
			return
		}
		if sessionOK {
			if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && !s.isSameSiteOrigin(r) {
				writeError(w, http.StatusForbidden, "invalid_origin", "session request origin must match this site")
				return
			}
			ctx := context.WithValue(r.Context(), sessionContextKey{}, cookie.Value)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

type sessionContextKey struct{}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.allowLogin(r.RemoteAddr, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts; retry later")
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil || in.Token == "" || len(in.Token) != len(s.token) || subtle.ConstantTimeCompare([]byte(in.Token), []byte(s.token)) != 1 {
		writeError(w, http.StatusUnauthorized, "unauthorized", "access token is invalid")
		return
	}
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not create session")
		return
	}
	id := base64.RawURLEncoding.EncodeToString(data)
	now := time.Now()
	s.sessionMu.Lock()
	for existing, expiry := range s.sessions {
		if !now.Before(expiry) {
			s.revokeSessionLocked(existing)
		}
	}
	s.sessions[id] = now.Add(sessionTTL)
	s.sessionMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: id, Path: "/", HttpOnly: true, Secure: s.publicRequestIsHTTPS(r), SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) sessionStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cookie, err := r.Cookie(sessionCookie)
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": err == nil && s.validSession(cookie.Value, time.Now())})
}

func (s *Server) validSession(id string, now time.Time) bool {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	expires, ok := s.sessions[id]
	if ok && !now.Before(expires) {
		s.revokeSessionLocked(id)
		return false
	}
	return ok
}

func (s *Server) revokeSession(id string) {
	s.sessionMu.Lock()
	s.revokeSessionLocked(id)
	s.sessionMu.Unlock()
}

func (s *Server) revokeSessionLocked(id string) {
	delete(s.sessions, id)
	for stream := range s.streams[id] {
		close(stream)
	}
	delete(s.streams, id)
}

func (s *Server) registerSessionStream(id string, stream chan struct{}) bool {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	expires, ok := s.sessions[id]
	if !ok || !time.Now().Before(expires) {
		s.revokeSessionLocked(id)
		return false
	}
	if s.streams[id] == nil {
		s.streams[id] = make(map[chan struct{}]struct{})
	}
	s.streams[id][stream] = struct{}{}
	return true
}

func (s *Server) unregisterSessionStream(id string, stream chan struct{}) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if streams := s.streams[id]; streams != nil {
		delete(streams, stream)
		if len(streams) == 0 {
			delete(s.streams, id)
		}
	}
}

func (s *Server) allowLogin(remote string, now time.Time) bool {
	key := remote
	if host, _, err := net.SplitHostPort(remote); err == nil {
		key = host
	}
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	window := s.rate["login:"+key]
	if window.started.IsZero() || now.Sub(window.started) >= time.Minute {
		window = rateWindow{started: now}
	}
	if window.count >= 10 {
		s.rate["login:"+key] = window
		return false
	}
	window.count++
	s.rate["login:"+key] = window
	return true
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

// isSameSiteOrigin uses the configured public origin when OAuth has a fixed
// canonical URL. Reverse proxies may send the app an internal Host/scheme;
// comparing Origin against those backend values rejects legitimate browser
// session writes even though the browser is on the configured service origin.
func (s *Server) isSameSiteOrigin(r *http.Request) bool {
	if s.oauth.Enabled && s.validOAuthConfig() {
		return s.originIsPublicBase(r)
	}
	return sameOrigin(r)
}

// publicRequestIsHTTPS derives cookie security from the fixed external origin
// when available, so an omitted X-Forwarded-Proto cannot create a non-Secure
// session cookie behind a TLS-terminating proxy.
func (s *Server) publicRequestIsHTTPS(r *http.Request) bool {
	if s.oauth.Enabled && s.validOAuthConfig() {
		publicURL, err := url.Parse(s.oauth.PublicBaseURL)
		return err == nil && publicURL.Scheme == "https"
	}
	return requestIsHTTPS(r)
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	return strings.EqualFold(u.Host, r.Host) && strings.EqualFold(u.Scheme, scheme)
}
func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/v1/topics" || strings.HasPrefix(r.URL.Path, "/api/v1/topics/"):
		s.topicsAPI(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/logout":
		w.Header().Set("Cache-Control", "no-store")
		if id, _ := r.Context().Value(sessionContextKey{}).(string); id != "" {
			s.revokeSession(id)
		}
		secure := s.publicRequestIsHTTPS(r)
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/mcp/capabilities":
		writeJSON(w, http.StatusOK, map[string]any{
			"endpoint":  "/mcp",
			"transport": "Streamable HTTP",
			"authentication": func() string {
				if s.oauth.Enabled {
					return "OAuth Authorization Code + PKCE S256 profile (limited OAuth 2.1 security baseline); legacy Bearer remains supported"
				}
				return "Authorization: Bearer <ACCESS_TOKEN>"
			}(),
			"oauth_enabled": s.oauth.Enabled,
			"access_model":  "仅供服务所有者本人或受信任的单用户客户端；共享密钥不支持按客户端授权、撤销或多租户隔离",
			"tools":         capabilityMetadata(s.maxBytes),
		})
	case strings.HasPrefix(r.URL.Path, "/api/v1/oauth/"):
		s.ownerOAuthAPI(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/v1/reports/") && strings.Contains(r.URL.Path, "/shares"):
		s.manageReportShares(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/v1/reports":
		s.createReport(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/v1/reports":
		s.reports(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/v1/sources":
		s.sources(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/v1/bots":
		s.bots(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/v1/search":
		s.search(w, r)
	case r.Method == "GET" && r.URL.Path == "/api/v1/events":
		s.sse(w, r)
	case r.Method == "POST" && r.URL.Path == "/api/v1/bots/read":
		s.markRead(w, r)
	case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/bots":
		s.renameBot(w, r)
	case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/bots":
		s.deleteBot(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/reports/"):
		s.deleteReport(w, r)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/reports/"):
		s.report(w, r)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/bots/"):
		s.botReports(w, r)
	default:
		writeError(w, 404, "not_found", "route not found")
	}
}
func (s *Server) createReport(w http.ResponseWriter, r *http.Request) {
	if allowed, wait := s.allowWrite(r.RemoteAddr, time.Now()); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(wait.Seconds()))))
		writeError(w, 429, "rate_limited", "too many report pushes; retry later")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var raw reportRequest
	if err := dec.Decode(&raw); err != nil {
		if isTooLarge(err) {
			writeError(w, 413, "payload_too_large", "request body exceeds configured limit")
		} else {
			writeError(w, 400, "invalid_json", err.Error())
		}
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json", "request must contain one JSON object")
		return
	}
	in, err := normalizeReportRequest(raw)
	if err != nil {
		writeValidationError(w, err)
		return
	}
	report, duplicate, err := s.publishReport(r.Context(), in)
	if err != nil {
		if e, ok := err.(*apiError); ok {
			if e.status == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", strconv.Itoa(max(1, e.retryAfter)))
			}
			writeError(w, e.status, e.code, e.message)
			return
		}
		log.Printf("create report failed: %v", err)
		writeError(w, 500, "internal_error", "could not save report")
		return
	}
	status := 201
	if duplicate {
		status = 200
	}
	writeJSON(w, status, map[string]any{"report_id": report.ID, "source": report.Source, "bot_id": report.BotID, "topic_id": report.TopicID, "duplicate": duplicate})
}

type reportRequest struct {
	TopicID      string   `json:"topic_id"`
	TopicName    string   `json:"topic_name"`
	Source       string   `json:"source"`
	BotID        string   `json:"bot_id"`
	BotName      string   `json:"bot_name"`
	StableTaskID string   `json:"stable_task_id"`
	TaskName     string   `json:"task_name"`
	ExecutedAt   string   `json:"executed_at"`
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Tags         []string `json:"tags"`
	ExternalID   string   `json:"external_id"`
	AvatarURL    string   `json:"avatar_url"`
	Content      string   `json:"content"`
}

type apiError struct {
	status     int
	code       string
	message    string
	retryAfter int
}

func (e *apiError) Error() string { return e.message }

func writeValidationError(w http.ResponseWriter, err error) {
	if e, ok := err.(*apiError); ok {
		writeError(w, e.status, e.code, e.message)
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_request", "request is invalid")
}

func normalizeReportRequest(raw reportRequest) (store.ReportInput, error) {
	raw.TopicID = strings.TrimSpace(raw.TopicID)
	raw.TopicName = strings.TrimSpace(raw.TopicName)
	if (raw.TopicID != "" && !topicIDPattern.MatchString(raw.TopicID)) || len(raw.TopicName) > 256 {
		return store.ReportInput{}, &apiError{status: 400, code: "invalid_topic", message: "topic_id must be a stable URL-safe identifier (1-128 bytes); topic_name is limited to 256 bytes"}
	}
	raw.Source = strings.TrimSpace(raw.Source)
	if raw.Source == "" {
		raw.Source = "unknown"
	}
	if !sourcePattern.MatchString(raw.Source) {
		return store.ReportInput{}, &apiError{status: 400, code: "invalid_source", message: "source must be lowercase letters, digits, or hyphens"}
	}
	raw.BotID = strings.TrimSpace(raw.BotID)
	raw.StableTaskID = strings.TrimSpace(raw.StableTaskID)
	raw.TaskName = strings.TrimSpace(raw.TaskName)
	if raw.BotID == "" {
		if raw.StableTaskID == "" || len(raw.StableTaskID) > 512 {
			return store.ReportInput{}, &apiError{status: 400, code: "invalid_bot", message: "bot_id or a stable_task_id is required; a stable task identity is needed to keep reports under one Bot"}
		}
		digest := sha256.Sum256([]byte(raw.Source + "\x00" + raw.StableTaskID))
		raw.BotID = "task-" + hex.EncodeToString(digest[:12])
	}
	if len(raw.BotID) > 128 {
		return store.ReportInput{}, &apiError{status: 400, code: "invalid_bot", message: "bot_id must be at most 128 characters"}
	}
	if strings.TrimSpace(raw.BotName) == "" {
		raw.BotName = raw.TaskName
	}
	if strings.TrimSpace(raw.BotName) == "" {
		raw.BotName = raw.BotID
	}
	if len(raw.BotName) > 256 {
		return store.ReportInput{}, &apiError{status: 400, code: "invalid_bot", message: "bot_name must be at most 256 characters"}
	}
	at, err := time.Parse(time.RFC3339, raw.ExecutedAt)
	if err != nil {
		return store.ReportInput{}, &apiError{status: 400, code: "invalid_executed_at", message: "executed_at must be RFC3339"}
	}
	if strings.TrimSpace(raw.Content) == "" {
		return store.ReportInput{}, &apiError{status: 400, code: "empty_content", message: "content must not be empty"}
	}
	if len(raw.Title) > 512 || len(raw.Summary) > 4096 || len(raw.ExternalID) > 256 || len(raw.Tags) > 32 {
		return store.ReportInput{}, &apiError{status: 400, code: "invalid_metadata", message: "title, summary, external_id, or tags exceed limits"}
	}
	for _, tag := range raw.Tags {
		if len(tag) > 128 {
			return store.ReportInput{}, &apiError{status: 400, code: "invalid_tags", message: "tag exceeds 128 bytes"}
		}
	}
	if raw.AvatarURL != "" {
		u, e := http.NewRequest("GET", raw.AvatarURL, nil)
		if e != nil || u.URL.Scheme != "https" || u.URL.Host == "" || len(raw.AvatarURL) > 2048 {
			return store.ReportInput{}, &apiError{status: 400, code: "invalid_avatar_url", message: "avatar_url must be a valid HTTPS URL"}
		}
	}
	if raw.Title == "" {
		raw.Title = firstHeading(raw.Content)
	}
	if raw.Summary == "" {
		raw.Summary = firstText(raw.Content, 280)
	}
	in := store.ReportInput{TopicID: raw.TopicID, TopicName: raw.TopicName, Source: raw.Source, BotID: raw.BotID, BotName: raw.BotName, ExecutedAt: at, Title: raw.Title, Summary: raw.Summary, Tags: raw.Tags, ExternalID: raw.ExternalID, AvatarURL: raw.AvatarURL, Content: raw.Content}
	return in, nil
}

func (s *Server) publishReport(ctx context.Context, in store.ReportInput) (store.Report, bool, error) {
	report, duplicate, err := s.store.CreateReport(ctx, in)
	if err != nil {
		if errors.Is(err, store.ErrTopicConflict) || errors.Is(err, store.ErrTopicStopped) {
			return store.Report{}, false, &apiError{status: 409, code: "topic_unavailable", message: err.Error()}
		}
		return store.Report{}, false, err
	}
	if !duplicate {
		s.hub.Publish("report.created", report)
	}
	return report, duplicate, nil
}

func (s *Server) allowWrite(remote string, now time.Time) (bool, time.Duration) {
	key := remote
	if host, _, err := net.SplitHostPort(remote); err == nil {
		key = host
	}
	return s.allowRate(key, 120, now)
}

func (s *Server) allowRate(key string, limit int, now time.Time) (bool, time.Duration) {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	for ip, window := range s.rate {
		if now.Sub(window.started) >= time.Minute {
			delete(s.rate, ip)
		}
	}
	window := s.rate[key]
	if window.started.IsZero() || now.Sub(window.started) >= time.Minute {
		window = rateWindow{started: now}
	}
	if window.count >= limit {
		s.rate[key] = window
		return false, time.Minute - now.Sub(window.started)
	}
	window.count++
	s.rate[key] = window
	return true, 0
}
func (s *Server) sources(w http.ResponseWriter, r *http.Request) {
	x, e := s.store.Sources(r.Context())
	if e != nil {
		serverError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": x})
}
func (s *Server) bots(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	x, e := s.store.Bots(r.Context(), source)
	if e != nil {
		serverError(w, e)
		return
	}
	b, n, size, e := s.store.Stats(r.Context(), source)
	if e != nil {
		serverError(w, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": x, "stats": map[string]any{"bot_count": b, "report_count": n, "database_bytes": size, "database_scope": "all"}})
}
func (s *Server) reports(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := queryInt(q.Get("limit"), 20)
	p, e := s.store.ListReports(r.Context(), q.Get("source"), q.Get("bot_id"), q.Get("date"), q.Get("cursor"), limit)
	if e != nil {
		writeError(w, 400, "invalid_query", e.Error())
		return
	}
	writeJSON(w, 200, p)
}
func (s *Server) botReports(w http.ResponseWriter, r *http.Request) {
	tail := strings.TrimPrefix(r.URL.Path, "/api/v1/bots/")
	parts := strings.Split(tail, "/")
	if len(parts) != 2 || parts[1] != "reports" {
		writeError(w, 404, "not_found", "route not found")
		return
	}
	q := r.URL.Query()
	p, e := s.store.ListReports(r.Context(), q.Get("source"), parts[0], q.Get("date"), q.Get("cursor"), queryInt(q.Get("limit"), 20))
	if e != nil {
		writeError(w, 400, "invalid_query", e.Error())
		return
	}
	writeJSON(w, 200, p)
}
func (s *Server) markRead(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Source   string `json:"source"`
		BotID    string `json:"bot_id"`
		ReportID int64  `json:"report_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Source == "" || in.BotID == "" || in.ReportID < 1 {
		writeError(w, 400, "invalid_request", "source, bot_id, and positive report_id are required")
		return
	}
	if err := s.store.MarkRead(r.Context(), in.Source, in.BotID, in.ReportID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, 404, "not_found", "report not found for this bot")
			return
		}
		serverError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	id, e := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v1/reports/"), 10, 64)
	if e != nil || id < 1 {
		writeError(w, 400, "invalid_id", "report id must be a positive integer")
		return
	}
	x, e := s.store.GetReport(r.Context(), id)
	if errors.Is(e, sql.ErrNoRows) {
		writeError(w, 404, "not_found", "report not found")
		return
	}
	if e != nil {
		serverError(w, e)
		return
	}
	writeJSON(w, 200, x)
}

func (s *Server) deleteReport(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v1/reports/"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, 400, "invalid_id", "report id must be a positive integer")
		return
	}
	report, err := s.store.DeleteReport(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "not_found", "report not found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	s.hub.Publish("report.deleted", map[string]any{"id": report.ID, "source": report.Source, "bot_id": report.BotID})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) deleteBot(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	source, botID := q.Get("source"), q.Get("bot_id")
	if !sourcePattern.MatchString(source) || strings.TrimSpace(botID) == "" || len(botID) > 128 {
		writeError(w, 400, "invalid_bot", "valid source and bot_id are required")
		return
	}
	reports, err := s.store.DeleteBot(r.Context(), source, botID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "not_found", "bot not found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	s.hub.Publish("bot.deleted", map[string]any{"source": source, "bot_id": botID, "deleted_reports": reports})
	writeJSON(w, 200, map[string]any{"ok": true, "deleted_reports": reports})
}

func (s *Server) renameBot(w http.ResponseWriter, r *http.Request) {
	if id, _ := r.Context().Value(sessionContextKey{}).(string); id == "" {
		writeError(w, http.StatusForbidden, "session_required", "Bot display names can only be changed from a browser session")
		return
	}
	q := r.URL.Query()
	source, botID := q.Get("source"), q.Get("bot_id")
	if !sourcePattern.MatchString(source) || strings.TrimSpace(botID) == "" || len(botID) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_bot", "valid source and bot_id are required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in struct {
		DisplayName string `json:"display_name"`
	}
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "display_name is required; use an empty string to restore the original name")
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_json", "request must contain one JSON object")
		return
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if len([]rune(in.DisplayName)) > 80 || strings.IndexFunc(in.DisplayName, func(r rune) bool { return r < 0x20 && r != '\t' }) >= 0 {
		writeError(w, http.StatusBadRequest, "invalid_display_name", "display_name must be at most 80 characters and contain no control characters")
		return
	}
	if err := s.store.RenameBot(r.Context(), source, botID, in.DisplayName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "bot not found")
			return
		}
		serverError(w, err)
		return
	}
	s.hub.Publish("bot.updated", map[string]string{"source": source, "bot_id": botID})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	term := strings.TrimSpace(q.Get("q"))
	if term == "" || len(term) > 256 {
		writeError(w, 400, "invalid_query", "q must contain 1 to 256 characters")
		return
	}
	from, err := dateBound(q.Get("from"), false)
	if err != nil {
		writeError(w, 400, "invalid_date", "from must be an ISO date (YYYY-MM-DD)")
		return
	}
	to, err := dateBound(q.Get("to"), true)
	if err != nil {
		writeError(w, 400, "invalid_date", "to must be an ISO date (YYYY-MM-DD)")
		return
	}
	tab := q.Get("topic_tab")
	if tab != "" && tab != "all" && tab != "follow" && tab != "updates" && tab != "archived" {
		writeError(w, 400, "invalid_query", "invalid topic_tab")
		return
	}
	if len(q.Get("topic_group")) > 128 {
		writeError(w, 400, "invalid_query", "topic_group too long")
		return
	}
	p, e := s.store.SearchScoped(r.Context(), term, q.Get("source"), q.Get("bot_id"), from, to, q.Get("cursor"), queryInt(q.Get("limit"), 20), store.TopicSearchScope{Tab: tab, Group: q.Get("topic_group")})
	if e != nil {
		serverError(w, e)
		return
	}
	writeJSON(w, 200, p)
}

func dateBound(value string, exclusiveEnd bool) (string, error) {
	if value == "" {
		return "", nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", err
	}
	if exclusiveEnd {
		parsed = parsed.AddDate(0, 0, 1)
	}
	return parsed.UTC().Format("2006-01-02T15:04:05.000000000Z"), nil
}
func (s *Server) sse(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "stream_unsupported", "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(ch)
	var revoked <-chan struct{}
	if id, _ := r.Context().Value(sessionContextKey{}).(string); id != "" {
		streamClosed := make(chan struct{})
		if !s.registerSessionStream(id, streamClosed) {
			return
		}
		defer s.unregisterSessionStream(id, streamClosed)
		revoked = streamClosed
	}
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	_, _ = io.WriteString(w, "retry: 3000\n\n")
	f.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-revoked:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if id, _ := r.Context().Value(sessionContextKey{}).(string); id != "" && !s.validSession(id, time.Now()) {
				return
			}
			if _, e := w.Write(msg); e != nil {
				return
			}
			f.Flush()
		case <-ticker.C:
			if id, _ := r.Context().Value(sessionContextKey{}).(string); id != "" && !s.validSession(id, time.Now()) {
				return
			}
			if _, e := io.WriteString(w, ": heartbeat\n\n"); e != nil {
				return
			}
			f.Flush()
		}
	}
}
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/share/") {
		setShareResponseHeaders(w)
	}
	name := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if name == "." || name == "" {
		name = "index.html"
	}
	file := "dist/" + name
	data, err := s.static.ReadFile(file)
	if err != nil {
		data, err = s.static.ReadFile("dist/index.html")
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if typ := mime.TypeByExtension(path.Ext(name)); typ != "" {
		w.Header().Set("Content-Type", typ)
	}
	_, _ = w.Write(data)
}
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		requestPath := r.URL.Path
		if strings.HasPrefix(requestPath, "/share/") {
			requestPath = "/share/[REDACTED]"
		} else if strings.HasPrefix(requestPath, "/api/v1/public/shares/") {
			requestPath = "/api/v1/public/shares/[REDACTED]"
		}
		log.Printf("%s %s %s", r.Method, requestPath, time.Since(start).Round(time.Millisecond))
	})
}
func firstHeading(s string) string {
	for _, line := range strings.Split(s, "\n") {
		v := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if len(v) > 0 && len(v) < len(line)+1 && strings.HasPrefix(strings.TrimSpace(line), "#") {
			return v
		}
	}
	return "Untitled report"
}
func firstText(s string, n int) string {
	for _, line := range strings.Split(s, "\n") {
		v := strings.TrimSpace(line)
		if v != "" && !strings.HasPrefix(v, "#") && !strings.HasPrefix(v, "```") {
			rr := []rune(v)
			if len(rr) > n {
				return string(rr[:n]) + "…"
			}
			return v
		}
	}
	return ""
}
func isTooLarge(err error) bool { var m *http.MaxBytesError; return errors.As(err, &m) }
func queryInt(v string, def int) int {
	n, e := strconv.Atoi(v)
	if e != nil || n < 1 {
		return def
	}
	return n
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func serverError(w http.ResponseWriter, e error) {
	log.Printf("request failed: %v", e)
	writeError(w, 500, "internal_error", "request failed")
}
