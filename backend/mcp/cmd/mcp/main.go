// Command mcp serves Taskwell's MCP endpoint at /mcp; its tools call the user service's API as the caller.
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

	"github.com/devender07/taskwell/backend/mcp/internal/server"
	"github.com/devender07/taskwell/backend/shared/env"
)

func main() {
	var h slog.Handler = slog.NewTextHandler(os.Stdout, nil)
	if os.Getenv("APP_ENV") == "production" {
		h = slog.NewJSONHandler(os.Stdout, nil)
	}
	slog.SetDefault(slog.New(h))
	addr := ":" + env.Get("PORT", "8095")
	apiURL := env.Get("TASKWELL_API_URL", "http://localhost:8090")
	appURL := env.Get("APP_URL", "http://localhost:5173")
	// Where clients reach /mcp, and the user API's public URL (the authorization server). Unset in production, both
	// are on the app's host.
	mcpURL, authURL := "http://localhost"+addr, "http://localhost:8090"
	if os.Getenv("APP_ENV") == "production" {
		mcpURL, authURL = appURL, appURL
	}
	mcpURL, authURL = env.Get("MCP_URL", mcpURL), env.Get("AUTH_URL", authURL)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/mcp", server.Handler(apiURL, appURL, mcpURL, &http.Client{Timeout: 30 * time.Second}))
	mux.Handle("GET /.well-known/oauth-protected-resource/mcp", server.Metadata(mcpURL, authURL))
	mux.Handle("GET /.well-known/oauth-protected-resource", server.Metadata(mcpURL, authURL))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("mcp listening", "addr", addr, "taskwell_api_url", apiURL, "app_url", appURL, "mcp_url", mcpURL, "auth_url", authURL)
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
