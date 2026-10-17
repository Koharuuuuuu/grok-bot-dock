package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/Nyakooo/grok-bot-dock/internal/store"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type topicSyncInput struct {
	Source       string `json:"source,omitempty" jsonschema:"Task platform source; omitted means unknown"`
	BotID        string `json:"bot_id,omitempty" jsonschema:"Stable Bot identity; provide this or stable_task_id"`
	StableTaskID string `json:"stable_task_id,omitempty" jsonschema:"Same stable task ID used for publishing"`
	Revision     int64  `json:"revision,omitempty" jsonschema:"Stop request revision returned by a previous poll"`
	Stopped      bool   `json:"stopped,omitempty" jsonschema:"Set true only after the external recurring task has actually been stopped"`
}

func topicSyncSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[topicSyncInput](nil)
	if err != nil {
		panic(err)
	}
	setSchemaMaxLength(schema, "source", 63)
	setSchemaMaxLength(schema, "bot_id", 128)
	setSchemaMaxLength(schema, "stable_task_id", 512)
	schema.AnyOf = []*jsonschema.Schema{{Required: []string{"bot_id"}}, {Required: []string{"stable_task_id"}}}
	schema.Properties["revision"].Minimum = jsonschema.Ptr(float64(0))
	return schema
}

var topicSyncCapability = mcpToolCapability{
	Name: "sync_topic_state", Title: "Check topic stop requests", Access: "写入",
	Description:         "检查所属主题的停止请求；仅在外部任务确实停止后，提交对应版本的确认。",
	ProtocolDescription: "Poll the publishing task's topic lifecycle before every recurring execution. If requested_action is stop, stop the external schedule using platform tools; only then acknowledge with stopped=true and the returned revision. Never claim a schedule stopped merely because you read a request. This does not stop any external platform itself. Keep the same source and bot_id/stable_task_id used by publish_report.",
}

func (s *Server) syncTopic(ctx context.Context, in topicSyncInput) (store.TopicControl, error) {
	raw := reportRequest{Source: in.Source, BotID: in.BotID, StableTaskID: in.StableTaskID, ExecutedAt: "2000-01-01T00:00:00Z", Content: "identity-validation"}
	normalized, err := normalizeReportRequest(raw)
	if err != nil {
		return store.TopicControl{}, err
	}
	if in.Revision < 0 {
		return store.TopicControl{}, store.ErrTopicState
	}
	return s.store.SyncTopic(ctx, normalized.Source, normalized.BotID, in.Revision, in.Stopped)
}
func (s *Server) handleMCPSyncTopic(ctx context.Context, _ *mcp.CallToolRequest, in topicSyncInput) (*mcp.CallToolResult, store.TopicControl, error) {
	if !s.allowOAuthContext(ctx, oauthWriteScope) {
		return nil, store.TopicControl{}, errors.New("insufficient_scope: reports:write is required")
	}
	control, err := s.syncTopic(ctx, in)
	if err != nil {
		return nil, store.TopicControl{}, fmt.Errorf("topic sync failed: %w", err)
	}
	if in.Stopped {
		s.hub.Publish("topic.updated", control)
	}
	return nil, control, nil
}
