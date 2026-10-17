package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Nyakooo/grok-bot-dock/internal/store"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type syncConnectionInput struct {
	Revision     int64 `json:"revision,omitempty" jsonschema:"The revision returned by the disconnect request"`
	Disconnected bool  `json:"disconnected,omitempty" jsonschema:"Set true only after removing this MCP connection from the client"`
}

var connectionSyncCapability = mcpToolCapability{
	Name: "sync_connection_state", Title: "Check connection removal request", Access: "lifecycle",
	Description:         "Check for a disconnect request for this OAuth MCP client and acknowledge only after removing its local connection.",
	ProtocolDescription: "Call before using the connection. If requested_action is disconnect, remove this Bolt Dock connection from the client and then call again with disconnected=true and the exact revision. Until acknowledged, this OAuth client may call only this tool. This handshake notifies this specific client; it does not remove settings in a client that never connects again.",
	Fields:              []string{"revision", "disconnected"},
	Example:             `{"revision":1,"disconnected":true}`,
}

func syncConnectionSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[syncConnectionInput](nil)
	if err != nil {
		panic(err)
	}
	schema.Properties["revision"].Minimum = jsonschema.Ptr(float64(0))
	return schema
}

func (s *Server) handleMCPSyncConnection(ctx context.Context, _ *mcp.CallToolRequest, in syncConnectionInput) (*mcp.CallToolResult, map[string]any, error) {
	principal, _ := ctx.Value(oauthPrincipalKey{}).(oauthPrincipal)
	if principal.clientID == "" {
		return nil, nil, errors.New("connection_sync_requires_oauth_client")
	}
	revision, requestedAt, err := s.store.OAuthClientDisconnect(ctx, principal.clientID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, map[string]any{"status": "connected", "requested_action": "none", "message": "No disconnection request is pending."}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if in.Revision != 0 && in.Revision != revision {
		return nil, nil, store.ErrOAuthDisconnectRevision
	}
	if !in.Disconnected {
		return nil, map[string]any{"status": "disconnect_requested", "requested_action": "disconnect", "revision": revision, "requested_at": requestedAt, "client_id": principal.clientID}, nil
	}
	if in.Revision != revision {
		return nil, nil, store.ErrOAuthDisconnectRevision
	}
	if err := s.store.AcknowledgeOAuthClientDeletion(ctx, principal.clientID, revision); err != nil {
		return nil, nil, err
	}
	_ = s.store.OAuthAudit(ctx, "client_deleted_acknowledged", principal.clientID, "success")
	s.hub.Publish("oauth.client.deleted", map[string]string{"client_id": principal.clientID, "status": "acknowledged"})
	return nil, map[string]any{"status": "deleted", "requested_action": "disconnect", "revision": revision, "message": "This OAuth client registration and its grants have been removed from Bolt Dock."}, nil
}

func (s *Server) requestOAuthClientDeletion(ctx context.Context, id string) (map[string]any, error) {
	revision, err := s.store.RequestOAuthClientDeletion(ctx, id)
	if err != nil {
		return nil, err
	}
	_ = s.store.OAuthAudit(ctx, "client_deletion_requested", id, "pending")
	return map[string]any{"ok": true, "status": "disconnect_requested", "revision": revision, "client_id": id}, nil
}

func (s *Server) forceDeleteOAuthClient(ctx context.Context, id string) error {
	if err := s.store.DeleteOAuthClientImmediately(ctx, id); err != nil {
		return fmt.Errorf("delete OAuth client: %w", err)
	}
	_ = s.store.OAuthAudit(ctx, "client_deleted_without_ack", id, "owner_forced")
	s.hub.Publish("oauth.client.deleted", map[string]string{"client_id": id, "status": "forced"})
	return nil
}
