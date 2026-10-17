package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/Nyakooo/grok-bot-dock/internal/events"
)

func TestGracefulShutdownStopsHTTPBeforeCheckpoint(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := events.New()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		stream := hub.Subscribe()
		defer hub.Unsubscribe(stream)
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(": connected\n\n"))
		flusher.Flush()
		for range stream {
		}
	})}
	serveResult := make(chan error, 1)
	go func() { serveResult <- srv.Serve(listener) }()
	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	checkpointCalled := false
	err = gracefulShutdown(srv, hub.Close, func(context.Context) error {
		select {
		case serveErr := <-serveResult:
			if !errors.Is(serveErr, http.ErrServerClosed) {
				return errors.New("HTTP server did not stop cleanly before checkpoint")
			}
		default:
			return errors.New("checkpoint ran before HTTP server stopped")
		}
		checkpointCalled = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !checkpointCalled {
		t.Fatal("checkpoint was not called")
	}
	response.Body.Close()
}

func TestGracefulShutdownReturnsCheckpointError(t *testing.T) {
	srv := &http.Server{}
	wantErr := errors.New("checkpoint failed")
	err := gracefulShutdown(srv, func() {}, func(context.Context) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("gracefulShutdown error = %v, want wrapped %v", err, wantErr)
	}
}
