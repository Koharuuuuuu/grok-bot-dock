package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nyakooo/grok-bot-dock/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTopicRESTEndToEnd(t *testing.T) {
	h, _, hub := testHandler(t, 1<<20)
	events := hub.Subscribe()
	defer hub.Unsubscribe(events)
	for _, path := range []string{"/api/v1/topics", "/api/v1/topics/airport/reports"} {
		if w := request(h, "GET", path, "", ""); w.Code != 401 {
			t.Fatalf("unauthenticated topic read %s: %d", path, w.Code)
		}
	}
	body := `{"source":"grok","stable_task_id":"airport-watch","topic_id":"airport","topic_name":"机场监控","executed_at":"2026-10-17T09:00:00+08:00","external_id":"run-1","title":"优惠变化","content":"# 正式报告\n\n候选情报"}`
	w := request(h, "POST", "/api/v1/reports", body, "test-secret")
	if w.Code != 201 {
		t.Fatalf("publish: %d %s", w.Code, w.Body)
	}
	var out struct {
		ReportID int64  `json:"report_id"`
		TopicID  string `json:"topic_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.TopicID != "airport" {
		t.Fatalf("missing topic response: %s", w.Body)
	}
	w = request(h, "GET", "/api/v1/topics", "", "test-secret")
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	var topics struct {
		Items []store.Topic `json:"items"`
	}
	json.Unmarshal(w.Body.Bytes(), &topics)
	if len(topics.Items) != 1 || topics.Items[0].Name != "机场监控" {
		t.Fatalf("topics: %s", w.Body)
	}
	for _, body := range []string{`{"name":"机场情报","group":"生活与消费","followed":true,"pinned":true}`, `{"action":"request_stop"}`} {
		w = request(h, "PATCH", "/api/v1/topics/airport", body, "test-secret")
		if w.Code != 200 {
			t.Fatalf("patch: %d %s", w.Code, w.Body)
		}
	}
	w = request(h, "POST", "/api/v1/topics/sync", `{"source":"grok","stable_task_id":"airport-watch"}`, "test-secret")
	var control store.TopicControl
	json.Unmarshal(w.Body.Bytes(), &control)
	if w.Code != 200 || control.RequestedAction != "stop" || control.Revision != 1 {
		t.Fatalf("poll: %s", w.Body)
	}
	w = request(h, "POST", "/api/v1/topics/sync", `{"source":"grok","stable_task_id":"airport-watch","revision":0,"stopped":true}`, "test-secret")
	if w.Code != 409 {
		t.Fatalf("stale ack: %d", w.Code)
	}
	w = request(h, "POST", "/api/v1/topics/sync", `{"source":"grok","stable_task_id":"airport-watch","revision":1,"stopped":true}`, "test-secret")
	json.Unmarshal(w.Body.Bytes(), &control)
	if w.Code != 200 || control.Status != "archived" {
		t.Fatalf("ack: %s", w.Body)
	}
	if w = request(h, "POST", "/api/v1/reports", body, "test-secret"); w.Code != 200 {
		t.Fatalf("archived retry lost: %d %s", w.Code, w.Body)
	}
	if w = request(h, "GET", "/api/v1/topics/airport/reports?limit=1", "", "test-secret"); w.Code != 200 {
		t.Fatalf("topic read: %s", w.Body)
	}
	if w = request(h, "GET", "/api/v1/search?q=候选情报", "", "test-secret"); w.Code != 200 {
		t.Fatal(w.Body)
	}
	var page store.Page
	json.Unmarshal(w.Body.Bytes(), &page)
	if len(page.Items) != 1 || page.Items[0].TopicID != "airport" {
		t.Fatalf("search lost topic: %s", w.Body)
	}
	w = request(h, "PATCH", "/api/v1/topics/airport", `{"action":"restore"}`, "test-secret")
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = request(h, "PATCH", "/api/v1/topics/airport", `{"action":"confirm_stopped","evidence":"wrong state"}`, "test-secret")
	if w.Code != 409 {
		t.Fatalf("invalid lifecycle: %d", w.Code)
	}
	w = request(h, "PATCH", "/api/v1/topics/airport", `{"followed":true} {}`, "test-secret")
	if w.Code != 400 {
		t.Fatalf("trailing JSON: %d", w.Code)
	}
	w = request(h, "POST", "/api/v1/topics/sync", `{"source":"bad source"}`, "test-secret")
	if w.Code != 400 {
		t.Fatalf("bad identity: %d %s", w.Code, w.Body)
	}
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("topic/report event not published")
	}
}

func TestMCPTopicIdentityAndStopHandshake(t *testing.T) {
	h, _, _ := testHandler(t, 1<<20)
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "topic-integration", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerRoundTripper{next: http.DefaultTransport, token: "test-secret"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "publish_report", Arguments: map[string]any{"source": "grok", "stable_task_id": "airport", "topic_id": "airport", "topic_name": "机场情报", "content": "# 最终报告", "executed_at": "2026-10-17T09:00:00Z", "external_id": "mcp-1"}})
	if err != nil || result.IsError {
		t.Fatalf("MCP topic publish: %+v %v", result, err)
	}
	out := decodeMCPOutput[publishReportOutput](t, result.StructuredContent)
	if out.TopicID != "airport" {
		t.Fatalf("MCP lost topic: %+v", out)
	}
	w := request(h, "PATCH", "/api/v1/topics/airport", `{"action":"request_stop"}`, "test-secret")
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "sync_topic_state", Arguments: map[string]any{"source": "grok", "stable_task_id": "airport"}})
	if err != nil || result.IsError {
		t.Fatalf("MCP poll: %+v %v", result, err)
	}
	control := decodeMCPOutput[store.TopicControl](t, result.StructuredContent)
	if control.RequestedAction != "stop" {
		t.Fatalf("MCP stop not delivered: %+v", control)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "sync_topic_state", Arguments: map[string]any{"source": "grok", "stable_task_id": "airport", "revision": control.Revision, "stopped": true}})
	if err != nil || result.IsError {
		t.Fatalf("MCP ack: %+v %v", result, err)
	}
	control = decodeMCPOutput[store.TopicControl](t, result.StructuredContent)
	if control.Status != "archived" {
		t.Fatalf("MCP did not complete archive: %+v", control)
	}
}
