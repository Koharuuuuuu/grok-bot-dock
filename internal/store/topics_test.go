package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestTopicsIdentityLifecycleAndIdempotency(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	input := report("grok", "airport", "run-1", "# 情报\n\n候选方案原文")
	input.TopicName = "机场监控"
	first, dup, err := s.CreateReport(ctx, input)
	if err != nil || dup || first.TopicID == "" {
		t.Fatalf("first: %+v %v %v", first, dup, err)
	}
	input.TopicID = "different-retry-topic"
	retry, dup, err := s.CreateReport(ctx, input)
	if err != nil || !dup || retry.TopicID != first.TopicID {
		t.Fatalf("retry changed topic: %+v %v %v", retry, dup, err)
	}
	input.ExternalID = "run-2"
	if _, _, err = s.CreateReport(ctx, input); !errors.Is(err, ErrTopicConflict) {
		t.Fatalf("conflicting task binding: %v", err)
	}
	input.TopicID = first.TopicID
	second, _, err := s.CreateReport(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	other := report("codex", "review", "review-1", "# 对照\n\n来自另一 Agent")
	other.TopicID = first.TopicID
	if _, _, err = s.CreateReport(ctx, other); err != nil {
		t.Fatal(err)
	}
	topics, err := s.Topics(ctx)
	if err != nil || len(topics) != 1 || len(topics[0].Agents) != 2 || topics[0].ReportCount != 3 {
		t.Fatalf("topic aggregation: %+v %v", topics, err)
	}
	if err = s.ReadReport(ctx, first.TopicID, second.ID); err != nil {
		t.Fatal(err)
	}
	legacyBots, _ := s.Bots(ctx, "grok")
	if len(legacyBots) != 1 || legacyBots[0].UnreadCount != 1 {
		t.Fatalf("legacy unread diverged: %+v", legacyBots)
	}
	topics, _ = s.Topics(ctx)
	if topics[0].UnreadCount != 2 {
		t.Fatalf("read watermarked other reports: %+v", topics[0])
	}
	if err = s.PatchTopic(ctx, first.TopicID, TopicPatch{Action: "request_stop"}); err != nil {
		t.Fatal(err)
	}
	poll, err := s.SyncTopic(ctx, "grok", "airport", 0, false)
	if err != nil || poll.RequestedAction != "stop" || poll.Revision != 1 {
		t.Fatalf("poll: %+v %v", poll, err)
	}
	if _, err = s.SyncTopic(ctx, "grok", "airport", 0, true); !errors.Is(err, ErrTopicState) {
		t.Fatalf("stale stop accepted: %v", err)
	}
	c, err := s.SyncTopic(ctx, "grok", "airport", 1, true)
	if err != nil || c.Status != "stop_requested" {
		t.Fatalf("one contributor archived all: %+v %v", c, err)
	}
	c, err = s.SyncTopic(ctx, "codex", "review", 1, true)
	if err != nil || c.Status != "archived" {
		t.Fatalf("all acknowledgements: %+v %v", c, err)
	}
	retry, dup, err = s.CreateReport(ctx, input)
	if err != nil || !dup || retry.ID != second.ID {
		t.Fatalf("archived retry: %+v %v %v", retry, dup, err)
	}
	input.ExternalID = "run-3"
	if _, _, err = s.CreateReport(ctx, input); !errors.Is(err, ErrTopicStopped) {
		t.Fatalf("archived write accepted: %v", err)
	}
	if err = s.PatchTopic(ctx, first.TopicID, TopicPatch{Action: "restore"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.CreateReport(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err = s.PatchTopic(ctx, first.TopicID, TopicPatch{Action: "request_stop"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SyncTopic(ctx, "grok", "airport", 1, true); !errors.Is(err, ErrTopicState) {
		t.Fatalf("old-cycle confirmation accepted: %v", err)
	}
	if err = s.PatchTopic(ctx, first.TopicID, TopicPatch{Action: "confirm_stopped"}); !errors.Is(err, ErrTopicState) {
		t.Fatalf("empty manual evidence accepted: %v", err)
	}
	currentRevision := int64(3)
	staleRevision := int64(1)
	if err = s.PatchTopic(ctx, first.TopicID, TopicPatch{Action: "confirm_stopped", Evidence: "stale owner dialog", Revision: &staleRevision}); !errors.Is(err, ErrTopicState) {
		t.Fatalf("stale owner confirmation accepted: %v", err)
	}
	if err = s.PatchTopic(ctx, first.TopicID, TopicPatch{Action: "confirm_stopped", Evidence: "owner stopped both external schedules", Revision: &currentRevision}); err != nil {
		t.Fatal(err)
	}
	topics, _ = s.Topics(ctx)
	if topics[0].StopEvidence != "owner stopped both external schedules" {
		t.Fatal("manual evidence lost")
	}
}

func TestTopicAndLegacyReadStateStayConsistent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, _, err := s.CreateReport(ctx, report("grok", "shared", "a", "# A"))
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.CreateReport(ctx, report("grok", "shared", "b", "# B"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ReadReport(ctx, a.TopicID, a.ID); err != nil {
		t.Fatal(err)
	}
	sources, _ := s.Sources(ctx)
	bots, _ := s.Bots(ctx, "")
	if sources[0].UnreadCount != 1 || bots[0].UnreadCount != 1 {
		t.Fatalf("topic->legacy unread: %+v %+v", sources, bots)
	}
	if err = s.MarkRead(ctx, "grok", "shared", b.ID); err != nil {
		t.Fatal(err)
	}
	topics, _ := s.Topics(ctx)
	if topics[0].UnreadCount != 0 {
		t.Fatalf("legacy->topic unread: %+v", topics)
	}
}

func TestScopedTopicSearchFiltersBeforePagination(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	group := "生活与消费"
	yes := true
	var firstTopic string
	for _, bot := range []string{"first", "second", "archived"} {
		in := report("grok", bot, bot, "# Evidence\n\n全局证据标记")
		in.TopicID = bot
		r, _, err := s.CreateReport(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if bot == "first" {
			firstTopic = r.TopicID
			if err = s.PatchTopic(ctx, r.TopicID, TopicPatch{Group: &group, Followed: &yes}); err != nil {
				t.Fatal(err)
			}
		}
		if bot == "archived" {
			if err = s.PatchTopic(ctx, r.TopicID, TopicPatch{Action: "request_stop"}); err != nil {
				t.Fatal(err)
			}
			revision := int64(1)
			if err = s.PatchTopic(ctx, r.TopicID, TopicPatch{Action: "confirm_stopped", Evidence: "Stopped in external platform", Revision: &revision}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, scope := range []TopicSearchScope{{Tab: "follow"}, {Tab: "all", Group: group}, {Tab: "archived"}} {
		p, err := s.SearchScoped(ctx, "全局证据标记", "", "", "", "", "", 1, scope)
		if err != nil {
			t.Fatal(err)
		}
		want := firstTopic
		if scope.Tab == "archived" {
			want = "archived"
		}
		if len(p.Items) != 1 || p.Items[0].TopicID != want || p.NextCursor != "" || len(p.TopicIDs) != 1 || p.TopicIDs[0] != want {
			t.Fatalf("scope %+v: %+v", scope, p)
		}
	}
	p, err := s.SearchScoped(ctx, "全局证据标记", "", "", "", "", "", 1, TopicSearchScope{Tab: "all"})
	if err != nil || len(p.TopicIDs) != 2 || p.NextCursor == "" {
		t.Fatalf("topic discovery limited to first report page: %+v %v", p, err)
	}
}

func TestTopicPaginationGroupingAndFailedPublishRollback(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	input := report("grok", "daily", "a", "# 内容")
	input.TopicID = "one"
	r, _, err := s.CreateReport(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"b", "c"} {
		input.ExternalID = id
		if _, _, err = s.CreateReport(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.TopicReports(ctx, r.TopicID, "", "", 2)
	if err != nil || len(p.Items) != 2 || p.NextCursor == "" {
		t.Fatalf("first page: %+v %v", p, err)
	}
	next, err := s.TopicReports(ctx, r.TopicID, "", p.NextCursor, 2)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != r.ID {
		t.Fatalf("next page: %+v %v", next, err)
	}
	name, group := "新的主题名", "生活与消费"
	pin := true
	if err = s.PatchTopic(ctx, r.TopicID, TopicPatch{Name: &name, Group: &group, Pinned: &pin}); err != nil {
		t.Fatal(err)
	}
	ts, _ := s.Topics(ctx)
	if ts[0].Name != name || ts[0].Group != group || !ts[0].Followed {
		t.Fatalf("patch: %+v", ts[0])
	}
	if err = s.PatchTopic(ctx, r.TopicID, TopicPatch{Action: "request_stop"}); err != nil {
		t.Fatal(err)
	}
	other := report("codex", "new-task", "new-1", "# New")
	other.TopicID = r.TopicID
	if _, _, err = s.CreateReport(ctx, other); !errors.Is(err, ErrTopicStopped) {
		t.Fatalf("new contributor while stopping: %v", err)
	}
	bots, _ := s.Bots(ctx, "")
	if len(bots) != 1 {
		t.Fatalf("failed publish left orphan bot: %+v", bots)
	}
	if err = s.ReadReport(ctx, "other-topic", r.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-topic read accepted: %v", err)
	}
}

func TestLegacyTopicMigrationIsRestartSafe(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE sources(source TEXT PRIMARY KEY,created_at TEXT NOT NULL);
 CREATE TABLE bots(id INTEGER PRIMARY KEY,source TEXT NOT NULL REFERENCES sources(source),bot_id TEXT NOT NULL,bot_name TEXT NOT NULL,display_name TEXT,avatar_url TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,updated_at TEXT NOT NULL,last_report_at TEXT NOT NULL,last_read_report_id INTEGER NOT NULL DEFAULT 0,UNIQUE(source,bot_id));
 CREATE TABLE reports(id INTEGER PRIMARY KEY,bot_pk INTEGER NOT NULL REFERENCES bots(id) ON DELETE CASCADE,external_id TEXT,executed_at TEXT NOT NULL,received_at TEXT NOT NULL,title TEXT NOT NULL DEFAULT '',summary TEXT NOT NULL DEFAULT '',tags_json TEXT NOT NULL DEFAULT '[]',content_md TEXT NOT NULL);
 INSERT INTO sources VALUES('grok','2026-10-01');
 INSERT INTO bots VALUES(1,'grok','legacy','old name','用户命名','','2026-10-01','2026-10-01','2026-10-01',1);
 INSERT INTO reports VALUES(1,1,'first','2026-10-01','2026-10-01','原始标题','原始摘要','[]','# 原始正文');`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		ts, err := s.Topics(ctx)
		if err != nil || len(ts) != 1 || ts[0].Name != "用户命名" || ts[0].UnreadCount != 0 || ts[0].ReportCount != 1 {
			s.Close()
			t.Fatalf("migration: %+v %v", ts, err)
		}
		r, err := s.GetReport(ctx, 1)
		if err != nil || r.Content != "# 原始正文" || r.TopicID != ts[0].ID {
			s.Close()
			t.Fatalf("original report lost: %+v %v", r, err)
		}
		s.Close()
	}
}
