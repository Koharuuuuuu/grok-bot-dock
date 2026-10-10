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
