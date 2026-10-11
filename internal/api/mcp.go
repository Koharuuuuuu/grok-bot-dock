package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpRemoteAddrKey struct{}

type mcpToolCapability struct {
	Name                string   `json:"name"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	ProtocolDescription string   `json:"-"`
	Access              string   `json:"access"`
	Fields              []string `json:"fields"`
	Example             string   `json:"example"`
}

var mcpToolCapabilities = []mcpToolCapability{
	{
		Name: "publish_report", Title: "Publish final report", Access: "写入",
		Description:         "提交已完成并确认的 Markdown 报告。写入报告，必要时创建 Bot；不会删除或重命名数据。",
		ProtocolDescription: "Publish a completed, confirmed final Markdown report to Bolt Dock. Do not submit drafts, debug logs, intermediate results, or failed tasks. This writes a report and may create a Bot; it never deletes or renames data. Supply a stable bot_id or stable_task_id, and reuse external_id for retries. The server returns metadata only, not the report body.",
	},
	{
		Name: "search_reports", Title: "Search report history", Access: "只读",
		Description:         "按全文关键词查询历史报告，可按来源、Bot、日期筛选并使用游标分页。仅返回元数据和摘要。",
		ProtocolDescription: "Search stored report history by full-text query, including Chinese, with optional source, Bot, date, and cursor filters. Read-only; returns report metadata and summaries, never full Markdown.",
	},
}

type publishReportInput struct {
	ExecutedAt   string   `json:"executed_at" jsonschema:"实际 RFC3339 完成时间"`
	Content      string   `json:"content" jsonschema:"完整 Markdown 正文；仅发布已确认的最终报告"`
	Source       *string  `json:"source,omitempty" jsonschema:"来源标识；省略或留空时记为 unknown"`
	BotID        *string  `json:"bot_id,omitempty" jsonschema:"稳定 Bot 标识；与 stable_task_id 至少提供一个"`
	StableTaskID *string  `json:"stable_task_id,omitempty" jsonschema:"稳定任务标识；未提供 bot_id 时据此映射到同一 Bot"`
	BotName      *string  `json:"bot_name,omitempty" jsonschema:"来源提供的 Bot 显示名称"`
	TaskName     *string  `json:"task_name,omitempty" jsonschema:"Bot 名称的任务名回退值"`
	Title        *string  `json:"title,omitempty" jsonschema:"报告标题"`
	Summary      *string  `json:"summary,omitempty" jsonschema:"报告摘要"`
	Tags         []string `json:"tags,omitempty" jsonschema:"报告标签"`
	ExternalID   *string  `json:"external_id,omitempty" jsonschema:"本次执行的稳定标识；重试时复用以实现幂等"`
	AvatarURL    *string  `json:"avatar_url,omitempty" jsonschema:"可选 HTTPS 头像地址"`
}

type publishReportOutput struct {
	ReportID   int64  `json:"report_id"`
	Created    bool   `json:"created"`
	Duplicate  bool   `json:"duplicate"`
	Source     string `json:"source"`
	BotID      string `json:"bot_id"`
	ExecutedAt string `json:"executed_at"`
}

type searchReportsInput struct {
	Query  string `json:"query" jsonschema:"1–256 字符全文关键词，支持中文"`
	Source string `json:"source,omitempty" jsonschema:"可选来源筛选"`
	BotID  string `json:"bot_id,omitempty" jsonschema:"可选稳定 Bot 标识筛选"`
	From   string `json:"from,omitempty" jsonschema:"可选起始日期，含当日，格式 YYYY-MM-DD"`
	To     string `json:"to,omitempty" jsonschema:"可选截止日期，含当日，格式 YYYY-MM-DD"`
	Limit  *int   `json:"limit,omitempty" jsonschema:"每页结果数，默认 10，最大 20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"上一页返回的可选 next_cursor；仅翻页时使用"`
}

func capabilityMetadata(maxBytes int64) []mcpToolCapability {
	capabilities := append([]mcpToolCapability(nil), mcpToolCapabilities...)
	publishSchema, searchSchema := buildMCPInputSchemas(maxBytes)
	for index, schema := range []*jsonschema.Schema{publishSchema, searchSchema} {
		capabilities[index].Fields = schemaFieldDescriptions(schema)
		capabilities[index].Example = schemaExample(schema)
	}
	return capabilities
}

func buildMCPInputSchemas(maxBytes int64) (*jsonschema.Schema, *jsonschema.Schema) {
	publishSchema, err := jsonschema.For[publishReportInput](nil)
	if err != nil {
		panic(fmt.Sprintf("build publish_report schema: %v", err))
	}
	setSchemaMaxLength(publishSchema, "source", 63)
	setSchemaMaxLength(publishSchema, "bot_id", 128)
	setSchemaMaxLength(publishSchema, "stable_task_id", 512)
	setSchemaMaxLength(publishSchema, "bot_name", 256)
	setSchemaMaxLength(publishSchema, "task_name", 256)
	setSchemaMaxLength(publishSchema, "executed_at", 64)
	setSchemaMaxLength(publishSchema, "title", 512)
	setSchemaMaxLength(publishSchema, "summary", 4096)
	setSchemaMaxLength(publishSchema, "external_id", 256)
	setSchemaMaxLength(publishSchema, "avatar_url", 2048)
	setSchemaMaxLength(publishSchema, "content", int(maxBytes))
	publishSchema.Properties["executed_at"].Format = "date-time"
	publishSchema.Properties["content"].MinLength = jsonschema.Ptr(1)
	publishSchema.AnyOf = []*jsonschema.Schema{{Required: []string{"bot_id"}}, {Required: []string{"stable_task_id"}}}
	if tags := publishSchema.Properties["tags"]; tags != nil && tags.Items != nil {
		tags.MaxItems = jsonschema.Ptr(32)
		tags.Items.MaxLength = jsonschema.Ptr(128)
	}
	searchSchema, err := jsonschema.For[searchReportsInput](nil)
	if err != nil {
		panic(fmt.Sprintf("build search_reports schema: %v", err))
	}
	setSchemaMaxLength(searchSchema, "query", 256)
	setSchemaMaxLength(searchSchema, "source", 63)
	setSchemaMaxLength(searchSchema, "bot_id", 128)
	setSchemaMaxLength(searchSchema, "from", 10)
	setSchemaMaxLength(searchSchema, "to", 10)
	setSchemaMaxLength(searchSchema, "cursor", int(maxBytes))
	searchSchema.Properties["query"].MinLength = jsonschema.Ptr(1)
	searchSchema.Properties["from"].Pattern = `^\d{4}-\d{2}-\d{2}$`
	searchSchema.Properties["to"].Pattern = `^\d{4}-\d{2}-\d{2}$`
	if limit := searchSchema.Properties["limit"]; limit != nil {
		limit.Minimum = jsonschema.Ptr(float64(1))
		limit.Maximum = jsonschema.Ptr(float64(20))
	}
	return publishSchema, searchSchema
}

func schemaFieldDescriptions(schema *jsonschema.Schema) []string {
	names := append([]string(nil), schema.PropertyOrder...)
	if len(names) == 0 {
		for name := range schema.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	required := map[string]bool{}
	for _, name := range schema.Required {
		required[name] = true
	}
	fields := make([]string, 0, len(names))
	for _, name := range names {
		property := schema.Properties[name]
		if property == nil {
			continue
		}
		label := name
		if property.Description != "" {
			label += "：" + property.Description
		}
		if required[name] {
			label += "（必填）"
		}
		fields = append(fields, label)
	}
	return fields
}

func schemaExample(schema *jsonschema.Schema) string {
	values := make(map[string]any, len(schema.Properties))
	for name, property := range schema.Properties {
		if name == "cursor" {
			continue
		}
		if property.Type == "array" {
			values[name] = []string{}
			continue
		}
		switch name {
		case "executed_at":
			values[name] = "2026-10-10T17:30:00+08:00"
		case "content":
			values[name] = "# 完整报告正文"
		case "query":
			values[name] = "关键词"
		case "limit":
			values[name] = 10
		case "from", "to":
			values[name] = "2026-10-01"
		case "bot_id":
			values[name] = "bot-identifier"
		case "stable_task_id":
			values[name] = "task-identifier"
		case "external_id":
			values[name] = "execution-id"
		case "source":
			values[name] = "example"
		case "avatar_url":
			values[name] = "https://example.com/avatar.png"
		case "bot_name":
			values[name] = "示例 Bot"
		case "task_name":
			values[name] = "示例任务"
		case "title":
			values[name] = "最终报告标题"
		case "summary":
			values[name] = "报告摘要"
		default:
			values[name] = "示例"
		}
	}
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

type reportSearchItem struct {
	ReportID   int64    `json:"report_id"`
	Source     string   `json:"source"`
	BotID      string   `json:"bot_id"`
	BotName    string   `json:"bot_name"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	ExecutedAt string   `json:"executed_at"`
	Tags       []string `json:"tags"`
}

type searchReportsOutput struct {
	Items      []reportSearchItem `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

func (s *Server) newMCPHandler() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "bolt-dock", Version: "1.0.0"}, &mcp.ServerOptions{
		Instructions: "Bolt Dock publishes and searches self-hosted execution reports. Only publish completed, confirmed final reports. Use a stable bot_id or stable_task_id; reuse external_id for retries. Client call strategy is not a security boundary.",
	})
	nonDestructive := false
	closedWorld := false
	publishSchema, searchSchema := buildMCPInputSchemas(s.maxBytes)
	mcp.AddTool(server, &mcp.Tool{
		Name:        mcpToolCapabilities[0].Name,
		Title:       mcpToolCapabilities[0].Title,
		Description: mcpToolCapabilities[0].ProtocolDescription,
		InputSchema: publishSchema,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &nonDestructive, IdempotentHint: false, OpenWorldHint: &closedWorld},
	}, s.handleMCPPublishReport)
	mcp.AddTool(server, &mcp.Tool{
		Name:        mcpToolCapabilities[1].Name,
		Title:       mcpToolCapabilities[1].Title,
		Description: mcpToolCapabilities[1].ProtocolDescription,
		InputSchema: searchSchema,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &nonDestructive, IdempotentHint: true, OpenWorldHint: &closedWorld},
	}, s.handleMCPSearchReports)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		MaxRequestBodyBytes:          s.maxBytes,
		Stateless:                    true,
		PropagateRequestCancellation: true,
	})
	return s.mcpAuth(handler)
}

func setSchemaMaxLength(schema *jsonschema.Schema, property string, maxLength int) {
	if field := schema.Properties[property]; field != nil {
		field.MaxLength = jsonschema.Ptr(maxLength)
	}
}

func (s *Server) mcpAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodDelete {
			w.Header().Set("Allow", "GET, POST, DELETE")
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "MCP supports GET, POST, and DELETE")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !s.isSameSiteOrigin(r) {
			writeError(w, http.StatusForbidden, "invalid_origin", "cross-origin MCP requests are not allowed")
			return
		}
		got, _ := bearerToken(r.Header.Get("Authorization"))
		legacyValid := s.token != "" && len(got) == len(s.token) && subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
		var principal oauthPrincipal
		oauthValid := false
		if !legacyValid {
			principal, oauthValid = s.oauthBearer(r)
			if oauthValid {
				r = r.WithContext(context.WithValue(r.Context(), oauthPrincipalKey{}, principal))
			} else {
				challenge := `Bearer realm="Bolt Dock MCP"`
				if s.oauth.Enabled && s.validOAuthConfig() {
					challenge = fmt.Sprintf(`Bearer realm="Bolt Dock MCP", resource_metadata="%s/.well-known/oauth-protected-resource/mcp", scope="%s %s"`, s.oauthBase(), oauthReadScope, oauthWriteScope)
				}
				w.Header().Set("WWW-Authenticate", challenge)
				writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
				return
			}
		}
		if oauthValid {
			originalBody := r.Body
			body, err := io.ReadAll(io.LimitReader(originalBody, s.maxBytes+1))
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), originalBody))
			if err == nil && int64(len(body)) <= s.maxBytes {
				var request struct {
					ID     any    `json:"id"`
					Method string `json:"method"`
					Params struct {
						Name string `json:"name"`
					} `json:"params"`
				}
				if json.Unmarshal(body, &request) == nil && request.Method == "tools/call" {
					required := ""
					if request.Params.Name == "search_reports" {
						required = oauthReadScope
					}
					if request.Params.Name == "publish_report" {
						required = oauthWriteScope
					}
					if required != "" && !principal.scopes[required] {
						w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer error="insufficient_scope", scope="%s", resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, required, s.oauthBase()))
						writeJSON(w, http.StatusForbidden, map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32003, "message": "insufficient_scope"}})
						return
					}
				}
			}
		}
		ctx := context.WithValue(r.Context(), mcpRemoteAddrKey{}, r.RemoteAddr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleMCPPublishReport(ctx context.Context, _ *mcp.CallToolRequest, input publishReportInput) (*mcp.CallToolResult, publishReportOutput, error) {
	if !s.allowOAuthContext(ctx, oauthWriteScope) {
		return nil, publishReportOutput{}, errors.New("insufficient_scope: reports:write is required")
	}
	raw := reportRequest{ExecutedAt: input.ExecutedAt, Content: input.Content}
	if input.Source != nil {
		raw.Source = *input.Source
	}
	if input.BotID != nil {
		raw.BotID = *input.BotID
	}
	if input.StableTaskID != nil {
		raw.StableTaskID = *input.StableTaskID
	}
	if input.BotName != nil {
		raw.BotName = *input.BotName
	}
	if input.TaskName != nil {
		raw.TaskName = *input.TaskName
	}
	if input.Title != nil {
		raw.Title = *input.Title
	}
	if input.Summary != nil {
		raw.Summary = *input.Summary
	}
	if input.Tags != nil {
		raw.Tags = input.Tags
	}
	if input.ExternalID != nil {
		raw.ExternalID = *input.ExternalID
	}
	if input.AvatarURL != nil {
		raw.AvatarURL = *input.AvatarURL
	}
	in, err := normalizeReportRequest(raw)
	if err != nil {
		return nil, publishReportOutput{}, err
	}
	remote, _ := ctx.Value(mcpRemoteAddrKey{}).(string)
	if allowed, wait := s.allowWrite(remote, time.Now()); !allowed {
		return nil, publishReportOutput{}, &apiError{status: http.StatusTooManyRequests, code: "rate_limited", message: "too many report pushes; retry later", retryAfter: max(1, int(wait.Seconds()))}
	}
	report, duplicate, err := s.publishReport(ctx, in)
	if err != nil {
		return nil, publishReportOutput{}, fmt.Errorf("could not save report")
	}
	return nil, publishReportOutput{
		ReportID: report.ID, Created: !duplicate, Duplicate: duplicate,
		Source: report.Source, BotID: report.BotID, ExecutedAt: report.ExecutedAt,
	}, nil
}

func (s *Server) handleMCPSearchReports(ctx context.Context, _ *mcp.CallToolRequest, input searchReportsInput) (*mcp.CallToolResult, searchReportsOutput, error) {
	if !s.allowOAuthContext(ctx, oauthReadScope) {
		return nil, searchReportsOutput{}, errors.New("insufficient_scope: reports:read is required")
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 256 {
		return nil, searchReportsOutput{}, fmt.Errorf("query must contain 1 to 256 characters")
	}
	source := strings.TrimSpace(input.Source)
	if source != "" && !sourcePattern.MatchString(source) {
		return nil, searchReportsOutput{}, fmt.Errorf("source must be lowercase letters, digits, or hyphens")
	}
	botID := strings.TrimSpace(input.BotID)
	if len(botID) > 128 {
		return nil, searchReportsOutput{}, fmt.Errorf("bot_id must be at most 128 characters")
	}
	limit := 10
	if input.Limit != nil {
		limit = *input.Limit
		if limit < 1 || limit > 20 {
			return nil, searchReportsOutput{}, fmt.Errorf("limit must be between 1 and 20")
		}
	}
	from, err := dateBound(strings.TrimSpace(input.From), false)
	if err != nil {
		return nil, searchReportsOutput{}, fmt.Errorf("from must be an ISO date (YYYY-MM-DD)")
	}
	to, err := dateBound(strings.TrimSpace(input.To), true)
	if err != nil {
		return nil, searchReportsOutput{}, fmt.Errorf("to must be an ISO date (YYYY-MM-DD)")
	}
	if input.From != "" && input.To != "" && strings.TrimSpace(input.From) > strings.TrimSpace(input.To) {
		return nil, searchReportsOutput{}, fmt.Errorf("from must not be later than to")
	}
	remote, _ := ctx.Value(mcpRemoteAddrKey{}).(string)
	if !s.allowMCPRead(remote, time.Now()) {
		return nil, searchReportsOutput{}, fmt.Errorf("too many search requests; retry later")
	}
	page, err := s.store.Search(ctx, query, source, botID, from, to, input.Cursor, limit)
	if err != nil {
		if input.Cursor != "" {
			return nil, searchReportsOutput{}, fmt.Errorf("cursor is invalid")
		}
		log.Printf("MCP report search failed")
		return nil, searchReportsOutput{}, fmt.Errorf("could not search reports")
	}
	out := searchReportsOutput{Items: make([]reportSearchItem, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, report := range page.Items {
		name := report.DisplayName
		if name == "" {
			name = report.BotName
		}
		out.Items = append(out.Items, reportSearchItem{ReportID: report.ID, Source: report.Source, BotID: report.BotID, BotName: name, Title: report.Title, Summary: report.Summary, ExecutedAt: report.ExecutedAt, Tags: report.Tags})
	}
	return nil, out, nil
}

func (s *Server) allowMCPRead(remote string, now time.Time) bool {
	key := remote
	if host, _, err := net.SplitHostPort(remote); err == nil {
		key = host
	}
	if key == "" {
		key = "unknown"
	}
	key = "mcp-read:" + key
	allowed, _ := s.allowRate(key, 60, now)
	return allowed
}
