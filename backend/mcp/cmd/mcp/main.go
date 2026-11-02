// Command mcp serves Tether's MCP endpoint at /mcp; its tools call the user service's API as the caller.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/devender07/tether/backend/mcp/internal/server"
)

func main() {
	var h slog.Handler = slog.NewTextHandler(os.Stdout, nil)
	if os.Getenv("APP_ENV") == "production" {
		h = slog.NewJSONHandler(os.Stdout, nil)
	}
	slog.SetDefault(slog.New(h))
	addr := ":" + env("PORT", "8095")
	apiURL := env("TETHER_API_URL", "http://localhost:8090")
	appURL := env("APP_URL", "http://localhost:5173")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/mcp", server.Handler(apiURL, appURL, &http.Client{Timeout: 30 * time.Second}))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("mcp listening", "addr", addr, "tether_api_url", apiURL, "app_url", appURL)
	select {
	case err := <-errc:
		slog.Error("server", "err", err)
		os.Exit(1)
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Warn("shutdown: requests still running", "err", err)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
