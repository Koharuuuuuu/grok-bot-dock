package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var ErrTopicStopped = errors.New("topic is archived or cannot accept a new task while stopping")
var ErrTopicConflict = errors.New("task already belongs to another topic")
var ErrTopicState = errors.New("invalid topic state or stale stop request")

type Topic struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Group        string      `json:"group"`
	Followed     bool        `json:"followed"`
	Pinned       bool        `json:"pinned"`
	Status       string      `json:"status"`
	Revision     int64       `json:"revision"`
	StopEvidence string      `json:"stop_evidence,omitempty"`
	ReportCount  int         `json:"report_count"`
	UnreadCount  int         `json:"unread_count"`
	LastReportAt string      `json:"last_report_at"`
	Summary      string      `json:"summary"`
	Agents       []TopicTask `json:"agents"`
}
type TopicTask struct {
	Source  string `json:"source"`
	BotID   string `json:"bot_id"`
	Stopped bool   `json:"stopped"`
}
type TopicPatch struct {
	Name     *string `json:"name,omitempty"`
	Group    *string `json:"group,omitempty"`
	Followed *bool   `json:"followed,omitempty"`
	Pinned   *bool   `json:"pinned,omitempty"`
	Action   string  `json:"action,omitempty"`
	Evidence string  `json:"evidence,omitempty"`
	Revision *int64  `json:"revision,omitempty"`
}
type TopicControl struct {
	TopicID         string `json:"topic_id"`
	Status          string `json:"status"`
	Revision        int64  `json:"revision"`
	RequestedAction string `json:"requested_action"`
}

func defaultTopicID(source, botID string) string {
	digest := sha256.Sum256([]byte(source + "\x00" + botID))
	return "topic-" + hex.EncodeToString(digest[:12])
}

func (s *Store) migrateTopics(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var applied int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=5`).Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return tx.Commit()
	}
	for _, q := range []string{
		`CREATE TABLE topics(id TEXT PRIMARY KEY,name TEXT NOT NULL,group_name TEXT NOT NULL DEFAULT '',followed INTEGER NOT NULL DEFAULT 0,pinned INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','stop_requested','archived')),revision INTEGER NOT NULL DEFAULT 0,stop_evidence TEXT NOT NULL DEFAULT '')`,
		`ALTER TABLE reports ADD COLUMN topic_id TEXT REFERENCES topics(id)`,
		`CREATE TABLE topic_tasks(topic_id TEXT NOT NULL REFERENCES topics(id),source TEXT NOT NULL,bot_id TEXT NOT NULL,stopped INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(source,bot_id),FOREIGN KEY(source,bot_id) REFERENCES bots(source,bot_id) ON DELETE CASCADE)`,
		`CREATE TABLE report_reads(report_id INTEGER PRIMARY KEY REFERENCES reports(id) ON DELETE CASCADE)`,
		`CREATE INDEX reports_topic_time_idx ON reports(topic_id,executed_at DESC,id DESC)`,
		`CREATE INDEX topic_tasks_topic_idx ON topic_tasks(topic_id)`,
	} {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT source,bot_id,COALESCE(NULLIF(display_name,''),bot_name),id,last_read_report_id FROM bots`)
	if err != nil {
		return err
	}
	type legacy struct {
		source, id, name string
		pk, read         int64
	}
	var old []legacy
	for rows.Next() {
		var b legacy
		if err = rows.Scan(&b.source, &b.id, &b.name, &b.pk, &b.read); err != nil {
			rows.Close()
			return err
		}
		old = append(old, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, b := range old {
		id := defaultTopicID(b.source, b.id)
		if _, err = tx.ExecContext(ctx, `INSERT INTO topics(id,name,followed) VALUES(?,?,1)`, id, b.name); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO topic_tasks(topic_id,source,bot_id) VALUES(?,?,?)`, id, b.source, b.id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE reports SET topic_id=? WHERE bot_pk=?`, id, b.pk); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO report_reads SELECT id FROM reports WHERE bot_pk=? AND id<=?`, b.pk, b.read); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at) VALUES(5,datetime('now'))`); err != nil {
		return err
	}
	return tx.Commit()
}

// Binding and report insertion share one transaction. Retries are resolved before
// this is called so they never create a topic or change its lifecycle.
func bindReportTopic(ctx context.Context, tx *sql.Tx, in ReportInput) (string, error) {
	var bound string
	var stopped bool
	err := tx.QueryRowContext(ctx, `SELECT topic_id,stopped FROM topic_tasks WHERE source=? AND bot_id=?`, in.Source, in.BotID).Scan(&bound, &stopped)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id := in.TopicID
	if bound != "" {
		if id != "" && id != bound {
			return "", ErrTopicConflict
		}
		id = bound
	}
	if id == "" {
		id = defaultTopicID(in.Source, in.BotID)
	}
	name := strings.TrimSpace(in.TopicName)
	if name == "" {
		name = in.BotName
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO topics(id,name) VALUES(?,?) ON CONFLICT(id) DO NOTHING`, id, name); err != nil {
		return "", err
	}
	var status string
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT status,revision FROM topics WHERE id=?`, id).Scan(&status, &revision); err != nil {
		return "", err
	}
	if status == "archived" || (status == "stop_requested" && (bound == "" || stopped)) {
		return "", ErrTopicStopped
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO topic_tasks(topic_id,source,bot_id) VALUES(?,?,?) ON CONFLICT(source,bot_id) DO NOTHING`, id, in.Source, in.BotID); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) Topics(ctx context.Context) ([]Topic, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.name,t.group_name,t.followed,t.pinned,t.status,t.revision,t.stop_evidence,COUNT(r.id),COUNT(CASE WHEN rr.report_id IS NULL THEN r.id END),COALESCE(MAX(r.executed_at),''),COALESCE((SELECT summary FROM reports WHERE topic_id=t.id ORDER BY executed_at DESC,id DESC LIMIT 1),'') FROM topics t LEFT JOIN reports r ON r.topic_id=t.id LEFT JOIN report_reads rr ON rr.report_id=r.id GROUP BY t.id ORDER BY t.pinned DESC,MAX(r.executed_at) DESC,t.id`)
	if err != nil {
		return nil, err
	}
	items := []Topic{}
	for rows.Next() {
		var t Topic
		if err = rows.Scan(&t.ID, &t.Name, &t.Group, &t.Followed, &t.Pinned, &t.Status, &t.Revision, &t.StopEvidence, &t.ReportCount, &t.UnreadCount, &t.LastReportAt, &t.Summary); err != nil {
			rows.Close()
			return nil, err
		}
		t.Agents = []TopicTask{}
		items = append(items, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	members, err := s.db.QueryContext(ctx, `SELECT topic_id,source,bot_id,stopped FROM topic_tasks ORDER BY source,bot_id`)
	if err != nil {
		return nil, err
	}
	defer members.Close()
	index := map[string]int{}
	for i := range items {
		index[items[i].ID] = i
	}
	for members.Next() {
		var id string
		var task TopicTask
		if err = members.Scan(&id, &task.Source, &task.BotID, &task.Stopped); err != nil {
			return nil, err
		}
		if i, ok := index[id]; ok {
			items[i].Agents = append(items[i].Agents, task)
		}
	}
	return items, members.Err()
}

func (s *Store) PatchTopic(ctx context.Context, id string, p TopicPatch) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT status,revision FROM topics WHERE id=?`, id).Scan(&status, &revision); err != nil {
		return err
	}
	for _, field := range []struct {
		column string
		value  *string
		max    int
	}{{"name", p.Name, 256}, {"group_name", p.Group, 128}} {
		if field.value != nil {
			v := strings.TrimSpace(*field.value)
			if len(v) > field.max || (field.column == "name" && v == "") {
				return fmt.Errorf("invalid topic %s", field.column)
			}
			if _, err = tx.ExecContext(ctx, `UPDATE topics SET `+field.column+`=? WHERE id=?`, v, id); err != nil {
				return err
			}
		}
	}
	if p.Followed != nil {
		if _, err = tx.ExecContext(ctx, `UPDATE topics SET followed=?,pinned=CASE WHEN ?=0 THEN 0 ELSE pinned END WHERE id=?`, *p.Followed, *p.Followed, id); err != nil {
			return err
		}
	}
	if p.Pinned != nil {
		if _, err = tx.ExecContext(ctx, `UPDATE topics SET pinned=?,followed=CASE WHEN ?=1 THEN 1 ELSE followed END WHERE id=?`, *p.Pinned, *p.Pinned, id); err != nil {
			return err
		}
	}
	switch p.Action {
	case "":
	case "request_stop":
		if status == "archived" {
			return ErrTopicState
		}
		if status == "active" {
			_, err = tx.ExecContext(ctx, `UPDATE topics SET status='stop_requested',revision=revision+1,stop_evidence='' WHERE id=?`, id)
			if err == nil {
				_, err = tx.ExecContext(ctx, `UPDATE topic_tasks SET stopped=0 WHERE topic_id=?`, id)
			}
		}
	case "confirm_stopped":
		if status != "stop_requested" || p.Revision == nil || *p.Revision != revision || strings.TrimSpace(p.Evidence) == "" || len(p.Evidence) > 1024 {
			return ErrTopicState
		}
		_, err = tx.ExecContext(ctx, `UPDATE topics SET status='archived',stop_evidence=? WHERE id=?`, strings.TrimSpace(p.Evidence), id)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE topic_tasks SET stopped=1 WHERE topic_id=?`, id)
		}
	case "restore":
		if status == "active" {
			break
		}
		_, err = tx.ExecContext(ctx, `UPDATE topics SET status='active',revision=revision+1,stop_evidence='' WHERE id=?`, id)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE topic_tasks SET stopped=0 WHERE topic_id=?`, id)
		}
	default:
		return ErrTopicState
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SyncTopic(ctx context.Context, source, botID string, revision int64, stopped bool) (TopicControl, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TopicControl{}, err
	}
	defer tx.Rollback()
	var c TopicControl
	if err = tx.QueryRowContext(ctx, `SELECT t.id,t.status,t.revision FROM topics t JOIN topic_tasks m ON m.topic_id=t.id WHERE m.source=? AND m.bot_id=?`, source, botID).Scan(&c.TopicID, &c.Status, &c.Revision); err != nil {
		return c, err
	}
	if stopped {
		if c.Status == "archived" && revision == c.Revision {
			c.RequestedAction = "stop"
			return c, tx.Commit()
		}
		if c.Status != "stop_requested" || revision != c.Revision {
			return c, ErrTopicState
		}
		if _, err = tx.ExecContext(ctx, `UPDATE topic_tasks SET stopped=1 WHERE source=? AND bot_id=?`, source, botID); err != nil {
			return c, err
		}
		var pending int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM topic_tasks WHERE topic_id=? AND stopped=0`, c.TopicID).Scan(&pending); err != nil {
			return c, err
		}
		if pending == 0 {
			c.Status = "archived"
			if _, err = tx.ExecContext(ctx, `UPDATE topics SET status='archived',stop_evidence='All contributing tasks acknowledged stopping' WHERE id=?`, c.TopicID); err != nil {
				return c, err
			}
		}
	}
	c.RequestedAction = "none"
	if c.Status != "active" {
		c.RequestedAction = "stop"
	}
	return c, tx.Commit()
}

func (s *Store) ReadReport(ctx context.Context, topicID string, reportID int64) error {
	result, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO report_reads(report_id) SELECT id FROM reports WHERE topic_id=? AND (?=0 OR id=?)`, topicID, reportID, reportID)
	if err != nil {
		return err
	}
	_ = result
	var exists int
	if reportID > 0 {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reports WHERE topic_id=? AND id=?`, topicID, reportID).Scan(&exists)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM topics WHERE id=?`, topicID).Scan(&exists)
	}
	if err == nil && exists == 0 {
		return sql.ErrNoRows
	}
	return err
}

func (s *Store) TopicReports(ctx context.Context, id, q, cursor string, limit int) (Page, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	where := ` WHERE r.topic_id=?`
	args := []any{id}
	if q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
		where += ` AND (r.title LIKE ? ESCAPE '\' OR r.summary LIKE ? ESCAPE '\' OR r.content_md LIKE ? ESCAPE '\')`
		args = append(args, like, like, like)
	}
	if cursor != "" {
		at, rid, err := decodeCursor(cursor)
		if err != nil {
			return Page{}, err
		}
		where += ` AND (r.executed_at<? OR (r.executed_at=? AND r.id<?))`
		args = append(args, at, at, rid)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, reportSelect+where+` ORDER BY r.executed_at DESC,r.id DESC LIMIT ?`, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	p := Page{Items: []Report{}}
	for rows.Next() {
		r, err := scanReport(rows, nil)
		if err != nil {
			return Page{}, err
		}
		p.Items = append(p.Items, r)
	}
	if err = rows.Err(); err != nil {
		return Page{}, err
	}
	if len(p.Items) > limit {
		r := p.Items[limit-1]
		p.Items = p.Items[:limit]
		p.NextCursor = encodeCursor(r.ExecutedAt, r.ID)
	}
	return p, nil
}
