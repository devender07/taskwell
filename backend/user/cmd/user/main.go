package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/devender07/tether/backend/user/internal/auth"
	"github.com/devender07/tether/backend/user/internal/db"
	"github.com/devender07/tether/backend/user/internal/file"
	"github.com/devender07/tether/backend/user/internal/issue"
	"github.com/devender07/tether/backend/user/internal/mail"
	"github.com/devender07/tether/backend/user/internal/notify"
	"github.com/devender07/tether/backend/user/internal/project"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
	"github.com/devender07/tether/backend/user/internal/workspace"
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
	mailer := mail.New(cfg)
	authH := auth.New(pool, cfg, mailer, func(ctx context.Context, userID, name string) error {
		return workspace.CreateDefault(ctx, pool, userID, name)
	})
	authH.Register(mux)
	wsH := workspace.New(pool, authH.Authenticate, mailer, cfg)
	wsH.Register(mux)
	project.New(pool, authH.Authenticate, wsH.Member).Register(mux)
	issueH := issue.New(pool, authH.Authenticate, wsH.Member)
	issueH.Register(mux)
	go issueH.Listen(context.Background())
	notifyH := notify.New(pool, authH.Authenticate, wsH.Member, mailer, cfg)
	notifyH.Register(mux)
	go notifyH.Run(context.Background(), duration("NOTIFY_INTERVAL", time.Minute), duration("NOTIFY_SETTLE", 30*time.Second))
	fileH, err := file.New(pool, env("UPLOAD_DIR", "./uploads"), authH.Authenticate, wsH.Member, cfg,
		func(ctx context.Context, userID string) (any, error) { return authH.Profile(ctx, userID) })
	if err != nil {
		slog.Error("startup", "err", err)
		os.Exit(1)
	}
	fileH.Register(mux)

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

// duration reads key as a time.Duration ("90s", "1m"), or def when it's unset or invalid.
func duration(key string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(key)); err == nil && d > 0 {
		return d
	}
	return def
}
