package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/aitest"
	"github.com/devender07/tether/backend/user/internal/auth"
	"github.com/devender07/tether/backend/user/internal/chat"
	"github.com/devender07/tether/backend/user/internal/cycle"
	"github.com/devender07/tether/backend/user/internal/db"
	"github.com/devender07/tether/backend/user/internal/file"
	"github.com/devender07/tether/backend/user/internal/gitlink"
	"github.com/devender07/tether/backend/user/internal/issue"
	"github.com/devender07/tether/backend/user/internal/mail"
	"github.com/devender07/tether/backend/user/internal/notify"
	"github.com/devender07/tether/backend/user/internal/obs"
	"github.com/devender07/tether/backend/user/internal/project"
	"github.com/devender07/tether/backend/user/internal/realtime"
	"github.com/devender07/tether/backend/user/internal/sweep"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

func main() {
	migrateOnly := flag.Bool("migrate", false, "apply the database migrations, then exit")
	healthcheck := flag.Bool("healthcheck", false, "exit 0 when the running service at METRICS_ADDR is ready, else 1")
	flag.Parse()
	prod := os.Getenv("APP_ENV") == "production"
	obs.Setup(prod)
	metricsAddr := env("METRICS_ADDR", "127.0.0.1:9090")
	if *healthcheck {
		os.Exit(obs.Healthcheck(metricsAddr))
	}
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
		fatal("database", "err", err)
	}
	defer pool.Close()
	// Production runs -migrate as a release step, then starts with MIGRATE_ON_START=false.
	if *migrateOnly || os.Getenv("MIGRATE_ON_START") != "false" {
		if err := db.Migrate(context.Background(), pool); err != nil {
			pool.Close()
			fatal("migrate", "err", err)
		}
		if *migrateOnly {
			slog.Info("migrations applied")
			return
		}
	}

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
	maxAttempts, err := strconv.Atoi(env("MAIL_MAX_ATTEMPTS", "8"))
	if err != nil || maxAttempts < 1 {
		fatal("MAIL_MAX_ATTEMPTS must be a number of at least 1")
	}
	mailer := mail.New(pool, cfg, duration("MAIL_TIMEOUT", 20*time.Second), duration("MAIL_RETRY_BASE", 30*time.Second), maxAttempts)
	authH := auth.New(pool, cfg, mailer, secret, workspace.CreateDefault)
	authH.Register(mux)
	// Every handler but auth's own signs requests in through this, so the access log knows the user.
	authenticate := func(r *http.Request) (string, error) {
		id, err := authH.Authenticate(r)
		obs.SetUser(r.Context(), id)
		return id, err
	}
	wsH := workspace.New(pool, authenticate, mailer, cfg)
	wsH.Register(mux)
	project.New(pool, authenticate, wsH.Member).Register(mux)
	issueH := issue.New(pool, authenticate, wsH.Member)
	issueH.Register(mux)
	cycle.New(pool, authenticate, wsH.Member, issueH.CycleIssues).Register(mux)
	jwtSecret := string(secret)
	if jwtSecret == "" {
		if jwtSecret, err = cfg.Get(context.Background(), "jwt_secret"); err != nil {
			fatal("startup", "err", err)
		}
	}
	gitKey, err := gitlink.Key(os.Getenv("INTEGRATION_SECRET_KEY"), jwtSecret, prod)
	if err != nil {
		fatal(err.Error())
	}
	gitlink.New(pool, gitKey, func(ctx context.Context) (string, error) { return cfg.Get(ctx, "app_base_url") },
		authenticate, wsH.Member).Register(mux)
	testKey, err := aitest.Key(os.Getenv("TEST_SECRET_KEY"), prod)
	if err != nil {
		fatal(err.Error())
	}
	aitest.New(pool, cfg, aitest.Options{Key: testKey, AllowHTTP: !prod, AllowPrivate: !prod && os.Getenv("AI_TEST_ALLOW_PRIVATE") == "true"},
		authenticate, wsH.Member).Register(mux)
	// Background work stops after the server has drained (see shutdown), so it's on its own context.
	work, stopWork := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { issueH.Listen(work) })
	rtH := realtime.New(pool, func(r *http.Request) (realtime.Session, error) {
		s, err := authH.Session(r)
		obs.SetUser(r.Context(), s.UserID)
		return s, err
	}, authH.Alive,
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
	rtH.OnRevoke(authH.Forget)
	if n, err := strconv.Atoi(os.Getenv("WS_MAX_CONNS_PER_USER")); err == nil && n > 0 {
		realtime.MaxConnsPerUser = n
	}
	rtH.ConfigChanged = cfg.Invalidate
	chatH := chat.New(pool, authenticate, wsH.Member)
	chatH.Register(mux)
	rtH.ChatAudience = chatH.Audience
	rtH.Register(mux)
	wg.Go(func() { rtH.Listen(work) })
	wg.Go(func() { rtH.Presence(work) })
	notifyH := notify.New(pool, authenticate, wsH.Member, mailer, cfg)
	notifyH.Register(mux)
	wg.Go(func() {
		notifyH.Run(work, duration("NOTIFY_INTERVAL", time.Minute), duration("NOTIFY_SETTLE", 30*time.Second))
	})
	wg.Go(func() { mailer.Run(work) })
	uploadDir := env("UPLOAD_DIR", "./uploads")
	fileH, err := file.New(pool, uploadDir, authenticate, wsH.Member, cfg,
		func(ctx context.Context, userID string) (any, error) { return authH.Profile(ctx, userID) })
	if err != nil {
		fatal("startup", "err", err)
	}
	fileH.Register(mux)
	sweeper := sweep.New(pool, uploadDir)
	wg.Go(func() { sweeper.Run(work, duration("SWEEP_INTERVAL", 24*time.Hour)) })

	internal := http.NewServeMux()
	internal.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		switch err := pool.Ping(ctx); {
		case err != nil:
			http.Error(w, "database: "+err.Error(), http.StatusServiceUnavailable)
		case !rtH.Listening():
			http.Error(w, "realtime listener down", http.StatusServiceUnavailable)
		case !issueH.Listening():
			http.Error(w, "collab listener down", http.StatusServiceUnavailable)
		default:
			io.WriteString(w, "ok\n") //nolint:errcheck // the client's gone
		}
	})
	internal.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		obs.WriteHTTP(w)
		obs.WritePool(w, pool.Stat())
		obs.Metric(w, "tether_realtime_sockets", "gauge", "Open realtime WebSockets.", "", float64(rtH.Sockets()))
		writeDBMetrics(r.Context(), w, pool)
	})
	metrics := &http.Server{Addr: metricsAddr, Handler: internal, ReadHeaderTimeout: 5 * time.Second}

	srv := &http.Server{Addr: addr, Handler: obs.Middleware(withTimeouts(authH.CheckOrigin(realtime.WithOrigin(mux)))),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 64 << 10}
	serve(srv, metrics, func() { stopWork(); wg.Wait() })
}

// writeDBMetrics writes the email queue's depth and oldest age and Postgres's NOTIFY queue usage; a failed query
// leaves its samples out.
func writeDBMetrics(ctx context.Context, w io.Writer, pool *pgxpool.Pool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var queued, failed int64
	var oldest, notifyUsage float64
	err := pool.QueryRow(ctx, `
		select count(*) filter (where failed_at is null), count(*) filter (where failed_at is not null),
		       coalesce(extract(epoch from now() - min(created_at) filter (where failed_at is null)), 0),
		       pg_notification_queue_usage()
		from outbound_mail`).Scan(&queued, &failed, &oldest, &notifyUsage)
	if err != nil {
		slog.WarnContext(ctx, "metrics query failed", "err", err)
		return
	}
	obs.Metric(w, "tether_mail_queued", "gauge", "Emails waiting to be sent.", "", float64(queued))
	obs.Metric(w, "tether_mail_failed", "gauge", "Emails given up on, kept 30 days.", "", float64(failed))
	obs.Metric(w, "tether_mail_oldest_seconds", "gauge", "Age of the oldest email waiting to be sent.", "", oldest)
	obs.Metric(w, "tether_pg_notification_queue_usage", "gauge", "Fraction of Postgres's NOTIFY queue in use (0-1).", "", notifyUsage)
}

// serve runs srv until SIGINT or SIGTERM, then stops taking requests, lets in-flight ones finish (up to 20s) and calls
// stop to end the background work. Keep the orchestrator's grace period above 40s (compose stop_grace_period).
func serve(srv, metrics *http.Server, stop func()) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	merrc := make(chan error, 1)
	go func() { merrc <- metrics.ListenAndServe() }()
	slog.Info("user listening", "addr", srv.Addr, "metrics_addr", metrics.Addr)
	select {
	case err := <-errc:
		fatal("server", "err", err)
	case err := <-merrc:
		fatal("metrics server", "err", err)
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
	_ = metrics.Close() // readyz answers until here, so the orchestrator sees a draining instance as up
	slog.Info("stopped")
}

// withTimeouts bounds each request: 30s to read the body, respond and run (the context ends then), 10 minutes for
// file uploads and downloads. The realtime socket sets its own.
func withTimeouts(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/realtime" {
			h.ServeHTTP(w, r)
			return
		}
		d := 30 * time.Second
		if strings.HasPrefix(r.URL.Path, "/api/files/") || strings.HasSuffix(r.URL.Path, "/files") || strings.HasSuffix(r.URL.Path, "/avatar") {
			d = 10 * time.Minute
		}
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(d))
		_ = rc.SetWriteDeadline(time.Now().Add(d + 5*time.Second)) // room to write the error when ctx ends
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		h.ServeHTTP(w, r.WithContext(ctx))
	})
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
