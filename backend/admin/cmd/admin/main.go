package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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

	"github.com/devender07/taskwell/backend/admin/internal/audit"
	"github.com/devender07/taskwell/backend/admin/internal/auth"
	"github.com/devender07/taskwell/backend/admin/internal/config"
	"github.com/devender07/taskwell/backend/admin/internal/defaults"
	"github.com/devender07/taskwell/backend/admin/internal/devreset"
	"github.com/devender07/taskwell/backend/admin/internal/directory"
	"github.com/devender07/taskwell/backend/admin/internal/realtime"
	"github.com/devender07/taskwell/backend/admin/internal/stats"
	"github.com/devender07/taskwell/backend/admin/internal/web"
	"github.com/devender07/taskwell/backend/shared/cors"
	"github.com/devender07/taskwell/backend/shared/env"
	"github.com/devender07/taskwell/backend/shared/obs"
	"github.com/devender07/taskwell/backend/shared/totp"
)

func main() {
	enrol := flag.String("enrol-mfa", "", "give the admin with this email an MFA factor (printed here) when they have none, then exit")
	resetMFA := flag.String("reset-mfa", "", "give this admin a new MFA factor (printed here) and end their admin sign-ins, then exit")
	mfaCode := flag.String("mfa-code", "", "dev only: print the current sign-in code of the admin with this email, then exit")
	healthcheck := flag.Bool("healthcheck", false, "exit 0 when the running service at METRICS_ADDR is ready, else 1")
	flag.Parse()

	prod := os.Getenv("APP_ENV") == "production"
	obs.Setup(prod)
	metricsAddr := env.Get("METRICS_ADDR", "127.0.0.1:9091")
	if *healthcheck {
		os.Exit(obs.Healthcheck(metricsAddr))
	}
	dbURL := env.Get("DATABASE_URL", "postgres://taskwell:taskwell@localhost:5433/taskwell?sslmode=disable")
	addr := ":" + env.Get("PORT", "8080")
	secret := []byte(os.Getenv("ADMIN_JWT_SECRET"))
	if prod && os.Getenv("DATABASE_URL") == "" {
		obs.Fatal("APP_ENV=production requires DATABASE_URL")
	}
	if prod && (len(secret) < 32 || string(secret) == os.Getenv("JWT_SECRET")) {
		obs.Fatal("APP_ENV=production requires ADMIN_JWT_SECRET of at least 32 bytes, different from JWT_SECRET")
	}
	if len(secret) == 0 {
		secret = nil // dev: system_config's jwt_secret
	}
	mfaSecret := os.Getenv("MFA_ENCRYPTION_KEY")
	if prod && len(mfaSecret) < 32 {
		obs.Fatal("APP_ENV=production requires MFA_ENCRYPTION_KEY of at least 32 bytes")
	}
	// The admin app's URL when it's on another host than this API (its own subdomain of the same domain).
	appURL := os.Getenv("ADMIN_APP_URL")
	if appURL == "" && !prod {
		appURL = "http://localhost:5174"
	}
	appOrigin := cors.Origin(appURL)
	allowed, err := web.ParseAllowlist(os.Getenv("ADMIN_ALLOWED_IPS"))
	if err != nil {
		obs.Fatal("ADMIN_ALLOWED_IPS", "err", err)
	}
	if allowed != nil {
		slog.Info("admin allowlist", "ranges", len(allowed))
	} else if prod {
		slog.Warn("ADMIN_ALLOWED_IPS is empty: the admin API accepts requests from any address")
	}

	// The user service owns the schema and runs migrations; this service only connects.
	pool, err := openPool(context.Background(), dbURL)
	if err != nil {
		obs.Fatal("database", "err", err)
	}
	defer pool.Close()
	if err := checkSchema(context.Background(), pool); err != nil {
		pool.Close()
		obs.Fatal("schema", "err", err)
	}
	mfaKey, err := loadMFAKey(context.Background(), pool, mfaSecret)
	if err != nil {
		pool.Close()
		obs.Fatal("mfa key", "err", err)
	}
	if n, err := auth.EncryptSecrets(context.Background(), pool, mfaKey); err != nil {
		pool.Close()
		obs.Fatal("mfa secrets", "err", err)
	} else if n > 0 {
		slog.Info("encrypted plaintext mfa secrets", "count", n)
	}

	if *enrol != "" {
		u, e, err := auth.EnrolMFA(context.Background(), pool, mfaKey, *enrol)
		if err != nil {
			pool.Close()
			obs.Fatal("enrol-mfa", "err", err)
		}
		if u == nil {
			fmt.Fprintf(os.Stderr, "no admin with the email %s\n", *enrol)
			pool.Close()
			os.Exit(1)
		}
		fmt.Printf("%s <%s> is an admin\n", u.Name, u.Email)
		if e == nil {
			fmt.Println("They already have MFA set up; run -reset-mfa to give them a new factor.")
			return
		}
		printEnrolment(e)
		return
	}

	if *mfaCode != "" {
		// Never in production: there the code must come from the admin's own device.
		if prod {
			pool.Close()
			obs.Fatal("-mfa-code is for local dev only")
		}
		now := time.Now()
		code, err := auth.CurrentCode(context.Background(), pool, mfaKey, *mfaCode, now)
		if err != nil || code == "" {
			pool.Close()
			obs.Fatal("mfa-code: no admin with that email, or they have no factor", "email", *mfaCode, "err", err)
		}
		fmt.Printf("%s  (changes in %ds)\n", code, 30-now.Unix()%30)
		return
	}

	if *resetMFA != "" {
		u, e, err := auth.ResetMFA(context.Background(), pool, mfaKey, *resetMFA)
		if err != nil || u == nil {
			pool.Close()
			obs.Fatal("reset-mfa: no admin with that email, or the reset failed", "email", *resetMFA, "err", err)
		}
		fmt.Printf("New MFA factor for %s <%s>; their admin sign-ins have ended.\n", u.Name, u.Email)
		printEnrolment(e)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	logConfig(prod, addr, dbURL, secret != nil)
	authH := auth.New(pool, secret, mfaKey)
	authH.Register(mux)
	// Every handler but auth's own signs requests in through this, so the access log knows the admin.
	authenticate := func(r *http.Request) (string, error) {
		id, err := authH.Authenticate(r)
		obs.SetUser(r.Context(), id)
		return id, err
	}
	config.New(pool, authenticate).Register(mux)
	defaults.New(pool, authenticate).Register(mux)
	if !prod {
		devreset.New(pool, authenticate).Register(mux)
	}
	directory.New(pool, authenticate).Register(mux)
	stats.New(pool, authenticate).Register(mux)
	audit.New(pool, authenticate).Register(mux)
	rtH := realtime.New(pool, authenticate)
	if u, err := url.Parse(appOrigin); err == nil {
		rtH.AppHost = u.Host
	}
	rtH.Register(mux)
	work, stopWork := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { rtH.Listen(work) })

	internal := http.NewServeMux()
	internal.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			http.Error(w, "database: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "ok\n") //nolint:errcheck // the client's gone
	})
	internal.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		obs.WriteHTTP(w)
		obs.WritePool(w, pool.Stat())
		obs.Metric(w, "taskwell_realtime_sockets", "gauge", "Open admin realtime WebSockets.", "", float64(rtH.Sockets()))
	})
	metrics := &http.Server{Addr: metricsAddr, Handler: internal, ReadHeaderTimeout: 5 * time.Second}

	srv := &http.Server{Addr: addr, Handler: obs.Middleware(web.AllowIPs(allowed, cors.Allow(func(*http.Request) string { return appOrigin },
		withTimeouts(web.SameOrigin(appOrigin, realtime.WithOrigin(mux)))))),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 64 << 10}
	serve(srv, metrics, func() { stopWork(); wg.Wait() })
}

// schemaVersion is the newest migration this service's queries need. The user service applies migrations; bump
// this when admin starts depending on a newer one.
const schemaVersion = "001_schema.sql"

// checkSchema fails unless the user service has applied schemaVersion.
func checkSchema(ctx context.Context, pool *pgxpool.Pool) error {
	var ok bool
	err := pool.QueryRow(ctx, `select to_regclass('schema_migrations') is not null
		and exists(select 1 from schema_migrations where version = $1)`, schemaVersion).Scan(&ok)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	if !ok {
		return fmt.Errorf("migration %s isn't applied: run the user service's -migrate first", schemaVersion)
	}
	return nil
}

// loadMFAKey returns the key TOTP secrets are sealed with: from MFA_ENCRYPTION_KEY, or in dev (when it's unset)
// derived from system_config's jwt_secret.
func loadMFAKey(ctx context.Context, pool *pgxpool.Pool, envKey string) ([]byte, error) {
	if envKey != "" {
		return totp.Key(envKey), nil
	}
	var s string
	if err := pool.QueryRow(ctx, `select value from system_config where key = 'jwt_secret'`).Scan(&s); err != nil || s == "" {
		return nil, fmt.Errorf("MFA_ENCRYPTION_KEY is unset and jwt_secret can't be read; start the user service once: %v", err)
	}
	slog.Warn("MFA_ENCRYPTION_KEY is unset: TOTP secrets are encrypted with a key derived from jwt_secret (dev only)")
	return totp.Key("jwt_secret:" + s), nil
}

// printEnrolment shows a new factor to the operator, who passes it to the admin over a safe channel.
func printEnrolment(e *auth.Enrolment) {
	fmt.Printf("\nAdd it to an authenticator app (time-based, 6 digits, 30 seconds):\n  setup key:   %s\n  otpauth URL: %s\n", e.Secret, e.URL)
	fmt.Println("\nRecovery codes (each signs in once; shown only now):")
	for _, c := range e.RecoveryCodes {
		fmt.Println("  " + c)
	}
}

// openPool connects with DB_MAX_CONNS connections (default 5; budget it with the user service's against
// max_connections), and the user service's DB_STATEMENT_TIMEOUT and DB_IDLE_TX_TIMEOUT defaults.
func openPool(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	maxConns, err := strconv.ParseInt(env.Get("DB_MAX_CONNS", "5"), 10, 32)
	if err != nil || maxConns < 2 {
		return nil, fmt.Errorf("DB_MAX_CONNS must be a number of at least 2") // the listener holds one
	}
	cfg.MaxConns = int32(maxConns)
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = env.Get("DB_STATEMENT_TIMEOUT", "30s")
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = env.Get("DB_IDLE_TX_TIMEOUT", "2min")
	return pgxpool.NewWithConfig(ctx, cfg)
}

// serve runs srv until SIGINT or SIGTERM, then stops taking requests, lets in-flight ones finish (up to 20s) and calls
// stop to end the background work. Open sockets just drop; tabs reconnect.
func serve(srv, metrics *http.Server, stop func()) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	merrc := make(chan error, 1)
	go func() { merrc <- metrics.ListenAndServe() }()
	slog.Info("admin listening", "addr", srv.Addr, "metrics_addr", metrics.Addr)
	select {
	case err := <-errc:
		obs.Fatal("server", "err", err)
	case err := <-merrc:
		obs.Fatal("metrics server", "err", err)
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
	_ = metrics.Close()
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
