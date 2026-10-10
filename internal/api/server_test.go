package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Nyakooo/grok-bot-dock/internal/events"
	"github.com/Nyakooo/grok-bot-dock/internal/store"
	"github.com/Nyakooo/grok-bot-dock/internal/webui"
)

func testHandler(t *testing.T, max int64) (http.Handler, *store.Store, *events.Hub) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	hub := events.New()
	return New(db, hub, "test-secret", max, webui.Files).Handler(), db, hub
}
func request(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}
func postBody(source, content, external string) string {
	body, _ := json.Marshal(map[string]any{"source": source, "bot_id": "daily", "bot_name": "Daily", "executed_at": "2026-10-09T20:00:00+08:00", "external_id": external, "content": content})
	return string(body)
}

func TestBrowserSessionCookieLoginRefreshAndLogout(t *testing.T) {
	h, _, _ := testHandler(t, 1<<20)
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"token":"test-secret"}`))
	login.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	h.ServeHTTP(loginRecorder, login)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login: %d %s", loginRecorder.Code, loginRecorder.Body.String())
	}
	cookies := loginRecorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one session cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookie || !cookie.HttpOnly || cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge != 0 || cookie.Expires.Year() != 1 {
		t.Fatalf("unexpected session cookie attributes: %#v", cookie)
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	sessionReq.AddCookie(cookie)
	sessionRecorder := httptest.NewRecorder()
	h.ServeHTTP(sessionRecorder, sessionReq)
	if sessionRecorder.Code != http.StatusOK || !strings.Contains(sessionRecorder.Body.String(), `"authenticated":true`) {
		t.Fatalf("session probe did not restore login state: %d %s", sessionRecorder.Code, sessionRecorder.Body.String())
	}
	for _, endpoint := range []string{"/api/v1/bots"} {
		req := httptest.NewRequest(http.MethodGet, endpoint, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("session did not survive page/API request to %s: %d %s", endpoint, w.Code, w.Body.String())
		}
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.AddCookie(cookie)
	logout.Header.Set("Origin", "http://example.com")
	logout.Header.Set("Host", "example.com")
	logoutRecorder := httptest.NewRecorder()
	h.ServeHTTP(logoutRecorder, logout)
	if logoutRecorder.Code != http.StatusOK {
		t.Fatalf("logout: %d %s", logoutRecorder.Code, logoutRecorder.Body.String())
	}
	cleared := logoutRecorder.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge != -1 || cleared[0].Path != "/" || !cleared[0].HttpOnly {
		t.Fatalf("session cookie was not cleared with matching attributes: %#v", cleared)
	}
	for _, endpoint := range []string{"/api/v1/bots"} {
		req := httptest.NewRequest(http.MethodGet, endpoint, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("revoked session still accesses %s: %d", endpoint, w.Code)
		}
	}
	loggedOutProbe := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	loggedOutProbe.AddCookie(cookie)
	loggedOutStatus := httptest.NewRecorder()
	h.ServeHTTP(loggedOutStatus, loggedOutProbe)
	if loggedOutStatus.Code != http.StatusOK || !strings.Contains(loggedOutStatus.Body.String(), `"authenticated":false`) {
		t.Fatalf("session probe did not report revoked cookie: %d %s", loggedOutStatus.Code, loggedOutStatus.Body.String())
	}
	if got := request(h, http.MethodGet, "/api/v1/bots", "", "test-secret"); got.Code != http.StatusOK {
		t.Fatalf("Bearer API compatibility regressed: %d %s", got.Code, got.Body.String())
	}
}

func TestSessionCookieSecureBehindHTTPSProxyAndRejectsForeignOrigin(t *testing.T) {
	h, _, _ := testHandler(t, 1<<20)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"token":"test-secret"}`))
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure {
		t.Fatalf("HTTPS proxy session cookie is not Secure: %#v", cookie)
	}
	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.AddCookie(cookie)
	logout.Header.Set("Origin", "https://attacker.example")
	logout.Header.Set("X-Forwarded-Proto", "https")
	rejected := httptest.NewRecorder()
	h.ServeHTTP(rejected, logout)
	if rejected.Code != http.StatusForbidden {
		t.Fatalf("foreign origin was not rejected: %d %s", rejected.Code, rejected.Body.String())
	}
}

func TestExpiredSessionAndInvalidPasswordAreRejected(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "session.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	server := New(db, events.New(), "test-secret", 1<<20, webui.Files)
	h := server.Handler()
	badLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"token":"wrong"}`))
	badLoginRecorder := httptest.NewRecorder()
	h.ServeHTTP(badLoginRecorder, badLogin)
	if badLoginRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("invalid access token was accepted: %d", badLoginRecorder.Code)
	}
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"token":"test-secret"}`))
	loginRecorder := httptest.NewRecorder()
	h.ServeHTTP(loginRecorder, login)
	cookie := loginRecorder.Result().Cookies()[0]
	server.sessionMu.Lock()
	server.sessions[cookie.Value] = time.Now().Add(-time.Second)
	server.sessionMu.Unlock()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/bots", nil)
	request.AddCookie(cookie)
	rejected := httptest.NewRecorder()
	h.ServeHTTP(rejected, request)
	if rejected.Code != http.StatusUnauthorized {
		t.Fatalf("expired session still accesses protected data: %d", rejected.Code)
	}
}

func TestReportPushAndReadFlow(t *testing.T) {
	h, _, _ := testHandler(t, 1<<20)
	if got := request(h, "POST", "/api/v1/reports", postBody("grok", "# AI\n\n人工智能摘要", "g1"), ""); got.Code != 401 {
		t.Fatalf("missing token status=%d", got.Code)
	}
	for _, source := range []string{"grok", "codex"} {
		got := request(h, "POST", "/api/v1/reports", postBody(source, "# AI\n\n人工智能摘要", source+"-1"), "test-secret")
		if got.Code != 201 {
			t.Fatalf("push %s: status=%d body=%s", source, got.Code, got.Body.String())
		}
	}
	dup := request(h, "POST", "/api/v1/reports", postBody("grok", "different body", "grok-1"), "test-secret")
	if dup.Code != 200 || !bytes.Contains(dup.Body.Bytes(), []byte(`"duplicate":true`)) {
		t.Fatalf("idempotent retry: %d %s", dup.Code, dup.Body.String())
	}
	sources := request(h, "GET", "/api/v1/sources", "", "test-secret")
	if sources.Code != 200 || !bytes.Contains(sources.Body.Bytes(), []byte(`"source":"grok"`)) || !bytes.Contains(sources.Body.Bytes(), []byte(`"source":"codex"`)) {
		t.Fatalf("sources: %d %s", sources.Code, sources.Body.String())
	}
	filtered := request(h, "GET", "/api/v1/bots?source=grok", "", "test-secret")
	if filtered.Code != 200 || !bytes.Contains(filtered.Body.Bytes(), []byte(`"bot_count":1`)) || !bytes.Contains(filtered.Body.Bytes(), []byte(`"report_count":1`)) {
		t.Fatalf("filtered stats: %d %s", filtered.Code, filtered.Body.String())
	}
	search := request(h, "GET", "/api/v1/search?q=%E4%BA%BA%E5%B7%A5%E6%99%BA%E8%83%BD&source=codex", "", "test-secret")
	if search.Code != 200 || !bytes.Contains(search.Body.Bytes(), []byte(`"source":"codex"`)) {
		t.Fatalf("search: %d %s", search.Code, search.Body.String())
	}
	dateSearch := request(h, "GET", "/api/v1/search?q=人工智能&from=2026-10-09&to=2026-10-09", "", "test-secret")
	if dateSearch.Code != 200 || !bytes.Contains(dateSearch.Body.Bytes(), []byte(`"source":"grok"`)) || !bytes.Contains(dateSearch.Body.Bytes(), []byte(`"source":"codex"`)) {
		t.Fatalf("inclusive date search: %d %s", dateSearch.Code, dateSearch.Body.String())
	}
	badDate := request(h, "GET", "/api/v1/search?q=人工智能&from=2026-99-99", "", "test-secret")
	if badDate.Code != 400 {
		t.Fatalf("invalid date status=%d body=%s", badDate.Code, badDate.Body.String())
	}
	index := request(h, "GET", "/", "", "")
	if index.Code != 200 || !bytes.Contains(index.Body.Bytes(), []byte("Bolt Dock")) {
		t.Fatalf("static app: %d", index.Code)
	}
}

func TestDeleteReportAndBotRequiresAuthAndPublishesChanges(t *testing.T) {
	h, db, hub := testHandler(t, 1<<20)
	created := request(h, http.MethodPost, "/api/v1/reports", postBody("grok", "# removable-report-token", "r1"), "test-secret")
	if created.Code != http.StatusCreated {
		t.Fatalf("create report: %d %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		ReportID int64 `json:"report_id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil || createdBody.ReportID < 1 {
		t.Fatalf("invalid create response: %#v err=%v", createdBody, err)
	}
	if got := request(h, http.MethodDelete, "/api/v1/reports/"+strconv.FormatInt(createdBody.ReportID, 10), "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated delete report status=%d", got.Code)
	}
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)
	deleted := request(h, http.MethodDelete, "/api/v1/reports/"+strconv.FormatInt(createdBody.ReportID, 10), "", "test-secret")
	if deleted.Code != http.StatusOK || !bytes.Contains(deleted.Body.Bytes(), []byte(`"ok":true`)) {
		t.Fatalf("delete report: %d %s", deleted.Code, deleted.Body.String())
	}
	if event := string(<-ch); !strings.Contains(event, "event: report.deleted") || !strings.Contains(event, `"id":`+strconv.FormatInt(createdBody.ReportID, 10)) {
		t.Fatalf("unexpected delete event: %s", event)
	}
	if got := request(h, http.MethodGet, "/api/v1/reports/"+strconv.FormatInt(createdBody.ReportID, 10), "", "test-secret"); got.Code != http.StatusNotFound {
		t.Fatalf("deleted report still exists: %d %s", got.Code, got.Body.String())
	}
	if got := request(h, http.MethodDelete, "/api/v1/reports/"+strconv.FormatInt(createdBody.ReportID, 10), "", "test-secret"); got.Code != http.StatusNotFound {
		t.Fatalf("deleting a missing report status=%d", got.Code)
	}

	second := request(h, http.MethodPost, "/api/v1/reports", postBody("grok", "# bot-cascade-token", "r2"), "test-secret")
	if second.Code != http.StatusCreated {
		t.Fatalf("create second report: %d %s", second.Code, second.Body.String())
	}
	if event := string(<-ch); !strings.Contains(event, "event: report.created") {
		t.Fatalf("unexpected create event: %s", event)
	}
	botDeletePath := "/api/v1/bots?source=grok&bot_id=daily"
	if got := request(h, http.MethodDelete, botDeletePath, "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated delete bot status=%d", got.Code)
	}
	botDeleted := request(h, http.MethodDelete, botDeletePath, "", "test-secret")
	if botDeleted.Code != http.StatusOK || !bytes.Contains(botDeleted.Body.Bytes(), []byte(`"deleted_reports":1`)) {
		t.Fatalf("delete bot and cascade: %d %s", botDeleted.Code, botDeleted.Body.String())
	}
	if event := string(<-ch); !strings.Contains(event, "event: bot.deleted") || !strings.Contains(event, `"bot_id":"daily"`) {
		t.Fatalf("unexpected bot delete event: %s", event)
	}
	bots := request(h, http.MethodGet, "/api/v1/bots", "", "test-secret")
	if !bytes.Contains(bots.Body.Bytes(), []byte(`"bot_count":0`)) || !bytes.Contains(bots.Body.Bytes(), []byte(`"report_count":0`)) {
		t.Fatalf("deleted bot remains in counts: %s", bots.Body.String())
	}
	sources := request(h, http.MethodGet, "/api/v1/sources", "", "test-secret")
	if !bytes.Contains(sources.Body.Bytes(), []byte(`"items":[]`)) {
		t.Fatalf("empty source remains after deleting last bot: %s", sources.Body.String())
	}
	if got := request(h, http.MethodDelete, botDeletePath, "", "test-secret"); got.Code != http.StatusNotFound {
		t.Fatalf("deleting a missing bot status=%d", got.Code)
	}
	if _, err := db.GetReport(context.Background(), createdBody.ReportID); err == nil {
		t.Fatal("store still contains deleted report")
	}
}

func TestValidationAndRequestLimit(t *testing.T) {
	h, _, _ := testHandler(t, 1024)
	invalid := request(h, "POST", "/api/v1/reports", postBody("Codex", "# test", ""), "test-secret")
	if invalid.Code != 400 {
		t.Fatalf("invalid source status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	large := postBody("grok", strings.Repeat("x", 2048), "")
	got := request(h, "POST", "/api/v1/reports", large, "test-secret")
	if got.Code != 413 {
		t.Fatalf("oversize status=%d body=%s", got.Code, got.Body.String())
	}
	avatar := strings.Replace(postBody("grok", "# hi", ""), `"content":"# hi"`, `"content":"# hi","avatar_url":"http://example.com/a.png"`, 1)
	got = request(h, "POST", "/api/v1/reports", avatar, "test-secret")
	if got.Code != 400 {
		t.Fatalf("insecure avatar status=%d body=%s", got.Code, got.Body.String())
	}
}

func TestSSEReportsNewReportAndDisconnect(t *testing.T) {
	h, _, _ := testHandler(t, 1<<20)
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-secret")
	stream, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK {
		t.Fatalf("SSE status=%d", stream.StatusCode)
	}
	eventLines := make(chan string, 4)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := bufio.NewScanner(stream.Body)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "event:") {
				eventLines <- scanner.Text()
			}
		}
	}()
	if stream.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("unexpected content type: %q", stream.Header.Get("Content-Type"))
	}
	pushReq, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/reports", strings.NewReader(postBody("grok", "# Live", "live")))
	if err != nil {
		t.Fatal(err)
	}
	pushReq.Header.Set("Authorization", "Bearer test-secret")
	pushReq.Header.Set("Content-Type", "application/json")
	push, err := server.Client().Do(pushReq)
	if err != nil {
		t.Fatal(err)
	}
	push.Body.Close()
	if push.StatusCode != http.StatusCreated {
		t.Fatalf("push status: %d", push.StatusCode)
	}
	select {
	case event := <-eventLines:
		if event != "event: report.created" {
			t.Fatalf("unexpected SSE event: %s", event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing SSE event")
	}
	cancel()
	stream.Body.Close()
	select {
	case <-readerDone:
	case <-time.After(time.Second):
		t.Fatal("SSE reader did not disconnect")
	}
}

func TestWriteRateLimitResetsAfterOneMinute(t *testing.T) {
	db := filepath.Join(t.TempDir(), "rate.db")
	s, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	server := New(s, events.New(), "secret", 1<<20, webui.Files)
	start := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 120; i++ {
		if ok, _ := server.allowWrite("192.0.2.10:1234", start); !ok {
			t.Fatalf("request %d was limited early", i+1)
		}
	}
	if ok, wait := server.allowWrite("192.0.2.10:1234", start); ok || wait <= 0 {
		t.Fatalf("limit did not apply: ok=%v wait=%s", ok, wait)
	}
	if ok, _ := server.allowWrite("192.0.2.10:1234", start.Add(time.Minute)); !ok {
		t.Fatal("rate limit did not reset")
	}
}
