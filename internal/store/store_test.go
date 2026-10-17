package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func report(source, bot, external, content string) ReportInput {
	return ReportInput{Source: source, BotID: bot, BotName: "共享名称", ExecutedAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC), ExternalID: external, Content: content, Title: "今日 AI 动态", Summary: "人工智能重要消息", Tags: []string{"AI"}}
}

func TestSourceScopedIdentityAndExternalID(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	grok, dup, err := s.CreateReport(ctx, report("grok", "daily", "same", "# Grok\n\n人工智能日报"))
	if err != nil || dup {
		t.Fatalf("create grok: dup=%v err=%v", dup, err)
	}
	codex, dup, err := s.CreateReport(ctx, report("codex", "daily", "same", "# Codex\n\n人工智能分析"))
	if err != nil || dup {
		t.Fatalf("create codex: dup=%v err=%v", dup, err)
	}
	if grok.ID == codex.ID || grok.Source == codex.Source {
		t.Fatalf("source identities were merged: %#v %#v", grok, codex)
	}
	retry, dup, err := s.CreateReport(ctx, report("grok", "daily", "same", "different retry body"))
	if err != nil || !dup || retry.ID != grok.ID {
		t.Fatalf("idempotency failed: report=%#v dup=%v err=%v", retry, dup, err)
	}
	sources, err := s.Sources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].ReportCount != 1 || sources[1].ReportCount != 1 {
		t.Fatalf("unexpected sources: %#v", sources)
	}
}

func TestDeleteReportRemovesSearchIndexAndRefreshesBot(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	first := report("grok", "daily", "one", "# older-only-token")
	first.ExecutedAt = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	older, _, err := s.CreateReport(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	latest := report("grok", "daily", "two", "# remove-this-token")
	latest.ExecutedAt = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	removed, _, err := s.CreateReport(ctx, latest)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.DeleteReport(ctx, removed.ID)
	if err != nil || got.ID != removed.ID {
		t.Fatalf("delete report: report=%#v err=%v", got, err)
	}
	if _, err = s.DeleteReport(ctx, removed.ID); err == nil {
		t.Fatal("deleting the same report twice should return not found")
	}
	page, err := s.Search(ctx, "remove-this-token", "", "", "", "", "", 20)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("deleted report remains searchable: page=%#v err=%v", page, err)
	}
	bots, err := s.Bots(ctx, "")
	if err != nil || len(bots) != 1 || bots[0].ReportCount != 1 || bots[0].LatestReport == nil || bots[0].LatestReport.ID != older.ID {
		t.Fatalf("bot latest report was not refreshed: bots=%#v err=%v", bots, err)
	}
}

func TestDeleteBotCascadesReportsAndRetainsOrRemovesSources(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, in := range []ReportInput{
		report("grok", "daily", "g1", "grok-delete-token"),
		report("grok", "weekly", "g2", "shared-source-token"),
		report("codex", "daily", "c1", "other-source-token"),
	} {
		if _, _, err := s.CreateReport(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	count, err := s.DeleteBot(ctx, "grok", "daily")
	if err != nil || count != 1 {
		t.Fatalf("delete bot: count=%d err=%v", count, err)
	}
	page, err := s.Search(ctx, "grok-delete-token", "", "", "", "", "", 20)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("cascaded report remains searchable: page=%#v err=%v", page, err)
	}
	sources, err := s.Sources(ctx)
	if err != nil || len(sources) != 2 {
		t.Fatalf("source with remaining bots should remain: sources=%#v err=%v", sources, err)
	}
	count, err = s.DeleteBot(ctx, "grok", "weekly")
	if err != nil || count != 1 {
		t.Fatalf("delete last bot for source: count=%d err=%v", count, err)
	}
	sources, err = s.Sources(ctx)
	if err != nil || len(sources) != 1 || sources[0].Source != "codex" {
		t.Fatalf("empty source should be removed: sources=%#v err=%v", sources, err)
	}
	if _, err = s.DeleteBot(ctx, "grok", "weekly"); err == nil {
		t.Fatal("deleting a missing bot should return not found")
	}
}

func TestAvatarRetentionAndPersistence(t *testing.T) {
	db := filepath.Join(t.TempDir(), "persist.db")
	s, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	first := report("grok", "daily", "1", "# First")
	first.AvatarURL = "https://example.com/bot.png"
	if _, _, err = s.CreateReport(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := report("grok", "daily", "2", "# Second")
	if _, _, err = s.CreateReport(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bots, err := s.Bots(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 1 || bots[0].AvatarURL != "https://example.com/bot.png" || bots[0].ReportCount != 2 {
		t.Fatalf("data did not survive restart: %#v", bots)
	}
}

func TestDisplayNameMigrationAndPersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE sources(source TEXT PRIMARY KEY, created_at TEXT NOT NULL);
CREATE TABLE bots(id INTEGER PRIMARY KEY, source TEXT NOT NULL REFERENCES sources(source), bot_id TEXT NOT NULL, bot_name TEXT NOT NULL, avatar_url TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, last_report_at TEXT NOT NULL, last_read_report_id INTEGER NOT NULL DEFAULT 0, UNIQUE(source,bot_id));
INSERT INTO sources(source,created_at) VALUES('grok','2026-10-01');
INSERT INTO bots(source,bot_id,bot_name,created_at,updated_at,last_report_at) VALUES('grok','daily','Original name','2026-10-01','2026-10-01','2026-10-01');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("legacy schema migration: %v", err)
	}
	ctx := context.Background()
	before, err := s.Bots(ctx, "")
	if err != nil || len(before) != 1 || before[0].BotName != "Original name" {
		t.Fatalf("migration lost existing bot: %#v err=%v", before, err)
	}
	if err := s.RenameBot(ctx, "grok", "daily", "Custom name"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateReport(ctx, report("grok", "daily", "later", "new report")); err != nil {
		t.Fatal(err)
	}
	reports, err := s.ListReports(ctx, "grok", "daily", "", "", 20)
	if err != nil || len(reports.Items) != 1 || reports.Items[0].BotName != "共享名称" || reports.Items[0].DisplayName != "Custom name" {
		t.Fatalf("raw and display names were not separated: %#v err=%v", reports, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Bots(ctx, "")
	if err != nil || len(got) != 1 || got[0].DisplayName != "Custom name" || got[0].ReportCount != 1 {
		t.Fatalf("custom name did not survive restart: %#v err=%v", got, err)
	}
}

func TestUnreadCountTracksReportsAfterLastRead(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	first, _, err := s.CreateReport(ctx, report("grok", "daily", "one", "# first"))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.CreateReport(ctx, report("grok", "daily", "two", "# second"))
	if err != nil {
		t.Fatal(err)
	}
	bots, err := s.Bots(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if bots[0].UnreadCount != 2 {
		t.Fatalf("initial unread count=%d want 2", bots[0].UnreadCount)
	}
	if err := s.MarkRead(ctx, "grok", "daily", first.ID); err != nil {
		t.Fatal(err)
	}
	bots, err = s.Bots(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if bots[0].UnreadCount != 1 {
		t.Fatalf("unread after first read=%d want 1", bots[0].UnreadCount)
	}
	if err := s.MarkRead(ctx, "grok", "daily", second.ID); err != nil {
		t.Fatal(err)
	}
	bots, err = s.Bots(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if bots[0].UnreadCount != 0 {
		t.Fatalf("unread after second read=%d want 0", bots[0].UnreadCount)
	}
	if err := s.MarkRead(ctx, "grok", "daily", second.ID+100); err == nil {
		t.Fatal("marking a report from another bot should fail")
	}
}

func TestChineseSearchShortSearchAndSourceFilter(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, in := range []ReportInput{report("grok", "daily", "g1", "# AI\n\n人工智能正在进步"), report("codex", "daily", "c1", "# AI\n\n人工智能开发工具")} {
		if _, _, err := s.CreateReport(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		q, source string
		want      int
	}{{"人工智能", "", 2}, {"智能", "", 2}, {"开发", "codex", 1}} {
		p, err := s.Search(ctx, tc.q, tc.source, "", "", "", "", 20)
		if err != nil {
			t.Fatalf("search %q: %v", tc.q, err)
		}
		if len(p.Items) != tc.want {
			t.Errorf("search %q source %q: got %d want %d", tc.q, tc.source, len(p.Items), tc.want)
		}
	}
}

func TestStableCursorPagination(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		in := report("grok", "daily", fmt.Sprint(i), "# same timestamp")
		in.ExecutedAt = at
		if _, _, err := s.CreateReport(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.ListReports(ctx, "", "", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("bad first page: %#v", first)
	}
	second, err := s.ListReports(ctx, "", "", "", first.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 2 || second.Items[0].ID == first.Items[1].ID {
		t.Fatalf("bad second page: %#v", second)
	}
}

func TestTimestampOrderingAcrossFractionalSeconds(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for _, entry := range []struct {
		title string
		nanos int
	}{{"later", 900_000_000}, {"earlier", 100_000_000}, {"whole", 0}} {
		in := report("grok", "daily", entry.title, "# timestamp order")
		in.ExecutedAt = base.Add(time.Duration(entry.nanos))
		in.Title = entry.title
		if _, _, err := s.CreateReport(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListReports(ctx, "", "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{page.Items[0].Title, page.Items[1].Title, page.Items[2].Title}; got[0] != "later" || got[1] != "earlier" || got[2] != "whole" {
		t.Fatalf("timestamps sorted incorrectly: %v", got)
	}
}

func TestConcurrentWrites(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := report("codex", fmt.Sprintf("bot-%02d", i), "", fmt.Sprintf("# report %d", i))
			_, _, err := s.CreateReport(context.Background(), in)
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	bots, err := s.Bots(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 20 {
		t.Fatalf("got %d concurrent bots, want 20", len(bots))
	}
}

func TestReportSharesDigestExpiryRevocationAndCascade(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	createdReport, _, err := s.CreateReport(ctx, report("grok", "daily", "share-1", "# Shared"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	digest := "digest:opaque-token"
	share, err := s.CreateReportShare(ctx, createdReport.ID, digest, now, expires)
	if err != nil {
		t.Fatal(err)
	}
	if share.ID == 0 || share.ReportID != createdReport.ID || share.ExpiresAt == "" {
		t.Fatalf("unexpected share metadata: %#v", share)
	}
	public, err := s.PublicShareReport(ctx, digest, now)
	if err != nil || public.Title != "今日 AI 动态" || public.BotName != "共享名称" || len(public.Tags) != 1 || public.Content != "# Shared" {
		t.Fatalf("public projection failed: %#v err=%v", public, err)
	}
	if _, err := s.PublicShareReport(ctx, digest, expires); err != sql.ErrNoRows {
		t.Fatalf("share remained valid at expiry: %v", err)
	}
	items, err := s.ListReportShares(ctx, createdReport.ID)
	if err != nil || len(items) != 1 || items[0].ID != share.ID {
		t.Fatalf("share listing failed: %#v err=%v", items, err)
	}
	if changed, err := s.RevokeReportShare(ctx, createdReport.ID, share.ID, now.Add(2*time.Minute)); err != nil || !changed {
		t.Fatalf("share revoke failed: changed=%v err=%v", changed, err)
	}
	if changed, err := s.RevokeReportShare(ctx, createdReport.ID, share.ID, now.Add(3*time.Minute)); err != nil || changed {
		t.Fatalf("revoke should be idempotent: changed=%v err=%v", changed, err)
	}
	if _, err := s.PublicShareReport(ctx, digest, now); err != sql.ErrNoRows {
		t.Fatalf("revoked share remained readable: %v", err)
	}
	if _, err := s.DeleteReport(ctx, createdReport.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM report_shares WHERE report_id=?`, createdReport.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("report deletion did not cascade share metadata: count=%d err=%v", count, err)
	}
}

func TestReportShareConcurrentLimitAndCleanup(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	createdReport, _, err := s.CreateReport(ctx, report("grok", "daily", "share-limit", "# Shared"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for i := 0; i < 28; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.CreateReportShare(ctx, createdReport.ID, fmt.Sprintf("digest-%d", i), now, now.Add(time.Hour))
			if err == nil {
				mu.Lock()
				created++
				mu.Unlock()
			} else if err != ErrShareLimit {
				t.Errorf("concurrent create: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if created != 20 {
		t.Fatalf("concurrent creation admitted %d links, want exactly 20", created)
	}
	items, err := s.ListReportShares(ctx, createdReport.ID)
	if err != nil || len(items) != 20 {
		t.Fatalf("share list count=%d err=%v", len(items), err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE report_shares SET expires_at=? WHERE id=?`, now.Add(-time.Second).Format(timestampFormat), items[0].ID); err != nil {
		t.Fatal(err)
	}
	removed, err := s.CleanupReportShares(ctx, now, 200)
	if err != nil || removed != 1 {
		t.Fatalf("cleanup removed=%d err=%v, want 1", removed, err)
	}
}

func TestReportShareMigrationAndSQLiteBackupRestore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, _, err := s.CreateReport(ctx, report("grok", "daily", "preserve", "# Existing report"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `DROP TABLE report_shares; DELETE FROM schema_migrations WHERE version=4`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dbPath)
	if err != nil {
		t.Fatalf("additive v4 migration failed: %v", err)
	}
	var versionCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=4`).Scan(&versionCount); err != nil || versionCount != 1 {
		t.Fatalf("schema v4 migration count=%d err=%v", versionCount, err)
	}
	if _, err := s.GetReport(ctx, created.ID); err != nil {
		t.Fatalf("existing report did not survive additive migration: %v", err)
	}
	now := time.Now().UTC()
	digest := "backup-digest-only"
	if _, err := s.CreateReportShare(ctx, created.ID, digest, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(dir, "restored.db")
	backup, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, backup, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(backupPath)
	if err != nil {
		t.Fatalf("restored database open: %v", err)
	}
	defer restored.Close()
	public, err := restored.PublicShareReport(ctx, digest, now)
	if err != nil || public.Content != "# Existing report" {
		t.Fatalf("restored backup lost share/report: %#v err=%v", public, err)
	}
}
