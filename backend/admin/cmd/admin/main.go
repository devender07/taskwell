package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/admin/internal/auth"
	"github.com/devender07/tether/backend/admin/internal/config"
	"github.com/devender07/tether/backend/admin/internal/directory"
	"github.com/devender07/tether/backend/admin/internal/realtime"
	"github.com/devender07/tether/backend/admin/internal/stats"
	"github.com/devender07/tether/backend/admin/internal/web"
)

func main() {
	promote := flag.String("promote", "", "make the user with this email a platform admin, then exit")
	flag.Parse()

	prod := os.Getenv("APP_ENV") == "production"
	dbURL := env("DATABASE_URL", "postgres://tether:tether@localhost:5433/tether?sslmode=disable")
	addr := ":" + env("PORT", "8080")
	secret := []byte(os.Getenv("ADMIN_JWT_SECRET"))
	if prod && os.Getenv("DATABASE_URL") == "" {
		fatal("APP_ENV=production requires DATABASE_URL")
	}
	if prod && (len(secret) < 32 || string(secret) == os.Getenv("JWT_SECRET")) {
		fatal("APP_ENV=production requires ADMIN_JWT_SECRET of at least 32 bytes, different from JWT_SECRET")
	}
	if len(secret) == 0 {
		secret = nil // dev: system_config's jwt_secret
	}

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
			fmt.Fprintf(os.Stderr, "no user with the verified email %s\n", *promote)
			os.Exit(1)
		}
		fmt.Printf("%s <%s> is now a platform admin\n", u.Name, u.Email)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	logConfig(prod, addr, dbURL, secret != nil)
	authH := auth.New(pool, secret)
	authH.Register(mux)
	config.New(pool, authH.Authenticate).Register(mux)
	directory.New(pool, authH.Authenticate).Register(mux)
	stats.New(pool, authH.Authenticate).Register(mux)
	rtH := realtime.New(pool, authH.Authenticate)
	rtH.Register(mux)
	go rtH.Listen(context.Background())

	slog.Info("admin listening", "addr", addr)
	// ponytail: no graceful shutdown or server timeouts, same as the user service; add both before production.
	if err := http.ListenAndServe(addr, web.SameOrigin(realtime.WithOrigin(mux))); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

// logConfig logs the effective settings, without secrets.
func logConfig(prod bool, addr, dbURL string, envSecret bool) {
	db := "invalid DATABASE_URL"
	if u, err := url.Parse(dbURL); err == nil {
		db = u.Redacted()
	}
	jwtSource := "system_config jwt_secret (dev fallback)"
	if envSecret {
		jwtSource = "ADMIN_JWT_SECRET"
	}
	appEnv := "development"
	if prod {
		appEnv = "production"
	}
	slog.Info("config", "app_env", appEnv, "addr", addr, "database", db, "jwt_secret", jwtSource)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
