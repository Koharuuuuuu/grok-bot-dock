package api

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Nyakooo/grok-bot-dock/internal/store"
)

var shareTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func (s *Server) requireShareOwner(w http.ResponseWriter, r *http.Request) bool {
	if id, _ := r.Context().Value(sessionContextKey{}).(string); id == "" {
		writeError(w, http.StatusForbidden, "session_required", "report shares require an owner browser session")
		return false
	}
	return true
}

func parseShareRoute(path string) (reportID int64, shareID int64, isList bool, ok bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/reports/"), "/")
	if len(parts) < 2 || parts[1] != "shares" {
		return 0, 0, false, false
	}
	reportID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || reportID < 1 {
		return 0, 0, false, false
	}
	if len(parts) == 2 {
		return reportID, 0, true, true
	}
	if len(parts) != 3 {
		return 0, 0, false, false
	}
	shareID, err = strconv.ParseInt(parts[2], 10, 64)
	return reportID, shareID, false, err == nil && shareID > 0
}

func (s *Server) manageReportShares(w http.ResponseWriter, r *http.Request) {
	if !s.requireShareOwner(w, r) {
		return
	}
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	if allowed, wait := s.allowRate("share-manage:"+remote, 60, time.Now()); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(wait.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}
	reportID, shareID, isList, ok := parseShareRoute(r.URL.Path)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	switch {
	case r.Method == http.MethodPost && isList:
		var in struct {
			Unit  string `json:"ttl_unit"`
			Value int64  `json:"ttl_value"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, s.maxBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&in); err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
				return
			}
			writeError(w, http.StatusBadRequest, "invalid_ttl", "ttl_unit must be hour or day and ttl_value must be within its allowed range")
			return
		}
		if in.Unit != "hour" && in.Unit != "day" || in.Unit == "hour" && (in.Value < 1 || in.Value > 168) || in.Unit == "day" && (in.Value < 1 || in.Value > 7) {
			writeError(w, http.StatusBadRequest, "invalid_ttl", "ttl_unit must be hour or day and ttl_value must be within its allowed range")
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid_request", "request must contain one JSON object")
			return
		}
		ttl := time.Duration(in.Value) * time.Hour
		if in.Unit == "day" {
			ttl *= 24
		}
		now := time.Now().UTC()
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			serverError(w, err)
			return
		}
		token := base64.RawURLEncoding.EncodeToString(secret)
		digest := sha256.Sum256([]byte(token))
		share, err := s.store.CreateReportShare(r.Context(), reportID, hex.EncodeToString(digest[:]), now, now.Add(ttl))
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "report not found")
			return
		}
		if errors.Is(err, store.ErrShareLimit) {
			writeError(w, http.StatusConflict, "share_limit", "this report already has the maximum number of active share links")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		shareURL := "/share/" + token
		writeJSON(w, http.StatusCreated, map[string]any{"share_id": share.ID, "share_path": shareURL, "expires_at": share.ExpiresAt})
	case r.Method == http.MethodGet && isList:
		items, err := s.store.ListReportShares(r.Context(), reportID)
		if err != nil {
			serverError(w, err)
			return
		}
		now := time.Now().UTC()
		for i := range items {
			switch {
			case items[i].RevokedAt != "":
				items[i].Status = "revoked"
			case !now.Before(parseTimestamp(items[i].ExpiresAt)):
				items[i].Status = "expired"
			default:
				items[i].Status = "active"
			}
			items[i].RevokedAt = ""
			items[i].ReportID = 0
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case r.Method == http.MethodDelete && !isList:
		_, err := s.store.RevokeReportShare(r.Context(), reportID, shareID, time.Now().UTC())
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "share not found")
			return
		}
		if err != nil {
			serverError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) publicShare(w http.ResponseWriter, r *http.Request) {
	setShareResponseHeaders(w)
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	if allowed, wait := s.allowRate("share-read:"+remote, 60, time.Now()); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(wait.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/api/v1/public/shares/")
	if !shareTokenPattern.MatchString(token) {
		writeError(w, http.StatusNotFound, "not_found", "share is unavailable")
		return
	}
	digest := sha256.Sum256([]byte(token))
	report, err := s.store.PublicShareReport(r.Context(), hex.EncodeToString(digest[:]), time.Now().UTC())
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "share is unavailable")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func setShareResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self'; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
}

func parseTimestamp(value string) time.Time {
	parsed, _ := time.Parse("2006-01-02T15:04:05.000000000Z", value)
	return parsed
}
