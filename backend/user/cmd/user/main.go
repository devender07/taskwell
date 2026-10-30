package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/devender07/tether/backend/user/internal/auth"
	"github.com/devender07/tether/backend/user/internal/db"
	"github.com/devender07/tether/backend/user/internal/mail"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
)

func main() {
	dbURL := env("DATABASE_URL", "postgres://tether:tether@localhost:5433/tether?sslmode=disable")
	addr := ":" + env("PORT", "8090")

	pool, err := db.Open(context.Background(), dbURL)
	if err != nil {
		slog.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := auth.EnsureJWTSecret(context.Background(), pool); err != nil {
		slog.Error("startup", "err", err)
		os.Exit(1)
	}

	cfg := sysconfig.New(pool)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	auth.New(pool, cfg, mail.New(cfg)).Register(mux)

	slog.Info("user listening", "addr", addr)
	// ponytail: no graceful shutdown or server timeouts; add http.Server with timeouts + signal handling before production.
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
