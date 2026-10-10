package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Nyakooo/grok-bot-dock/internal/api"
	"github.com/Nyakooo/grok-bot-dock/internal/events"
	"github.com/Nyakooo/grok-bot-dock/internal/store"
	"github.com/Nyakooo/grok-bot-dock/internal/webui"
)

func main() {
	token := os.Getenv("ACCESS_TOKEN")
	if token == "" {
		log.Fatal("ACCESS_TOKEN is required; refusing to start without API and reading protection")
	}
	port := env("PORT", "18765")
	data := env("DATA_DIR", "./data")
	if err := os.MkdirAll(data, 0700); err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(filepath.Join(data, "bolt-dock.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	max := int64(1 << 20)
	if v := os.Getenv("MAX_REQUEST_BYTES"); v != "" {
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil || n < 1024 {
			log.Fatal("MAX_REQUEST_BYTES must be an integer >= 1024")
		}
		max = n
	}
	oauthEnabled := strings.EqualFold(strings.TrimSpace(os.Getenv("OAUTH_ENABLED")), "true")
	oauthBase := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL"))
	oauthConfig := api.OAuthConfig{Enabled: oauthEnabled, PublicBaseURL: oauthBase}
	if !api.ValidateOAuthConfig(oauthConfig) {
		log.Fatal("when OAUTH_ENABLED=true, PUBLIC_BASE_URL must be a canonical HTTPS origin (HTTP is allowed only on localhost)")
	}
	srv := api.NewWithOAuth(db, events.New(), token, max, webui.Files, oauthConfig)
	addr := "0.0.0.0:" + port
	log.Printf("Bolt Dock listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}
func env(key, fallback string) string {
	if x := os.Getenv(key); x != "" {
		return x
	}
	return fallback
}
