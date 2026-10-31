package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/devender07/tether/backend/user/internal/auth"
	"github.com/devender07/tether/backend/user/internal/db"
	"github.com/devender07/tether/backend/user/internal/file"
	"github.com/devender07/tether/backend/user/internal/issue"
	"github.com/devender07/tether/backend/user/internal/mail"
	"github.com/devender07/tether/backend/user/internal/notify"
	"github.com/devender07/tether/backend/user/internal/project"
	"github.com/devender07/tether/backend/user/internal/realtime"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

func main() {
	prod := os.Getenv("APP_ENV") == "production"
	dbURL := env("DATABASE_URL", "postgres://tether:tether@localhost:5433/tether?sslmode=disable")
	addr := ":" + env("PORT", "8090")
	secret := []byte(os.Getenv("JWT_SECRET"))
	if prod {
		switch {
		case os.Getenv("DATABASE_URL") == "":
			fatal("APP_ENV=production requires DATABASE_URL")
		case len(secret) < 32:
			fatal("APP_ENV=production requires JWT_SECRET of at least 32 bytes (e.g. openssl rand -base64 32)")
		}
	}
	if len(secret) == 0 {
		secret = nil // dev: system_config's generated jwt_secret
	}

	pool, err := db.Open(context.Background(), dbURL)
	if err != nil {
		slog.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if secret == nil {
		if err := auth.EnsureJWTSecret(context.Background(), pool); err != nil {
			fatal("startup", "err", err)
		}
	}

	cfg := sysconfig.New(pool)
	base, err := cfg.Get(context.Background(), "app_base_url")
	if err != nil {
		fatal("startup", "err", err)
	}
	if prod && !strings.HasPrefix(base, "https://") {
		fatal("APP_ENV=production requires an https app_base_url (set it in the admin app or system_config)", "app_base_url", base)
	}
	logConfig(prod, addr, dbURL, base, secret != nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mailer := mail.New(cfg)
	authH := auth.New(pool, cfg, mailer, secret, func(ctx context.Context, userID, name string) error {
		return workspace.CreateDefault(ctx, pool, userID, name)
	})
	authH.Register(mux)
	wsH := workspace.New(pool, authH.Authenticate, mailer, cfg)
	wsH.Register(mux)
	project.New(pool, authH.Authenticate, wsH.Member).Register(mux)
	issueH := issue.New(pool, authH.Authenticate, wsH.Member)
	issueH.Register(mux)
	go issueH.Listen(context.Background())
	rtH := realtime.New(pool, authH.Session, authH.Alive,
		func(ctx context.Context, userID, slug string) (string, error) {
			ws, err := wsH.Member(ctx, userID, slug)
			if ws == nil {
				return "", err
			}
			return ws.ID, nil
		},
		func(ctx context.Context, c *realtime.Conn, userID, slug, identifier string) (realtime.Collab, string, error) {
			s, msg, err := issueH.Join(ctx, c, userID, slug, identifier)
			if s == nil {
				return nil, msg, err // not a nil *issue.Session inside the interface
			}
			return s, "", nil
		})
	rtH.Register(mux)
	go rtH.Listen(context.Background())
	go rtH.Presence(context.Background())
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
	if err := http.ListenAndServe(addr, authH.CheckOrigin(realtime.WithOrigin(mux))); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

// logConfig logs the effective settings, without secrets.
func logConfig(prod bool, addr, dbURL, base string, envSecret bool) {
	db := "invalid DATABASE_URL"
	if u, err := url.Parse(dbURL); err == nil {
		db = u.Redacted()
		if prod && u.Query().Get("sslmode") != "verify-full" {
			slog.Warn("DATABASE_URL should use sslmode=verify-full in production", "sslmode", u.Query().Get("sslmode"))
		}
	}
	jwtSource := "system_config (dev fallback)"
	if envSecret {
		jwtSource = "JWT_SECRET"
	}
	appEnv := "development"
	if prod {
		appEnv = "production"
	}
	slog.Info("config", "app_env", appEnv, "addr", addr, "database", db, "app_base_url", base, "jwt_secret", jwtSource,
		"upload_dir", env("UPLOAD_DIR", "./uploads"))
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
