package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

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
	pool, err := openPool(context.Background(), dbURL)
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
	work, stopWork := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { rtH.Listen(work) })

	srv := &http.Server{Addr: addr, Handler: withTimeouts(web.SameOrigin(realtime.WithOrigin(mux))),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 64 << 10}
	serve(srv, func() { stopWork(); wg.Wait() })
}

// openPool connects with DB_MAX_CONNS connections (default 5; budget it with the user service's against
// max_connections), and the user service's DB_STATEMENT_TIMEOUT and DB_IDLE_TX_TIMEOUT defaults.
func openPool(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	maxConns, err := strconv.ParseInt(env("DB_MAX_CONNS", "5"), 10, 32)
	if err != nil || maxConns < 2 {
		return nil, fmt.Errorf("DB_MAX_CONNS must be a number of at least 2") // the listener holds one
	}
	cfg.MaxConns = int32(maxConns)
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = env("DB_STATEMENT_TIMEOUT", "30s")
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = env("DB_IDLE_TX_TIMEOUT", "2min")
	return pgxpool.NewWithConfig(ctx, cfg)
}

// serve runs srv until SIGINT or SIGTERM, then stops taking requests, lets in-flight ones finish (up to 20s) and calls
// stop to end the background work. Open sockets just drop; tabs reconnect.
func serve(srv *http.Server, stop func()) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("admin listening", "addr", srv.Addr)
	select {
	case err := <-errc:
		fatal("server", "err", err)
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	sctx, scancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer scancel()
	if err := srv.Shutdown(sctx); err != nil {
		slog.Warn("shutdown: requests still running", "err", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server", "err", err)
	}
	stop()
	slog.Info("stopped")
}

// withTimeouts gives each request 30s to read its body, respond and run (the context ends then). The realtime socket
// sets its own.
func withTimeouts(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/realtime" {
			h.ServeHTTP(w, r)
			return
		}
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(30 * time.Second))
		_ = rc.SetWriteDeadline(time.Now().Add(35 * time.Second)) // room to write the error when ctx ends
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		h.ServeHTTP(w, r.WithContext(ctx))
	})
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
