package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

const timestampFormat = "2006-01-02T15:04:05.000000000Z"

type ReportInput struct {
	Source     string    `json:"source"`
	BotID      string    `json:"bot_id"`
	BotName    string    `json:"bot_name"`
	ExecutedAt time.Time `json:"-"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary"`
	Tags       []string  `json:"tags"`
	ExternalID string    `json:"external_id"`
	AvatarURL  string    `json:"avatar_url"`
	Content    string    `json:"content"`
}

type Report struct {
	ID          int64    `json:"id"`
	Source      string   `json:"source"`
	BotID       string   `json:"bot_id"`
	BotName     string   `json:"bot_name"`
	DisplayName string   `json:"display_name,omitempty"`
	AvatarURL   string   `json:"avatar_url,omitempty"`
	ExecutedAt  string   `json:"executed_at"`
	ReceivedAt  string   `json:"received_at"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Tags        []string `json:"tags"`
	Content     string   `json:"content"`
}

type Bot struct {
	Source           string  `json:"source"`
	BotID            string  `json:"bot_id"`
	BotName          string  `json:"bot_name"`
	DisplayName      string  `json:"display_name,omitempty"`
	AvatarURL        string  `json:"avatar_url,omitempty"`
	LastReportAt     string  `json:"last_report_at"`
	ReportCount      int     `json:"report_count"`
	UnreadCount      int     `json:"unread_count"`
	LatestReport     *Report `json:"latest_report,omitempty"`
	LastReadReportID int64   `json:"last_read_report_id"`
}

type Source struct {
	Source      string `json:"source"`
	BotCount    int    `json:"bot_count"`
	ReportCount int    `json:"report_count"`
}
type Page struct {
	Items      []Report `json:"items"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON"} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sources(source TEXT PRIMARY KEY, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS bots(id INTEGER PRIMARY KEY, source TEXT NOT NULL REFERENCES sources(source), bot_id TEXT NOT NULL, bot_name TEXT NOT NULL, display_name TEXT, avatar_url TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, last_report_at TEXT NOT NULL, last_read_report_id INTEGER NOT NULL DEFAULT 0, UNIQUE(source,bot_id));
CREATE TABLE IF NOT EXISTS reports(id INTEGER PRIMARY KEY, bot_pk INTEGER NOT NULL REFERENCES bots(id) ON DELETE CASCADE, external_id TEXT, executed_at TEXT NOT NULL, received_at TEXT NOT NULL, title TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '', tags_json TEXT NOT NULL DEFAULT '[]', content_md TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS reports_external_id_unique ON reports(bot_pk,external_id) WHERE external_id IS NOT NULL AND external_id <> '';
CREATE INDEX IF NOT EXISTS reports_bot_time_idx ON reports(bot_pk,executed_at DESC,id DESC);
CREATE VIRTUAL TABLE IF NOT EXISTS report_fts USING fts5(source UNINDEXED,bot_id UNINDEXED,title,summary,content,tags,tokenize='trigram');
CREATE TABLE IF NOT EXISTS report_fts_map(report_id INTEGER PRIMARY KEY REFERENCES reports(id) ON DELETE CASCADE, fts_rowid INTEGER UNIQUE NOT NULL);
CREATE TRIGGER IF NOT EXISTS reports_fts_insert AFTER INSERT ON reports BEGIN
 INSERT INTO report_fts(source,bot_id,title,summary,content,tags) SELECT b.source,b.bot_id,new.title,new.summary,new.content_md,new.tags_json FROM bots b WHERE b.id=new.bot_pk;
 INSERT INTO report_fts_map(report_id,fts_rowid) VALUES(new.id,last_insert_rowid());
END;
CREATE TRIGGER IF NOT EXISTS reports_fts_delete AFTER DELETE ON reports BEGIN
 DELETE FROM report_fts WHERE rowid=(SELECT fts_rowid FROM report_fts_map WHERE report_id=old.id);
 DELETE FROM report_fts_map WHERE report_id=old.id;
END;
INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(1,datetime('now'));`)
	if err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info(bots)")
	if err != nil {
		return err
	}
	hasDisplayName := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == "display_name" {
			hasDisplayName = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if !hasDisplayName {
		_, err = s.db.ExecContext(ctx, "ALTER TABLE bots ADD COLUMN display_name TEXT")
	}
	if err != nil {
		return err
	}
	return s.migrateOAuth(ctx)
}

func (s *Store) CreateReport(ctx context.Context, in ReportInput) (Report, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Report{}, false, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(timestampFormat)
	executed := in.ExecutedAt.UTC().Format(timestampFormat)
	if _, err = tx.ExecContext(ctx, "INSERT INTO sources(source,created_at) VALUES(?,?) ON CONFLICT(source) DO NOTHING", in.Source, now); err != nil {
		return Report{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bots(source,bot_id,bot_name,avatar_url,created_at,updated_at,last_report_at) VALUES(?,?,?,?,?,?,?)
 ON CONFLICT(source,bot_id) DO UPDATE SET bot_name=excluded.bot_name,avatar_url=CASE WHEN excluded.avatar_url<>'' THEN excluded.avatar_url ELSE bots.avatar_url END,updated_at=excluded.updated_at`, in.Source, in.BotID, in.BotName, in.AvatarURL, now, now, executed)
	if err != nil {
		return Report{}, false, err
	}
	var botPK int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM bots WHERE source=? AND bot_id=?", in.Source, in.BotID).Scan(&botPK); err != nil {
		return Report{}, false, err
	}
	if in.ExternalID != "" {
		var old Report
		old, err = scanReport(tx.QueryRowContext(ctx, reportSelect+" WHERE r.bot_pk=? AND r.external_id=?", botPK, in.ExternalID), nil)
		if err == nil {
			if err = tx.Commit(); err != nil {
				return Report{}, false, err
			}
			return old, true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Report{}, false, err
		}
	}
	tags, _ := json.Marshal(in.Tags)
	res, err := tx.ExecContext(ctx, "INSERT INTO reports(bot_pk,external_id,executed_at,received_at,title,summary,tags_json,content_md) VALUES(?,?,?,?,?,?,?,?)", botPK, nullIfEmpty(in.ExternalID), executed, now, in.Title, in.Summary, string(tags), in.Content)
	if err != nil {
		return Report{}, false, err
	}
	id, _ := res.LastInsertId()
	if _, err = tx.ExecContext(ctx, "UPDATE bots SET last_report_at=CASE WHEN last_report_at<? THEN ? ELSE last_report_at END WHERE id=?", executed, executed, botPK); err != nil {
		return Report{}, false, err
	}
	r, err := scanReport(tx.QueryRowContext(ctx, reportSelect+" WHERE r.id=?", id), nil)
	if err != nil {
		return Report{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Report{}, false, err
	}
	return r, false, nil
}
func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

const reportSelect = `SELECT r.id,b.source,b.bot_id,b.bot_name,COALESCE(b.display_name,''),b.avatar_url,r.executed_at,r.received_at,r.title,r.summary,r.tags_json,r.content_md FROM reports r JOIN bots b ON b.id=r.bot_pk`

type rowScanner interface{ Scan(...any) error }

func scanReport(row rowScanner, _ *Report) (Report, error) {
	var r Report
	var tags string
	err := row.Scan(&r.ID, &r.Source, &r.BotID, &r.BotName, &r.DisplayName, &r.AvatarURL, &r.ExecutedAt, &r.ReceivedAt, &r.Title, &r.Summary, &tags, &r.Content)
	if err == nil {
		_ = json.Unmarshal([]byte(tags), &r.Tags)
	}
	return r, err
}
func (s *Store) GetReport(ctx context.Context, id int64) (Report, error) {
	return scanReport(s.db.QueryRowContext(ctx, reportSelect+" WHERE r.id=?", id), nil)
}

func (s *Store) DeleteReport(ctx context.Context, id int64) (Report, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback()

	report, err := scanReport(tx.QueryRowContext(ctx, reportSelect+" WHERE r.id=?", id), nil)
	if err != nil {
		return Report{}, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM reports WHERE id=?", id); err != nil {
		return Report{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE bots SET
	last_report_at=COALESCE((SELECT MAX(executed_at) FROM reports WHERE bot_pk=bots.id),'')
	WHERE source=? AND bot_id=?`, report.Source, report.BotID); err != nil {
		return Report{}, err
	}
	if err = tx.Commit(); err != nil {
		return Report{}, err
	}
	return report, nil
}

func (s *Store) DeleteBot(ctx context.Context, source, botID string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var botPK int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM bots WHERE source=? AND bot_id=?", source, botID).Scan(&botPK); err != nil {
		return 0, err
	}
	var reportCount int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM reports WHERE bot_pk=?", botPK).Scan(&reportCount); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM bots WHERE id=?", botPK); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM sources WHERE source=? AND NOT EXISTS (SELECT 1 FROM bots WHERE bots.source=sources.source)", source); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return reportCount, nil
}

func (s *Store) RenameBot(ctx context.Context, source, botID, displayName string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE bots SET display_name=?,updated_at=? WHERE source=? AND bot_id=?", nullIfEmpty(displayName), time.Now().UTC().Format(timestampFormat), source, botID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) Sources(ctx context.Context) ([]Source, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.source,COUNT(DISTINCT b.id),COUNT(r.id) FROM sources s LEFT JOIN bots b ON b.source=s.source LEFT JOIN reports r ON r.bot_pk=b.id GROUP BY s.source ORDER BY s.source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		var x Source
		if err := rows.Scan(&x.Source, &x.BotCount, &x.ReportCount); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) Bots(ctx context.Context, source string) ([]Bot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT b.source,b.bot_id,b.bot_name,COALESCE(b.display_name,''),b.avatar_url,b.last_report_at,COUNT(r.id),COUNT(CASE WHEN r.id>b.last_read_report_id THEN 1 END),b.last_read_report_id FROM bots b LEFT JOIN reports r ON r.bot_pk=b.id WHERE (?='' OR b.source=?) GROUP BY b.id ORDER BY b.last_report_at DESC,b.id DESC`, source, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Bot{}
	for rows.Next() {
		var b Bot
		if err := rows.Scan(&b.Source, &b.BotID, &b.BotName, &b.DisplayName, &b.AvatarURL, &b.LastReportAt, &b.ReportCount, &b.UnreadCount, &b.LastReadReportID); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		r, err := s.LatestReport(ctx, out[i].Source, out[i].BotID)
		if err == nil {
			out[i].LatestReport = &r
		}
	}
	return out, nil
}
func (s *Store) LatestReport(ctx context.Context, source, botID string) (Report, error) {
	return scanReport(s.db.QueryRowContext(ctx, reportSelect+" WHERE b.source=? AND b.bot_id=? ORDER BY r.executed_at DESC,r.id DESC LIMIT 1", source, botID), nil)
}
func (s *Store) MarkRead(ctx context.Context, source, botID string, reportID int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE bots SET last_read_report_id=MAX(last_read_report_id,?) WHERE source=? AND bot_id=? AND EXISTS(SELECT 1 FROM reports r WHERE r.id=? AND r.bot_pk=bots.id)`, reportID, source, botID, reportID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func encodeCursor(t string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s|%d", t, id)))
}
func decodeCursor(v string) (string, int64, error) {
	b, e := base64.RawURLEncoding.DecodeString(v)
	if e != nil {
		return "", 0, e
	}
	parts := strings.Split(string(b), "|")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid cursor")
	}
	var id int64
	if _, e = fmt.Sscan(parts[1], &id); e != nil {
		return "", 0, e
	}
	return parts[0], id, nil
}
func (s *Store) ListReports(ctx context.Context, source, botID, date, cursor string, limit int) (Page, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	where := ` WHERE (?='' OR b.source=?) AND (?='' OR b.bot_id=?) AND (?='' OR substr(r.executed_at,1,10)=?)`
	args := []any{source, source, botID, botID, date, date}
	if cursor != "" {
		t, id, e := decodeCursor(cursor)
		if e != nil {
			return Page{}, e
		}
		where += ` AND (r.executed_at<? OR (r.executed_at=? AND r.id<?))`
		args = append(args, t, t, id)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, reportSelect+where+` ORDER BY r.executed_at DESC,r.id DESC LIMIT ?`, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	items := []Report{}
	for rows.Next() {
		r, e := scanReport(rows, nil)
		if e != nil {
			return Page{}, e
		}
		items = append(items, r)
	}
	if err = rows.Err(); err != nil {
		return Page{}, err
	}
	page := Page{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		page.Items = items[:limit]
		page.NextCursor = encodeCursor(last.ExecutedAt, last.ID)
	}
	return page, nil
}

func (s *Store) Search(ctx context.Context, q, source, botID, from, to, cursor string, limit int) (Page, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	base := reportSelect + ` JOIN report_fts_map fm ON fm.report_id=r.id JOIN report_fts f ON f.rowid=fm.fts_rowid WHERE (?='' OR b.source=?) AND (?='' OR b.bot_id=?) AND (?='' OR r.executed_at>=?) AND (?='' OR r.executed_at<?)`
	args := []any{source, source, botID, botID, from, from, to, to}
	if len([]rune(q)) >= 3 {
		base += ` AND report_fts MATCH ?`
		args = append(args, `"`+strings.ReplaceAll(q, `"`, `""`)+`"`)
	} else {
		base += ` AND (r.title LIKE ? ESCAPE '\' OR r.summary LIKE ? ESCAPE '\' OR r.content_md LIKE ? ESCAPE '\' OR r.tags_json LIKE ? ESCAPE '\')`
		like := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(q) + "%"
		args = append(args, like, like, like, like)
	}
	if cursor != "" {
		t, id, e := decodeCursor(cursor)
		if e != nil {
			return Page{}, e
		}
		base += ` AND (r.executed_at<? OR (r.executed_at=? AND r.id<?))`
		args = append(args, t, t, id)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, base+` ORDER BY r.executed_at DESC,r.id DESC LIMIT ?`, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	items := []Report{}
	for rows.Next() {
		r, e := scanReport(rows, nil)
		if e != nil {
			return Page{}, e
		}
		items = append(items, r)
	}
	if err = rows.Err(); err != nil {
		return Page{}, err
	}
	page := Page{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		page.Items = items[:limit]
		page.NextCursor = encodeCursor(last.ExecutedAt, last.ID)
	}
	return page, nil
}
func (s *Store) Stats(ctx context.Context, source string) (int, int, int64, error) {
	var bots, reports int
	var bytes int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT b.id),COUNT(r.id) FROM bots b LEFT JOIN reports r ON r.bot_pk=b.id WHERE (?='' OR b.source=?)`, source, source).Scan(&bots, &reports)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, file := range []string{s.path, s.path + "-wal", s.path + "-shm"} {
		info, statErr := os.Stat(file)
		if statErr == nil {
			bytes += info.Size()
		} else if !os.IsNotExist(statErr) {
			return 0, 0, 0, statErr
		}
	}
	return bots, reports, bytes, nil
}
