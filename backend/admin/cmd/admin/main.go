package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/admin/internal/auth"
	"github.com/devender07/tether/backend/admin/internal/config"
	"github.com/devender07/tether/backend/admin/internal/directory"
	"github.com/devender07/tether/backend/admin/internal/stats"
)

func main() {
	promote := flag.String("promote", "", "make the user with this email a platform admin, then exit")
	flag.Parse()

	dbURL := env("DATABASE_URL", "postgres://tether:tether@localhost:5433/tether?sslmode=disable")
	addr := ":" + env("PORT", "8080")

	// The user service owns the schema and runs migrations; this service only connects.
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		slog.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if *promote != "" {
		u, err := auth.Promote(context.Background(), pool, *promote)
		if err != nil {
			slog.Error("promote", "err", err)
			os.Exit(1)
		}
		if u == nil {
			fmt.Fprintf(os.Stderr, "no user with email %s\n", *promote)
			os.Exit(1)
		}
		fmt.Printf("%s <%s> is now a platform admin\n", u.Name, u.Email)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	authH := auth.New(pool)
	authH.Register(mux)
	config.New(pool, authH.Authenticate).Register(mux)
	directory.New(pool, authH.Authenticate).Register(mux)
	stats.New(pool, authH.Authenticate).Register(mux)

	slog.Info("admin listening", "addr", addr)
	// ponytail: no graceful shutdown or server timeouts, same as the user service; add both before production.
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
