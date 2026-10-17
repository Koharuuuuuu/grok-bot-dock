package api

import (
	"context"
	"encoding/json"
	"fmt"
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

type bearerRoundTripper struct {
	next  http.RoundTripper
	token string
}

func TestMCPPublishSharesReportRateLimit(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/mcp-rate-limit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	server := New(db, events.New(), "test-secret", 1<<20, webui.Files)
	remote := "192.0.2.10:1234"
	server.rate["192.0.2.10"] = rateWindow{started: time.Now(), count: 120}
	ctx := context.WithValue(context.Background(), mcpRemoteAddrKey{}, remote)
	botID := "rate-limit-test"
	_, _, err = server.handleMCPPublishReport(ctx, nil, publishReportInput{
		ExecutedAt: "2026-10-10T10:00:00Z",
		Content:    "# Rate limited report",
		BotID:      &botID,
	})
	apiErr, ok := err.(*apiError)
	if !ok || apiErr.status != http.StatusTooManyRequests || apiErr.code != "rate_limited" {
		t.Fatalf("publish error = %#v, want 429 rate_limited", err)
	}
	page, err := db.ListReports(context.Background(), "", "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("rate-limited MCP publish saved %d reports", len(page.Items))
	}
}

func (rt bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.Header = req.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+rt.token)
	return rt.next.RoundTrip(copy)
}

func TestMCPRequiresBearerAndRejectsForeignOrigin(t *testing.T) {
	h, _, _ := testHandler(t, 1<<20)
	for _, tc := range []struct {
		name          string
		cookie        bool
		badToken      bool
		authorization string
		origin        string
		want          int
	}{
		{name: "missing token", want: http.StatusUnauthorized},
		{name: "incorrect token", badToken: true, want: http.StatusUnauthorized},
		{name: "case-insensitive bearer scheme", authorization: "bEaReR test-secret", want: http.StatusMethodNotAllowed},
		{name: "web cookie is not MCP auth", cookie: true, want: http.StatusUnauthorized},
		{name: "foreign origin", origin: "https://evil.example", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			if tc.badToken {
				req.Header.Set("Authorization", "Bearer wrong-secret")
			}
			if tc.cookie {
				req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "valid-web-session"})
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	put := httptest.NewRequest(http.MethodPut, "/mcp", nil)
	put.Header.Set("Authorization", "Bearer test-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, put)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT status = %d, want 405", w.Code)
	}
	wrongPath := httptest.NewRequest(http.MethodGet, "/mcp/", nil)
	wrongPath.Header.Set("Authorization", "Bearer test-secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, wrongPath)
	if w.Code != http.StatusNotFound {
		t.Fatalf("trailing slash status = %d, want 404", w.Code)
	}
}

func TestMCPStreamableToolsShareRESTStorageAndIdempotency(t *testing.T) {
	h, db, hub := testHandler(t, 1<<20)
	restPush := request(h, http.MethodPost, "/api/v1/reports", postBody("grok", "# REST 报告\n\n跨接口搜索标记 RESTONLY。SAME_BOT_CROSS_SOURCE grok record.", "rest-execution-1"), "test-secret")
	if restPush.Code != http.StatusCreated {
		t.Fatalf("REST publish before MCP search: %d %s", restPush.Code, restPush.Body.String())
	}
	updates := hub.Subscribe()
	t.Cleanup(func() { hub.Unsubscribe(updates) })
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpClient := &http.Client{Transport: bearerRoundTripper{next: http.DefaultTransport, token: "test-secret"}}
	client := mcp.NewClient(&mcp.Implementation{Name: "bolt-dock-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: httpClient}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatalf("MCP initialize: %v", err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(listed.Tools) != 4 {
		t.Fatalf("listed %d tools, want 4", len(listed.Tools))
	}
	serializedTools, err := json.Marshal(listed.Tools)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(serializedTools), `"maxLength":256`) || !strings.Contains(string(serializedTools), `"maxLength":1048576`) || !strings.Contains(string(serializedTools), `"maxItems":32`) {
		t.Fatalf("tool schemas omit documented length constraints: %s", serializedTools)
	}
	for _, tool := range listed.Tools {
		if tool.Name != "publish_report" && tool.Name != "search_reports" && tool.Name != "sync_topic_state" && tool.Name != "sync_connection_state" {
			t.Errorf("unexpected MCP tool %q", tool.Name)
		}
	}

	args := map[string]any{
		"source": "codex", "stable_task_id": "p7-test-run", "bot_name": "P7 测试",
		"executed_at": "2026-10-10T10:00:00Z", "external_id": "p7-execution-1",
		"title": "Bolt Dock P7 MCP 测试报告", "summary": "这是一份中文摘要。", "content": "# Bolt Dock P7 MCP 测试报告\n\n验收记录通过。正文机密标记：SECRET_UNIQUE_BODY。",
	}
	first, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "publish_report", Arguments: args})
	if err != nil || first.IsError {
		t.Fatalf("publish_report: result=%#v err=%v", first, err)
	}
	firstOut := decodeMCPOutput[publishReportOutput](t, first.StructuredContent)
	if !firstOut.Created || firstOut.Duplicate || firstOut.ReportID < 1 {
		t.Fatalf("unexpected first publish: %#v", firstOut)
	}

	second, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "publish_report", Arguments: args})
	if err != nil || second.IsError {
		t.Fatalf("idempotent publish: result=%#v err=%v", second, err)
	}
	secondOut := decodeMCPOutput[publishReportOutput](t, second.StructuredContent)
	if secondOut.Created || !secondOut.Duplicate || secondOut.ReportID != firstOut.ReportID {
		t.Fatalf("retry was not idempotent: %#v", secondOut)
	}

	select {
	case event := <-updates:
		if !strings.HasPrefix(string(event), "event: report.created\n") {
			t.Fatalf("unexpected SSE event: %s", event)
		}
	case <-time.After(time.Second):
		t.Fatal("MCP publish did not emit the existing SSE event")
	}
	select {
	case event := <-updates:
		t.Fatalf("duplicate publish emitted another event: %s", event)
	case <-time.After(40 * time.Millisecond):
	}

	page, err := db.ListReports(ctx, "codex", firstOut.BotID, "", "", 20)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("REST storage did not contain exactly one report: page=%#v err=%v", page, err)
	}
	secondRunArgs := map[string]any{
		"source": "codex", "stable_task_id": "p7-test-run", "bot_name": "P7 测试",
		"executed_at": "2026-10-10T11:00:00Z", "external_id": "p7-execution-2",
		"title": "Bolt Dock P7 第二次执行", "summary": "第二次中文验收摘要。", "content": "# 第二次验收记录\n\n不同执行内容。",
	}
	secondRun, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "publish_report", Arguments: secondRunArgs})
	if err != nil || secondRun.IsError {
		t.Fatalf("second stable-task publish: result=%#v err=%v", secondRun, err)
	}
	secondRunOut := decodeMCPOutput[publishReportOutput](t, secondRun.StructuredContent)
	if !secondRunOut.Created || secondRunOut.BotID != firstOut.BotID {
		t.Fatalf("stable_task_id did not map to the same Bot: %#v", secondRunOut)
	}
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("second execution did not emit SSE")
	}
	page, err = db.ListReports(ctx, "codex", firstOut.BotID, "", "", 20)
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("expected two executions under one Bot: page=%#v err=%v", page, err)
	}
	crossSource, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "publish_report", Arguments: map[string]any{
		"source": "codex", "bot_id": "daily", "executed_at": "2026-10-10T12:30:00Z",
		"external_id": "same-bot-id-codex", "content": "SAME_BOT_CROSS_SOURCE codex record.",
	}})
	if err != nil || crossSource.IsError {
		t.Fatalf("publish same Bot ID from another source: result=%#v err=%v", crossSource, err)
	}
	for _, source := range []string{"codex", "grok"} {
		isolated, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{
			"query": "SAME_BOT_CROSS_SOURCE", "source": source, "bot_id": "daily",
		}})
		if err != nil || isolated.IsError {
			t.Fatalf("search same Bot ID for %q: result=%#v err=%v", source, isolated, err)
		}
		got := decodeMCPOutput[searchReportsOutput](t, isolated.StructuredContent)
		if len(got.Items) != 1 || got.Items[0].Source != source || got.Items[0].BotID != "daily" {
			t.Fatalf("source %q was not isolated for shared bot_id: %#v", source, got)
		}
	}

	search, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "验收记录", "source": "codex"}})
	if err != nil || search.IsError {
		t.Fatalf("search_reports: result=%#v err=%v", search, err)
	}
	searchOut := decodeMCPOutput[searchReportsOutput](t, search.StructuredContent)
	if len(searchOut.Items) != 2 || searchOut.Items[0].BotName != "P7 测试" {
		t.Fatalf("search result mismatch: %#v", searchOut)
	}
	encoded, _ := json.Marshal(searchOut)
	if strings.Contains(string(encoded), "SECRET_UNIQUE_BODY") {
		t.Fatal("search result unexpectedly contains the full report body")
	}
	restSearch := request(h, http.MethodGet, "/api/v1/search?q="+url.QueryEscape("验收记录")+"&source=codex", "", "test-secret")
	if restSearch.Code != http.StatusOK || !strings.Contains(restSearch.Body.String(), `"id":`+strconv.FormatInt(firstOut.ReportID, 10)) {
		t.Fatalf("REST search could not find MCP-published report: %d %s", restSearch.Code, restSearch.Body.String())
	}
	reverseSearch, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "RESTONLY", "source": "grok"}})
	if err != nil || reverseSearch.IsError {
		t.Fatalf("MCP search for REST report: result=%#v err=%v", reverseSearch, err)
	}
	if got := decodeMCPOutput[searchReportsOutput](t, reverseSearch.StructuredContent); len(got.Items) != 1 || got.Items[0].Source != "grok" {
		t.Fatalf("MCP did not find REST-published report: %#v", got)
	}

	empty, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "不存在的内容"}})
	if err != nil || empty.IsError {
		t.Fatalf("empty search should succeed: result=%#v err=%v", empty, err)
	}
	if got := decodeMCPOutput[searchReportsOutput](t, empty.StructuredContent); len(got.Items) != 0 {
		t.Fatalf("empty query returned unexpected items: %#v", got)
	}
	paged, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "验收记录", "source": "codex", "limit": 1}})
	if err != nil || paged.IsError {
		t.Fatalf("first search page: result=%#v err=%v", paged, err)
	}
	pagedOut := decodeMCPOutput[searchReportsOutput](t, paged.StructuredContent)
	if len(pagedOut.Items) != 1 || pagedOut.NextCursor == "" {
		t.Fatalf("search pagination did not return an opaque cursor: %#v", pagedOut)
	}
	nextPage, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "验收记录", "source": "codex", "limit": 1, "cursor": pagedOut.NextCursor}})
	if err != nil || nextPage.IsError {
		t.Fatalf("next search page: result=%#v err=%v", nextPage, err)
	}
	if got := decodeMCPOutput[searchReportsOutput](t, nextPage.StructuredContent); len(got.Items) != 1 || got.Items[0].ReportID == pagedOut.Items[0].ReportID {
		t.Fatalf("cursor page did not advance: %#v", got)
	}
	wrongDate, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "验收记录", "source": "codex", "from": "2026-10-11"}})
	if err != nil || wrongDate.IsError {
		t.Fatalf("date-filtered empty search: result=%#v err=%v", wrongDate, err)
	}
	if got := decodeMCPOutput[searchReportsOutput](t, wrongDate.StructuredContent); len(got.Items) != 0 {
		t.Fatalf("date filter returned out-of-range reports: %#v", got)
	}
	badCursor, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "验收记录", "cursor": "not-a-cursor"}})
	if err != nil {
		t.Fatalf("bad cursor transport error: %v", err)
	}
	if !badCursor.IsError {
		t.Fatal("invalid cursor was not returned as a tool error")
	}

	const concurrentPublishes = 6
	results := make(chan error, concurrentPublishes)
	for i := 0; i < concurrentPublishes; i++ {
		go func(i int) {
			result, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: "publish_report", Arguments: map[string]any{
				"source": "codex", "bot_id": "parallel-" + strconv.Itoa(i), "executed_at": "2026-10-10T12:00:00Z",
				"external_id": "parallel-execution-" + strconv.Itoa(i), "content": "并发验收报告。",
			}})
			if callErr == nil && result.IsError {
				callErr = fmt.Errorf("tool returned isError=true")
			}
			results <- callErr
		}(i)
	}
	for i := 0; i < concurrentPublishes; i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent MCP publish: %v", err)
		}
	}
}

func TestMCPToolValidationReturnsToolErrors(t *testing.T) {
	h, _, _ := testHandler(t, 1<<20)
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpClient := &http.Client{Transport: bearerRoundTripper{next: http.DefaultTransport, token: "test-secret"}}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: httpClient}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatalf("MCP initialize: %v", err)
	}
	defer session.Close()
	missingIdentity, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "publish_report", Arguments: map[string]any{"executed_at": "2026-10-10T10:00:00Z", "content": "final"}})
	if err != nil {
		t.Fatalf("publish tool validation transport error: %v", err)
	}
	if !missingIdentity.IsError {
		t.Fatal("missing stable Bot identity was not reported as a tool error")
	}
	tooMany, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "hello", "limit": 21}})
	if err != nil {
		t.Fatalf("search limit transport error: %v", err)
	}
	if !tooMany.IsError {
		t.Fatal("search limit above 20 was not rejected")
	}
	badDate, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "hello", "from": "2026-13-01"}})
	if err != nil {
		t.Fatalf("search date transport error: %v", err)
	}
	if !badDate.IsError {
		t.Fatal("invalid date was not rejected")
	}
}

func TestMCPRequestBoundsAndProtocolErrors(t *testing.T) {
	h, _, _ := testHandler(t, 1024)
	large := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+strings.Repeat(" ", 2048)))
	large.Header.Set("Authorization", "Bearer test-secret")
	large.Header.Set("Content-Type", "application/json")
	large.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, large)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized MCP request status = %d, want 413; body=%s", w.Code, w.Body.String())
	}

	badJSON := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`not-json`))
	badJSON.Header.Set("Authorization", "Bearer test-secret")
	badJSON.Header.Set("Content-Type", "application/json")
	badJSON.Header.Set("Accept", "application/json, text/event-stream")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, badJSON)
	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("invalid JSON-RPC request status = %d, want a 4xx; body=%s", w.Code, w.Body.String())
	}

	badAccept := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	badAccept.Header.Set("Authorization", "Bearer test-secret")
	badAccept.Header.Set("Content-Type", "application/json")
	badAccept.Header.Set("Accept", "text/plain")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, badAccept)
	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("unsupported Accept status = %d, want a 4xx; body=%s", w.Code, w.Body.String())
	}

	invalidRPC := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"initialize","params":[]}`))
	invalidRPC.Header.Set("Authorization", "Bearer test-secret")
	invalidRPC.Header.Set("Content-Type", "application/json")
	invalidRPC.Header.Set("Accept", "application/json, text/event-stream")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, invalidRPC)
	if w.Code < 200 || w.Code >= 300 || !strings.Contains(w.Body.String(), `"error"`) {
		t.Fatalf("invalid JSON-RPC params did not return a protocol error: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestMCPClientDisconnectCancelsRequest(t *testing.T) {
	h, _, _ := testHandler(t, 1<<20)
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	client := &http.Client{Transport: bearerRoundTripper{next: http.DefaultTransport, token: "test-secret"}}
	bodyReader, bodyWriter := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/mcp", bodyReader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	done := make(chan error, 1)
	go func() {
		resp, requestErr := client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		done <- requestErr
	}()
	if _, err := io.WriteString(bodyWriter, `{"jsonrpc":"2.0","id":3,"method":"initialize","params":`); err != nil {
		t.Fatalf("write partial MCP request: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	_ = bodyWriter.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled MCP request unexpectedly completed successfully")
		}
	case <-time.After(time.Second):
		t.Fatal("client did not observe MCP request cancellation")
	}
}

func TestMCPStatelessCurrentProtocolNegotiation(t *testing.T) {
	h, _, _ := testHandler(t, 1<<20)
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpClient := &http.Client{Transport: bearerRoundTripper{next: http.DefaultTransport, token: "test-secret"}}
	client := mcp.NewClient(&mcp.Implementation{Name: "current-protocol-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: httpClient}, nil)
	if err != nil {
		t.Fatalf("negotiate current MCP protocol: %v", err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 4 {
		t.Fatalf("current protocol tool discovery: tools=%#v err=%v", listed, err)
	}
}

func decodeMCPOutput[T any](t *testing.T, value any) T {
	t.Helper()
	var out T
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decode MCP output %s: %v", data, err)
	}
	return out
}
