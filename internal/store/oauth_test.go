package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOAuthAdditiveMigrationAndTokenSurviveRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = db.CreateReport(ctx, ReportInput{Source: "legacy", BotID: "bot", BotName: "legacy bot", ExecutedAt: time.Now().UTC(), Content: "historical report"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.RegisterOAuthClient(ctx, "client", "Client", []string{"https://client.example/callback"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CreateOAuthCode(ctx, "code-hash", OAuthCode{ClientID: "client", RedirectURI: "https://client.example/callback", Challenge: "challenge", Resource: "https://dock.example/mcp", Scopes: []string{"reports:read"}, ExpiresAt: time.Now().Add(time.Minute)}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	binding := OAuthCodeBinding{ClientID: "client", RedirectURI: "https://client.example/callback", Resource: "https://dock.example/mcp"}
	if _, err = db.ExchangeOAuthCode(ctx, "code-hash", binding, "token-hash", time.Now().Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CreateOAuthCode(ctx, "expired-code-hash", OAuthCode{ClientID: "client", RedirectURI: "https://client.example/callback", Challenge: "challenge", Resource: "https://dock.example/mcp", Scopes: []string{"reports:read"}, ExpiresAt: time.Now().Add(time.Minute)}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExchangeOAuthCode(ctx, "expired-code-hash", binding, "expired-token-hash", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.OAuthToken(ctx, "expired-token-hash", "https://dock.example/mcp"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired access token was accepted: %v", err)
	}
	if _, err = db.CreateOAuthCode(ctx, "expired-code-only-hash", OAuthCode{ClientID: "client", RedirectURI: "https://client.example/callback", Challenge: "challenge", Resource: "https://dock.example/mcp", Scopes: []string{"reports:read"}, ExpiresAt: time.Now().Add(-time.Minute)}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExchangeOAuthCode(ctx, "expired-code-only-hash", binding, "must-not-be-created", time.Now().Add(15*time.Minute)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired authorization code was exchangeable: %v", err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(t.TempDir(), "restored.db")
	if err = os.WriteFile(restoredPath, backup, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(restoredPath)
	if err != nil {
		t.Fatalf("open restored isolated backup: %v", err)
	}
	if _, err = restored.OAuthToken(ctx, "token-hash", "https://dock.example/mcp"); err != nil {
		t.Fatalf("restored backup lost OAuth token: %v", err)
	}
	if err = restored.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var migration int
	if err = db.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=2").Scan(&migration); err != nil || migration != 1 {
		t.Fatalf("OAuth migration record=%d err=%v", migration, err)
	}
	if err = db.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=3").Scan(&migration); err != nil || migration != 1 {
		t.Fatalf("CIMD client metadata migration record=%d err=%v", migration, err)
	}
	client, err := db.OAuthClient(ctx, "client")
	if err != nil || client.Registration != "manual" {
		t.Fatalf("legacy OAuth client registration type=%q err=%v", client.Registration, err)
	}
	var storedPlain int
	if err = db.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM oauth_access_tokens WHERE token_hash='raw-token'").Scan(&storedPlain); err != nil || storedPlain != 0 {
		t.Fatalf("plain token rows=%d err=%v", storedPlain, err)
	}
	scopes, err := db.OAuthToken(ctx, "token-hash", "https://dock.example/mcp")
	if err != nil || len(scopes) != 1 || scopes[0] != "reports:read" {
		t.Fatalf("persisted token scopes=%v err=%v", scopes, err)
	}
	page, err := db.ListReports(ctx, "legacy", "bot", "", "", 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].Content != "historical report" {
		t.Fatalf("legacy report changed: page=%#v err=%v", page, err)
	}
	if err = db.DisableOAuthClient(ctx, "client"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.OAuthToken(ctx, "token-hash", "https://dock.example/mcp"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("disabled client token error=%v", err)
	}
	if err = db.EnableOAuthClient(ctx, "client"); err != nil {
		t.Fatalf("enable previously disabled client: %v", err)
	}
	client, err = db.OAuthClient(ctx, "client")
	if err != nil || client.DisabledAt != "" {
		t.Fatalf("client was not re-enabled: disabled_at=%q err=%v", client.DisabledAt, err)
	}
	if _, err = db.OAuthToken(ctx, "token-hash", "https://dock.example/mcp"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("enabling client restored a revoked token: %v", err)
	}
	if err = db.EnableOAuthClient(ctx, "client"); err != nil {
		t.Fatalf("re-enabling active client should be idempotent: %v", err)
	}
	if err = db.EnableOAuthClient(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("enabling missing client error=%v", err)
	}
}

func TestOAuthV2DatabaseMigratesClientRegistrationType(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "oauth-v2.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RegisterOAuthClient(ctx, "v2-client", "V2 client", []string{"https://client.example/cb"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `ALTER TABLE oauth_clients DROP COLUMN registration_type; DELETE FROM schema_migrations WHERE version=3`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatalf("open v2 database with v3 additive migration: %v", err)
	}
	defer db.Close()
	client, err := db.OAuthClient(ctx, "v2-client")
	if err != nil || client.Registration != "manual" {
		t.Fatalf("migrated client registration type=%q err=%v", client.Registration, err)
	}
	var migrations int
	if err := db.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=3").Scan(&migrations); err != nil || migrations != 1 {
		t.Fatalf("v3 migration count=%d err=%v", migrations, err)
	}
}

func TestOAuthAuthorizationCodeCanOnlyBeConsumedOnceConcurrently(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "parallel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.RegisterOAuthClient(ctx, "parallel-client", "Parallel", []string{"https://client.example/cb"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateOAuthCode(ctx, "parallel-code", OAuthCode{ClientID: "parallel-client", RedirectURI: "https://client.example/cb", Challenge: "challenge", Resource: "https://dock.example/mcp", Scopes: []string{"reports:read"}, ExpiresAt: time.Now().Add(time.Minute)}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	successes := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := db.ExchangeOAuthCode(ctx, "parallel-code", OAuthCodeBinding{ClientID: "parallel-client", RedirectURI: "https://client.example/cb", Resource: "https://dock.example/mcp"}, fmt.Sprintf("token-%d", i), time.Now().Add(time.Minute))
			successes <- err
		}(i)
	}
	wg.Wait()
	close(successes)
	count := 0
	for err := range successes {
		if err == nil {
			count++
		} else if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("unexpected concurrent exchange error: %v", err)
		}
	}
	if count != 1 {
		t.Fatalf("successful concurrent code exchanges=%d want exactly one", count)
	}
}

func TestOAuthClientSecretRotationKeepsClientTypeAndDoesNotStorePlaintext(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "client-secret.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.RegisterOAuthClient(ctx, "confidential", "Confidential", []string{"https://client.example/cb"}, "old-hash"); err != nil {
		t.Fatal(err)
	}
	if err := db.RegisterOAuthClient(ctx, "public", "Public", []string{"http://127.0.0.1/cb"}, ""); err != nil {
		t.Fatal(err)
	}
	items, err := db.OAuthClients(ctx)
	if err != nil || len(items) != 2 {
		t.Fatalf("list OAuth clients: items=%#v err=%v", items, err)
	}
	byID := map[string]OAuthClient{}
	for _, item := range items {
		byID[item.ID] = item
	}
	if !byID["confidential"].Confidential || byID["confidential"].SecretHash != "" || byID["public"].Confidential || byID["public"].SecretHash != "" {
		t.Fatalf("client type or secret hash leaked in list: %#v", items)
	}
	if err := db.RotateOAuthClientSecret(ctx, "confidential", "new-hash"); err != nil {
		t.Fatalf("rotate confidential secret: %v", err)
	}
	client, err := db.OAuthClient(ctx, "confidential")
	if err != nil || client.SecretHash != "new-hash" {
		t.Fatalf("stored secret hash=%q err=%v", client.SecretHash, err)
	}
	if err := db.RotateOAuthClientSecret(ctx, "public", "new-hash"); !errors.Is(err, ErrOAuthPublicClient) {
		t.Fatalf("public client rotation error=%v want ErrOAuthPublicClient", err)
	}
	if err := db.RotateOAuthClientSecret(ctx, "missing", "new-hash"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing client rotation error=%v want sql.ErrNoRows", err)
	}
	if err := db.DisableOAuthClient(ctx, "confidential"); err != nil {
		t.Fatal(err)
	}
	if err := db.RotateOAuthClientSecret(ctx, "confidential", "another-hash"); !errors.Is(err, ErrOAuthDisabled) {
		t.Fatalf("disabled client rotation error=%v want ErrOAuthDisabled", err)
	}
}

func TestOAuthMetadataClientUpdateInvalidatesGrantsAndTokens(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "cimd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	clientID := "https://client.example/oauth/metadata.json"
	redirects := []string{"https://client.example/callback", "http://127.0.0.1:3434/callback"}
	if err := db.UpsertOAuthMetadataClient(ctx, clientID, "CIMD client", redirects); err != nil {
		t.Fatal(err)
	}
	client, err := db.OAuthClient(ctx, clientID)
	if err != nil || client.Registration != "cimd" || client.Confidential || len(client.Redirects) != 2 {
		t.Fatalf("CIMD client=%#v err=%v", client, err)
	}
	code := OAuthCode{ClientID: clientID, RedirectURI: redirects[0], Challenge: "challenge", Resource: "https://dock.example/mcp", Scopes: []string{"reports:read"}, ExpiresAt: time.Now().Add(time.Minute)}
	if _, err := db.CreateOAuthCode(ctx, "cimd-code", code, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExchangeOAuthCode(ctx, "cimd-code", OAuthCodeBinding{ClientID: clientID, RedirectURI: redirects[0], Resource: code.Resource}, "cimd-token", time.Now().Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertOAuthMetadataClient(ctx, clientID, "Renamed CIMD client", []string{"https://client.example/new-callback"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.OAuthToken(ctx, "cimd-token", code.Resource); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("token survived changed CIMD metadata: %v", err)
	}
	if err := db.RegisterOAuthClient(ctx, "manual-id", "Manual", []string{"https://client.example/cb"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertOAuthMetadataClient(ctx, "manual-id", "Impostor", []string{"https://attacker.example/cb"}); !errors.Is(err, ErrOAuthClientTypeConflict) {
		t.Fatalf("CIMD overwrote manual client: %v", err)
	}
	if err := db.DisableOAuthClient(ctx, clientID); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertOAuthMetadataClient(ctx, clientID, "CIMD client", redirects); !errors.Is(err, ErrOAuthDisabled) {
		t.Fatalf("metadata refresh re-enabled disabled client: %v", err)
	}
}

func TestOAuthMigrationPreservesLegacyReportsBotsFTSAndIdempotency(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-v1.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := ReportInput{Source: "legacy", BotID: "bot-1", BotName: "Legacy Bot", ExecutedAt: time.Now().UTC(), ExternalID: "legacy-task-1", Title: "Legacy search title", Content: "OAuth migration must retain searchable report content"}
	if _, duplicate, err := db.CreateReport(ctx, in); err != nil || duplicate {
		t.Fatalf("create old report duplicate=%v err=%v", duplicate, err)
	}
	if _, duplicate, err := db.CreateReport(ctx, in); err != nil || !duplicate {
		t.Fatalf("old report idempotency duplicate=%v err=%v", duplicate, err)
	}
	counts := func() map[string]int {
		t.Helper()
		got := make(map[string]int)
		for name, query := range map[string]string{
			"bots":    "SELECT COUNT(*) FROM bots",
			"reports": "SELECT COUNT(*) FROM reports",
			"fts":     "SELECT COUNT(*) FROM report_fts",
			"fts_map": "SELECT COUNT(*) FROM report_fts_map",
		} {
			var count int
			if err := db.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
				t.Fatalf("count %s: %v", name, err)
			}
			got[name] = count
		}
		return got
	}
	before := counts()
	if _, err := db.db.ExecContext(ctx, `DROP TABLE oauth_access_tokens; DROP TABLE oauth_authorization_codes; DROP TABLE oauth_grants; DROP TABLE oauth_audit_events; DROP TABLE oauth_clients; DELETE FROM schema_migrations WHERE version=2`); err != nil {
		t.Fatalf("prepare v1 schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatalf("apply additive OAuth migration: %v", err)
	}
	defer db.Close()
	after := counts()
	for key, want := range before {
		if after[key] != want {
			t.Errorf("legacy %s count changed across OAuth migration: before=%d after=%d", key, want, after[key])
		}
	}
	var indexCount int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='reports_external_id_unique'`).Scan(&indexCount); err != nil || indexCount != 1 {
		t.Fatalf("legacy external_id unique index count=%d err=%v", indexCount, err)
	}
	page, err := db.Search(ctx, "searchable report content", "legacy", "bot-1", "", "", "", 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].Content != in.Content {
		t.Fatalf("legacy FTS content not searchable: page=%#v err=%v", page, err)
	}
	if _, duplicate, err := db.CreateReport(ctx, in); err != nil || !duplicate {
		t.Fatalf("idempotent retry after migration duplicate=%v err=%v", duplicate, err)
	}
	if final := counts(); final["reports"] != before["reports"] {
		t.Fatalf("idempotent retry changed report count: before=%d after=%d", before["reports"], final["reports"])
	}
}

func TestOAuthTokenLookupRacingRevocationNeverSurvivesCommittedRevoke(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "revoke-race.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.RegisterOAuthClient(ctx, "race-client", "Race", []string{"https://client.example/cb"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateOAuthCode(ctx, "race-code", OAuthCode{ClientID: "race-client", RedirectURI: "https://client.example/cb", Challenge: "challenge", Resource: "https://dock.example/mcp", Scopes: []string{"reports:read"}, ExpiresAt: time.Now().Add(time.Minute)}, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExchangeOAuthCode(ctx, "race-code", OAuthCodeBinding{ClientID: "race-client", RedirectURI: "https://client.example/cb", Resource: "https://dock.example/mcp"}, "race-token-hash", time.Now().Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 20)
	var wg sync.WaitGroup
	for i := 0; i < cap(results)-1; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := db.OAuthToken(ctx, "race-token-hash", "https://dock.example/mcp")
			results <- err
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		results <- db.RevokeOAuthToken(ctx, "race-token-hash", "race-client")
	}()
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("unexpected concurrent lookup/revoke error: %v", err)
		}
	}
	if _, err := db.OAuthToken(ctx, "race-token-hash", "https://dock.example/mcp"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("token remained usable after revoke completed: %v", err)
	}
}

func TestSQLiteWriteWaitsForContextWhenBusyAndRecovers(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "busy.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	locker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	locker.SetMaxOpenConns(1)
	locker.SetMaxIdleConns(1)
	defer locker.Close()
	if _, err := locker.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		t.Fatal(err)
	}
	if _, err := locker.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("acquire SQLite writer lock: %v", err)
	}
	timeout, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	_, _, writeErr := db.CreateReport(timeout, ReportInput{Source: "busy", BotID: "writer", BotName: "Writer", ExecutedAt: time.Now().UTC(), Content: "lock test"})
	cancel()
	if writeErr == nil {
		t.Fatal("write unexpectedly passed while an independent connection held the SQLite writer lock")
	}
	if _, err := locker.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatalf("release SQLite writer lock: %v", err)
	}
	if _, _, err := db.CreateReport(ctx, ReportInput{Source: "busy", BotID: "writer", BotName: "Writer", ExecutedAt: time.Now().UTC(), Content: "lock recovery"}); err != nil {
		t.Fatalf("store did not recover after SQLite writer lock released: %v", err)
	}
}

func TestOAuthClientDisconnectRequestIsIdempotentAndAcknowledgmentDeletesRegistration(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "disconnect.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.RegisterOAuthClient(ctx, "disconnect-client", "Disconnect Client", []string{"https://client.example/callback"}, ""); err != nil {
		t.Fatal(err)
	}
	revision, err := db.RequestOAuthClientDeletion(ctx, "disconnect-client")
	if err != nil || revision != 1 {
		t.Fatalf("request deletion revision=%d err=%v", revision, err)
	}
	client, err := db.OAuthClient(ctx, "disconnect-client")
	if err != nil || client.DisconnectRevision != revision || client.DisconnectRequestedAt == "" {
		t.Fatalf("pending client state=%#v err=%v", client, err)
	}
	retry, err := db.RequestOAuthClientDeletion(ctx, "disconnect-client")
	if err != nil || retry != revision {
		t.Fatalf("retry changed active request revision=%d err=%v", retry, err)
	}
	if err = db.AcknowledgeOAuthClientDeletion(ctx, "disconnect-client", revision+1); !errors.Is(err, ErrOAuthDisconnectRevision) {
		t.Fatalf("stale acknowledgment err=%v", err)
	}
	if err = db.AcknowledgeOAuthClientDeletion(ctx, "disconnect-client", revision); err != nil {
		t.Fatal(err)
	}
	if _, err = db.OAuthClient(ctx, "disconnect-client"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("acknowledged client remains: %v", err)
	}
}
