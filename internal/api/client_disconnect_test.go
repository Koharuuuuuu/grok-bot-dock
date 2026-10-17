package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nyakooo/grok-bot-dock/internal/events"
	"github.com/Nyakooo/grok-bot-dock/internal/store"
	"github.com/Nyakooo/grok-bot-dock/internal/webui"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestOAuthClientDeletionNotifiesExactClientBeforeRemovingIt(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "disconnect.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.RegisterOAuthClient(ctx, "https://agent.example/client.json", "Research Agent", []string{"https://agent.example/callback"}, ""); err != nil {
		t.Fatal(err)
	}
	resource := "https://dock.example/mcp"
	code := store.OAuthCode{ClientID: "https://agent.example/client.json", RedirectURI: "https://agent.example/callback", Challenge: "challenge", Resource: resource, Scopes: []string{oauthReadScope}, ExpiresAt: time.Now().Add(time.Hour)}
	if _, err = db.CreateOAuthCode(ctx, oauthHash("code"), code, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExchangeOAuthCode(ctx, oauthHash("code"), store.OAuthCodeBinding{ClientID: code.ClientID, RedirectURI: code.RedirectURI, Resource: resource}, oauthHash("token"), time.Now().Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	hub := events.New()
	h := NewWithOAuth(db, hub, "test-secret", 1<<20, webui.Files, OAuthConfig{Enabled: true, PublicBaseURL: "https://dock.example"}).Handler()
	server := httptest.NewServer(h)
	defer server.Close()
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "disconnect-test", Version: "1"}, nil).Connect(callCtx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerRoundTripper{next: http.DefaultTransport, token: "token"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	login := request(h, http.MethodPost, "/api/v1/auth/login", `{"token":"test-secret"}`, "")
	if login.Code != 200 {
		t.Fatalf("login: %d %s", login.Code, login.Body)
	}
	cookie := login.Result().Cookies()[0]
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/oauth/clients/https%3A%2F%2Fagent.example%2Fclient.json", nil)
	deleteReq.AddCookie(cookie)
	deleteReq.Header.Set("Origin", "https://dock.example")
	deleteResponse := httptest.NewRecorder()
	h.ServeHTTP(deleteResponse, deleteReq)
	if deleteResponse.Code != 200 {
		t.Fatalf("request deletion: %d %s", deleteResponse.Code, deleteResponse.Body)
	}
	var pending struct {
		Status   string `json:"status"`
		Revision int64  `json:"revision"`
	}
	if err := json.Unmarshal(deleteResponse.Body.Bytes(), &pending); err != nil || pending.Status != "disconnect_requested" || pending.Revision != 1 {
		t.Fatalf("unexpected delete response %#v err=%v", pending, err)
	}

	// A client can reacquire a narrowly usable token after its access token
	// expires; pending-deletion middleware still restricts it to the handshake.
	authorizeQuery := url.Values{
		"response_type": {"code"}, "client_id": {code.ClientID}, "redirect_uri": {code.RedirectURI},
		"scope": {oauthReadScope}, "state": {"state"}, "code_challenge": {strings.Repeat("A", 43)},
		"code_challenge_method": {"S256"}, "resource": {resource},
	}
	authorizeRequest := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+authorizeQuery.Encode(), nil)
	authorizeRequest.AddCookie(cookie)
	authorizeResponse := httptest.NewRecorder()
	h.ServeHTTP(authorizeResponse, authorizeRequest)
	if authorizeResponse.Code != http.StatusOK || !strings.Contains(authorizeResponse.Body.String(), "/oauth/consent") {
		t.Fatalf("pending client could not reauthorize for its deletion handshake: %d %s", authorizeResponse.Code, authorizeResponse.Body)
	}

	if result, callErr := session.CallTool(callCtx, &mcp.CallToolParams{Name: "search_reports", Arguments: map[string]any{"query": "private"}}); callErr == nil && (result == nil || !result.IsError) {
		t.Fatal("pending client could still use an ordinary tool")
	}
	result, err := session.CallTool(callCtx, &mcp.CallToolParams{Name: "sync_connection_state"})
	if err != nil || result.IsError {
		t.Fatalf("poll disconnect: %+v %v", result, err)
	}
	var notice map[string]any
	encoded, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(encoded, &notice); err != nil || notice["requested_action"] != "disconnect" || notice["client_id"] != code.ClientID {
		t.Fatalf("notice was not client-specific: %s %v", encoded, err)
	}
	result, err = session.CallTool(callCtx, &mcp.CallToolParams{Name: "sync_connection_state", Arguments: map[string]any{"revision": float64(1), "disconnected": true}})
	if err != nil || result.IsError {
		t.Fatalf("ack disconnect: %+v %v", result, err)
	}
	if _, err = db.OAuthClient(ctx, code.ClientID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("client registration remains after acknowledgement: %v", err)
	}
	if _, _, err = db.OAuthTokenClient(ctx, oauthHash("token"), resource); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("grant/token remains after deletion: %v", err)
	}

	if err = db.RegisterOAuthClient(ctx, "force-client", "Offline Client", []string{"https://offline.example/callback"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RequestOAuthClientDeletion(ctx, "force-client"); err != nil {
		t.Fatal(err)
	}
	forceReq := httptest.NewRequest(http.MethodPost, "/api/v1/oauth/clients/force-delete", strings.NewReader(`{"client_id":"force-client"}`))
	forceReq.AddCookie(cookie)
	forceReq.Header.Set("Origin", "https://dock.example")
	forceResponse := httptest.NewRecorder()
	h.ServeHTTP(forceResponse, forceReq)
	if forceResponse.Code != 200 || !strings.Contains(forceResponse.Body.String(), "deleted_without_ack") {
		t.Fatalf("force delete status=%d body=%s", forceResponse.Code, forceResponse.Body)
	}
	if _, err = db.OAuthClient(ctx, "force-client"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("offline client registration remains: %v", err)
	}
}
