package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrShareLimit = errors.New("active report share limit reached")

type ReportShare struct {
	ID        int64  `json:"share_id"`
	ReportID  int64  `json:"report_id,omitempty"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	RevokedAt string `json:"revoked_at,omitempty"`
	Status    string `json:"status,omitempty"`
}

// PublicShareReport is deliberately a narrow projection for anonymous readers.
type PublicShareReport struct {
	Source     string   `json:"source"`
	BotName    string   `json:"bot_name"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Tags       []string `json:"tags"`
	ExecutedAt string   `json:"executed_at"`
	Content    string   `json:"content"`
	ExpiresAt  string   `json:"expires_at"`
}

func (s *Store) migrateShares(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS report_shares (id INTEGER PRIMARY KEY, report_id INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE, token_hash TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, expires_at TEXT NOT NULL, revoked_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS report_shares_report_idx ON report_shares(report_id, expires_at)`,
		`CREATE INDEX IF NOT EXISTS report_shares_expiry_idx ON report_shares(expires_at)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(4, datetime('now'))`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateReportShare inserts only while the report exists and fewer than 20
// unexpired, unrevoked links exist. The INSERT...SELECT is one atomic write.
func (s *Store) CreateReportShare(ctx context.Context, reportID int64, tokenHash string, now, expires time.Time) (ReportShare, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO report_shares(report_id,token_hash,created_at,expires_at)
	SELECT ?,?,?,? WHERE EXISTS(SELECT 1 FROM reports WHERE id=?)
	AND (SELECT COUNT(*) FROM report_shares WHERE report_id=? AND revoked_at='' AND expires_at>?) < 20`,
		reportID, tokenHash, now.UTC().Format(timestampFormat), expires.UTC().Format(timestampFormat), reportID, reportID, now.UTC().Format(timestampFormat))
	if err != nil {
		return ReportShare{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return ReportShare{}, err
	}
	if rows == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM reports WHERE id=?)`, reportID).Scan(&exists); err != nil {
			return ReportShare{}, err
		}
		if exists == 0 {
			return ReportShare{}, sql.ErrNoRows
		}
		return ReportShare{}, ErrShareLimit
	}
	var share ReportShare
	err = s.db.QueryRowContext(ctx, `SELECT id,report_id,created_at,expires_at,revoked_at FROM report_shares WHERE token_hash=?`, tokenHash).
		Scan(&share.ID, &share.ReportID, &share.CreatedAt, &share.ExpiresAt, &share.RevokedAt)
	return share, err
}

func (s *Store) ListReportShares(ctx context.Context, reportID int64) ([]ReportShare, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,report_id,created_at,expires_at,revoked_at FROM report_shares WHERE report_id=? ORDER BY created_at DESC,id DESC`, reportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ReportShare, 0)
	for rows.Next() {
		var item ReportShare
		if err := rows.Scan(&item.ID, &item.ReportID, &item.CreatedAt, &item.ExpiresAt, &item.RevokedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RevokeReportShare(ctx context.Context, reportID, shareID int64, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE report_shares SET revoked_at=? WHERE report_id=? AND id=? AND revoked_at=''`, now.UTC().Format(timestampFormat), reportID, shareID)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed > 0 {
		return changed > 0, err
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM report_shares WHERE report_id=? AND id=?)`, reportID, shareID).Scan(&exists); err != nil {
		return false, err
	}
	if exists == 0 {
		return false, sql.ErrNoRows
	}
	return false, nil
}

func (s *Store) PublicShareReport(ctx context.Context, tokenHash string, now time.Time) (PublicShareReport, error) {
	var report PublicShareReport
	var tags string
	err := s.db.QueryRowContext(ctx, `SELECT b.source,COALESCE(NULLIF(b.display_name,''),b.bot_name),r.title,r.summary,r.tags_json,r.executed_at,r.content_md,sh.expires_at
	FROM report_shares sh JOIN reports r ON r.id=sh.report_id JOIN bots b ON b.id=r.bot_pk
	WHERE sh.token_hash=? AND sh.revoked_at='' AND sh.expires_at>?`, tokenHash, now.UTC().Format(timestampFormat)).
		Scan(&report.Source, &report.BotName, &report.Title, &report.Summary, &tags, &report.ExecutedAt, &report.Content, &report.ExpiresAt)
	if err != nil {
		return report, err
	}
	if err := json.Unmarshal([]byte(tags), &report.Tags); err != nil {
		return PublicShareReport{}, err
	}
	if report.Tags == nil {
		report.Tags = []string{}
	}
	return report, nil
}

// CleanupReportShares removes only unusable metadata in bounded batches.
func (s *Store) CleanupReportShares(ctx context.Context, now time.Time, limit int) (int64, error) {
	if limit < 1 || limit > 1000 {
		limit = 200
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM report_shares WHERE id IN (
	SELECT id FROM report_shares WHERE revoked_at<>'' OR expires_at<=? ORDER BY id LIMIT ?
	)`, now.UTC().Format(timestampFormat), limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
