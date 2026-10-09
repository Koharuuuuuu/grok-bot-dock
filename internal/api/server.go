package api

import (
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
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
	store    *store.Store
	hub      *events.Hub
	token    string
	maxBytes int64
	static   embed.FS
	rateMu   sync.Mutex
	rate     map[string]rateWindow
}

type rateWindow struct {
	started time.Time
	count   int
}

var sourcePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func New(s *store.Store, h *events.Hub, token string, maxBytes int64, static embed.FS) *Server {
	return &Server{store: s, hub: h, token: token, maxBytes: maxBytes, static: static, rate: make(map[string]rateWindow)}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.Handle("/api/", s.auth(http.HandlerFunc(s.api)))
	mux.HandleFunc("/", s.serveStatic)
	return logRequests(mux)
}
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.token == "" || len(got) != len(s.token) || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeError(w, 401, "unauthorized", "valid bearer token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	switch {
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
	var raw struct {
		Source     string   `json:"source"`
		BotID      string   `json:"bot_id"`
		BotName    string   `json:"bot_name"`
		ExecutedAt string   `json:"executed_at"`
		Title      string   `json:"title"`
		Summary    string   `json:"summary"`
		Tags       []string `json:"tags"`
		ExternalID string   `json:"external_id"`
		AvatarURL  string   `json:"avatar_url"`
		Content    string   `json:"content"`
	}
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
		writeError(w, 400, "invalid_json", "request must contain one JSON object")
		return
	}
	if !sourcePattern.MatchString(raw.Source) {
		writeError(w, 400, "invalid_source", "source must be lowercase letters, digits, or hyphens")
		return
	}
	if strings.TrimSpace(raw.BotID) == "" || len(raw.BotID) > 128 || strings.TrimSpace(raw.BotName) == "" || len(raw.BotName) > 256 {
		writeError(w, 400, "invalid_bot", "bot_id and bot_name are required and must fit the length limits")
		return
	}
	at, err := time.Parse(time.RFC3339, raw.ExecutedAt)
	if err != nil {
		writeError(w, 400, "invalid_executed_at", "executed_at must be RFC3339")
		return
	}
	if strings.TrimSpace(raw.Content) == "" {
		writeError(w, 400, "empty_content", "content must not be empty")
		return
	}
	if len(raw.Title) > 512 || len(raw.Summary) > 4096 || len(raw.ExternalID) > 256 || len(raw.Tags) > 32 {
		writeError(w, 400, "invalid_metadata", "title, summary, external_id, or tags exceed limits")
		return
	}
	for _, tag := range raw.Tags {
		if len(tag) > 128 {
			writeError(w, 400, "invalid_tags", "tag exceeds 128 bytes")
			return
		}
	}
	if raw.AvatarURL != "" {
		u, e := http.NewRequest("GET", raw.AvatarURL, nil)
		if e != nil || u.URL.Scheme != "https" || u.URL.Host == "" || len(raw.AvatarURL) > 2048 {
			writeError(w, 400, "invalid_avatar_url", "avatar_url must be a valid HTTPS URL")
			return
		}
	}
	if raw.Title == "" {
		raw.Title = firstHeading(raw.Content)
	}
	if raw.Summary == "" {
		raw.Summary = firstText(raw.Content, 280)
	}
	in := store.ReportInput{Source: raw.Source, BotID: raw.BotID, BotName: raw.BotName, ExecutedAt: at, Title: raw.Title, Summary: raw.Summary, Tags: raw.Tags, ExternalID: raw.ExternalID, AvatarURL: raw.AvatarURL, Content: raw.Content}
	report, duplicate, err := s.store.CreateReport(r.Context(), in)
	if err != nil {
		log.Printf("create report failed: %v", err)
		writeError(w, 500, "internal_error", "could not save report")
		return
	}
	status := 201
	if duplicate {
		status = 200
	} else {
		s.hub.Publish("report.created", report)
	}
	writeJSON(w, status, map[string]any{"report_id": report.ID, "source": report.Source, "bot_id": report.BotID, "duplicate": duplicate})
}

func (s *Server) allowWrite(remote string, now time.Time) (bool, time.Duration) {
	key := remote
	if host, _, err := net.SplitHostPort(remote); err == nil {
		key = host
	}
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
	if window.count >= 120 {
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
	p, e := s.store.Search(r.Context(), term, q.Get("source"), q.Get("bot_id"), from, to, q.Get("cursor"), queryInt(q.Get("limit"), 20))
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
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	_, _ = io.WriteString(w, "retry: 3000\n\n")
	f.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, e := w.Write(msg); e != nil {
				return
			}
			f.Flush()
		case <-ticker.C:
			if _, e := io.WriteString(w, ": heartbeat\n\n"); e != nil {
				return
			}
			f.Flush()
		}
	}
}
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
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
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
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
