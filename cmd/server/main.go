package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Nyakooo/grok-bot-dock/internal/api"
	"github.com/Nyakooo/grok-bot-dock/internal/events"
	"github.com/Nyakooo/grok-bot-dock/internal/store"
	"github.com/Nyakooo/grok-bot-dock/internal/webui"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	token := os.Getenv("ACCESS_TOKEN")
	if token == "" {
		return errors.New("ACCESS_TOKEN is required; refusing to start without API and reading protection")
	}
	port := env("PORT", "18765")
	data := env("DATA_DIR", "./data")
	if err := os.MkdirAll(data, 0700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	db, err := store.Open(filepath.Join(data, "bolt-dock.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()

	max := int64(1 << 20)
	if v := os.Getenv("MAX_REQUEST_BYTES"); v != "" {
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil || n < 1024 {
			return errors.New("MAX_REQUEST_BYTES must be an integer >= 1024")
		}
		max = n
	}
	oauthEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("OAUTH_ENABLED")), "true")
	oauthBase := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL"))
	oauthConfig := api.OAuthConfig{Enabled: oauthEnabled, PublicBaseURL: oauthBase}
	if !api.ValidateOAuthConfig(oauthConfig) {
		return errors.New("when OAUTH_ENABLED=true, PUBLIC_BASE_URL must be a canonical HTTPS origin (HTTP is allowed only on localhost)")
	}

	application := api.NewWithOAuth(db, events.New(), token, max, webui.Files, oauthConfig)
	srv := &http.Server{
		Addr:    "0.0.0.0:" + port,
		Handler: application.Handler(),
	}
	stop, stopSignal := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignal()
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	log.Printf("Bolt Dock listening on %s", srv.Addr)

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-stop.Done():
		log.Printf("shutdown signal received; draining requests and checkpointing SQLite WAL")
	}

	if err := gracefulShutdown(srv, application.ShutdownStreams, db.Checkpoint); err != nil {
		return err
	}
	log.Printf("shutdown complete; SQLite WAL checkpointed")
	return nil
}

func gracefulShutdown(srv *http.Server, closeStreams func(), checkpoint func(context.Context) error) error {
	closeStreams()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Second)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		cancelShutdown()
		_ = srv.Close()
		return fmt.Errorf("graceful HTTP shutdown: %w", err)
	}
	cancelShutdown()

	checkpointCtx, cancelCheckpoint := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCheckpoint()
	if err := checkpoint(checkpointCtx); err != nil {
		return fmt.Errorf("checkpoint database before exit: %w", err)
	}
	return nil
}

func env(key, fallback string) string {
	if x := os.Getenv(key); x != "" {
		return x
	}
	return fallback
}
