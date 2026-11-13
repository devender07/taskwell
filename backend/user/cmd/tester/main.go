// Command tester runs AI test runs: it claims queued runs from the database, has Claude write test cases and drive
// a sandboxed Chrome (over CDP) through its own egress proxy, and writes results, step logs and screenshots back.
// The user API never sees ANTHROPIC_API_KEY.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/devender07/taskwell/backend/shared/env"
	"github.com/devender07/taskwell/backend/shared/obs"
	"github.com/devender07/taskwell/backend/user/internal/aitest"
	"github.com/devender07/taskwell/backend/user/internal/db"
	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/storage"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
)

func main() {
	prod := os.Getenv("APP_ENV") == "production"
	obs.Setup(prod)
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	switch {
	case prod && os.Getenv("DATABASE_URL") == "":
		obs.Fatal("APP_ENV=production requires DATABASE_URL")
	case prod && apiKey == "":
		obs.Fatal("APP_ENV=production requires ANTHROPIC_API_KEY")
	}
	key, err := aitest.Key(os.Getenv("TEST_SECRET_KEY"), prod)
	if err != nil {
		obs.Fatal(err.Error())
	}
	browsers, err := strconv.Atoi(env.Get("AI_TEST_BROWSERS", "2"))
	if err != nil || browsers < 1 {
		obs.Fatal("AI_TEST_BROWSERS must be a number of at least 1")
	}
	// Dev only: local test sites (127.0.0.1, the LAN) may be visited, and their self-signed or local-CA certificates
	// are accepted, since the Chrome container doesn't trust the host's development CA.
	allowPrivate := !prod && os.Getenv("AI_TEST_ALLOW_PRIVATE") == "true"

	stop, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, err := db.Open(context.Background(), env.Get("DATABASE_URL", "postgres://taskwell:taskwell@localhost:5433/taskwell?sslmode=disable"))
	if err != nil {
		obs.Fatal("database", "err", err)
	}
	defer pool.Close()

	proxy := aitest.NewProxy(allowPrivate)
	proxySrv := &http.Server{Addr: env.Get("PROXY_ADDR", ":3128"), Handler: proxy, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := proxySrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			obs.Fatal("proxy", "err", err)
		}
	}()
	live := aitest.NewLive()
	liveMux := http.NewServeMux()
	liveMux.Handle("GET /live/{run}", live)
	liveSrv := &http.Server{Addr: env.Get("LIVE_ADDR", ":3129"), Handler: liveMux, ReadHeaderTimeout: 10 * time.Second}
	host, _ := os.Hostname() // in compose, the container's hostname is its id, which the network resolves
	_, port, _ := net.SplitHostPort(liveSrv.Addr)
	o := aitest.RunnerOptions{Live: live, LiveURL: env.Get("LIVE_URL", "http://"+net.JoinHostPort(host, port)), Key: key, ChromeURL: env.Get("CHROME_URL", "http://chrome:9222"),
		ProxyURL: env.Get("PROXY_URL", "http://tester:3128"), Browsers: browsers, Proxy: proxy, IgnoreCertErrors: allowPrivate}
	if apiKey != "" {
		o.Send = aitest.NewSender(apiKey)
	}
	slog.Info("tester running", "app_env", map[bool]string{true: "production", false: "development"}[prod], "proxy_addr", proxySrv.Addr,
		"proxy_url", o.ProxyURL, "chrome_url", o.ChromeURL, "browsers", browsers, "live_url", o.LiveURL,
		"anthropic_api_key", apiKey != "", "allow_private_sites", allowPrivate)
	go plan.Watch(context.Background(), pool, 10*time.Second) // the AI budget of each plan
	cfg := sysconfig.New(pool)
	o.Files = storage.New(cfg)
	runner := aitest.NewRunner(pool, cfg, o)
	runner.RegisterSessions(liveMux) // browser sessions share the internal port with the live view
	go func() {
		if err := liveSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			obs.Fatal("live", "err", err)
		}
	}()
	runner.Run(stop) // returns once browser sessions are closed and running runs end their current step
	ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	_ = proxySrv.Shutdown(ctx)
	_ = liveSrv.Shutdown(ctx)
	slog.Info("stopped")
}
