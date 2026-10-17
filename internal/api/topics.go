package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/Nyakooo/grok-bot-dock/internal/store"
)

var topicIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

func decodeTopicBody(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		writeError(w, 400, "invalid_request", "invalid topic request")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		writeError(w, 400, "invalid_request", "one JSON object required")
		return false
	}
	return true
}
func topicError(w http.ResponseWriter, err error) {
	var invalid *apiError
	if errors.As(err, &invalid) {
		writeValidationError(w, invalid)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "not_found", "topic or task not found")
		return
	}
	if errors.Is(err, store.ErrTopicState) {
		writeError(w, 409, "invalid_topic_state", err.Error())
		return
	}
	serverError(w, err)
}
func (s *Server) topicsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/api/v1/topics" && r.Method == http.MethodGet {
		topics, err := s.store.Topics(r.Context())
		if err != nil {
			serverError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"items": topics})
		return
	}
	if r.URL.Path == "/api/v1/topics/sync" && r.Method == http.MethodPost {
		var in topicSyncInput
		if !decodeTopicBody(w, r, &in) {
			return
		}
		control, err := s.syncTopic(r.Context(), in)
		if err != nil {
			topicError(w, err)
			return
		}
		if in.Stopped {
			s.hub.Publish("topic.updated", control)
		}
		writeJSON(w, 200, control)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/topics/"), "/")
	if len(parts) > 2 || !topicIDPattern.MatchString(parts[0]) {
		writeError(w, 404, "not_found", "topic route not found")
		return
	}
	id := parts[0]
	if len(parts) == 1 && r.Method == http.MethodPatch {
		var p store.TopicPatch
		if !decodeTopicBody(w, r, &p) {
			return
		}
		if (p.Name != nil && (strings.TrimSpace(*p.Name) == "" || len(*p.Name) > 256)) || (p.Group != nil && len(*p.Group) > 128) || len(p.Evidence) > 1024 {
			writeError(w, 400, "invalid_topic", "invalid topic metadata")
			return
		}
		if err := s.store.PatchTopic(r.Context(), id, p); err != nil {
			topicError(w, err)
			return
		}
		s.hub.Publish("topic.updated", map[string]string{"topic_id": id})
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if len(parts) == 2 && parts[1] == "reports" && r.Method == http.MethodGet {
		q := r.URL.Query()
		if len(q.Get("q")) > 256 {
			writeError(w, 400, "invalid_query", "query too long")
			return
		}
		page, err := s.store.TopicReports(r.Context(), id, q.Get("q"), q.Get("cursor"), queryInt(q.Get("limit"), 20))
		if err != nil {
			writeError(w, 400, "invalid_query", "invalid report query")
			return
		}
		writeJSON(w, 200, page)
		return
	}
	if len(parts) == 2 && parts[1] == "read" && r.Method == http.MethodPost {
		var in struct {
			ReportID int64 `json:"report_id"`
		}
		if !decodeTopicBody(w, r, &in) {
			return
		}
		if in.ReportID < 0 {
			writeError(w, 400, "invalid_request", "positive report_id or zero for all required")
			return
		}
		if err := s.store.ReadReport(r.Context(), id, in.ReportID); err != nil {
			topicError(w, err)
			return
		}
		s.hub.Publish("topic.updated", map[string]string{"topic_id": id})
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	writeError(w, 404, "not_found", "topic route not found")
}
