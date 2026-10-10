package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpRemoteAddrKey struct{}

type publishReportInput struct {
	ExecutedAt   string   `json:"executed_at" jsonschema:"RFC3339 execution time of the completed report"`
	Content      string   `json:"content" jsonschema:"Complete Markdown report; publish only a confirmed final report, never drafts, logs, intermediate results, or failed tasks"`
	Source       *string  `json:"source,omitempty" jsonschema:"Stable lowercase source identifier; omitted or blank is stored as unknown"`
	BotID        *string  `json:"bot_id,omitempty" jsonschema:"Stable Bot identifier; provide this or stable_task_id"`
	StableTaskID *string  `json:"stable_task_id,omitempty" jsonschema:"Stable task identity; deterministically maps to the same Bot when bot_id is not supplied"`
	BotName      *string  `json:"bot_name,omitempty" jsonschema:"Bot display name reported by the source"`
	TaskName     *string  `json:"task_name,omitempty" jsonschema:"Stable task name used as a Bot name fallback"`
	Title        *string  `json:"title,omitempty" jsonschema:"Report title"`
	Summary      *string  `json:"summary,omitempty" jsonschema:"Short report summary"`
	Tags         []string `json:"tags,omitempty" jsonschema:"Report tags"`
	ExternalID   *string  `json:"external_id,omitempty" jsonschema:"Stable identifier for this execution; reuse it when retrying to make publication idempotent"`
	AvatarURL    *string  `json:"avatar_url,omitempty" jsonschema:"Optional HTTPS avatar URL"`
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
	Query  string `json:"query" jsonschema:"Non-empty full-text search query, including Chinese text"`
	Source string `json:"source,omitempty" jsonschema:"Optional source filter"`
	BotID  string `json:"bot_id,omitempty" jsonschema:"Optional stable Bot ID filter"`
	From   string `json:"from,omitempty" jsonschema:"Inclusive start date in YYYY-MM-DD format"`
	To     string `json:"to,omitempty" jsonschema:"Inclusive end date in YYYY-MM-DD format"`
	Limit  *int   `json:"limit,omitempty" jsonschema:"Maximum results per page; defaults to 10 and cannot exceed 20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor returned by a previous search_reports call"`
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
	setSchemaMaxLength(publishSchema, "content", int(s.maxBytes))
	publishSchema.Properties["executed_at"].Format = "date-time"
	publishSchema.Properties["content"].MinLength = jsonschema.Ptr(1)
	publishSchema.AnyOf = []*jsonschema.Schema{
		{Required: []string{"bot_id"}},
		{Required: []string{"stable_task_id"}},
	}
	if tags := publishSchema.Properties["tags"]; tags != nil && tags.Items != nil {
		tags.MaxItems = jsonschema.Ptr(32)
		tags.Items.MaxLength = jsonschema.Ptr(128)
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "publish_report",
		Title:       "Publish final report",
		Description: "Publish a completed, confirmed final Markdown report to Bolt Dock. Do not submit drafts, debug logs, intermediate results, or failed tasks. This writes a report and may create a Bot; it never deletes or renames data. Supply a stable bot_id or stable_task_id, and reuse external_id for retries. The server returns metadata only, not the report body.",
		InputSchema: publishSchema,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &nonDestructive, IdempotentHint: false, OpenWorldHint: &closedWorld},
	}, s.handleMCPPublishReport)
	searchSchema, err := jsonschema.For[searchReportsInput](nil)
	if err != nil {
		panic(fmt.Sprintf("build search_reports schema: %v", err))
	}
	setSchemaMaxLength(searchSchema, "query", 256)
	setSchemaMaxLength(searchSchema, "source", 63)
	setSchemaMaxLength(searchSchema, "bot_id", 128)
	setSchemaMaxLength(searchSchema, "from", 10)
	setSchemaMaxLength(searchSchema, "to", 10)
	setSchemaMaxLength(searchSchema, "cursor", int(s.maxBytes))
	searchSchema.Properties["query"].MinLength = jsonschema.Ptr(1)
	searchSchema.Properties["from"].Pattern = `^\d{4}-\d{2}-\d{2}$`
	searchSchema.Properties["to"].Pattern = `^\d{4}-\d{2}-\d{2}$`
	if limit := searchSchema.Properties["limit"]; limit != nil {
		limit.Minimum = jsonschema.Ptr(float64(1))
		limit.Maximum = jsonschema.Ptr(float64(20))
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_reports",
		Title:       "Search report history",
		Description: "Search stored report history by full-text query, including Chinese, with optional source, Bot, date, and cursor filters. Read-only; returns report metadata and summaries, never full Markdown.",
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
		if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "invalid_origin", "cross-origin MCP requests are not allowed")
			return
		}
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		got := ""
		if strings.HasPrefix(header, prefix) {
			got = strings.TrimPrefix(header, prefix)
		}
		valid := s.token != "" && len(got) == len(s.token) && subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
		if !valid {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Bolt Dock MCP"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
			return
		}
		ctx := context.WithValue(r.Context(), mcpRemoteAddrKey{}, r.RemoteAddr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleMCPPublishReport(ctx context.Context, _ *mcp.CallToolRequest, input publishReportInput) (*mcp.CallToolResult, publishReportOutput, error) {
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
