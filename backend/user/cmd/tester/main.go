// Command tester runs AI test runs: it claims queued runs from the database, has Claude write test cases and drive
// a sandboxed Chrome (over CDP) through its own egress proxy, and writes results, step logs and screenshots back.
// The user API never sees ANTHROPIC_API_KEY.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/devender07/tether/backend/user/internal/aitest"
	"github.com/devender07/tether/backend/user/internal/db"
	"github.com/devender07/tether/backend/user/internal/obs"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
)

func main() {
	prod := os.Getenv("APP_ENV") == "production"
	obs.Setup(prod)
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	switch {
	case prod && os.Getenv("DATABASE_URL") == "":
		fatal("APP_ENV=production requires DATABASE_URL")
	case prod && apiKey == "":
		fatal("APP_ENV=production requires ANTHROPIC_API_KEY")
	}
	key, err := aitest.Key(os.Getenv("TEST_SECRET_KEY"), prod)
	if err != nil {
		fatal(err.Error())
	}
	browsers, err := strconv.Atoi(env("AI_TEST_BROWSERS", "2"))
	if err != nil || browsers < 1 {
		fatal("AI_TEST_BROWSERS must be a number of at least 1")
	}
	allowPrivate := !prod && os.Getenv("AI_TEST_ALLOW_PRIVATE") == "true"

	stop, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	pool, err := db.Open(context.Background(), env("DATABASE_URL", "postgres://tether:tether@localhost:5433/tether?sslmode=disable"))
	if err != nil {
		fatal("database", "err", err)
	}
	defer pool.Close()

	proxy := aitest.NewProxy(allowPrivate)
	proxySrv := &http.Server{Addr: env("PROXY_ADDR", ":3128"), Handler: proxy, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := proxySrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			fatal("proxy", "err", err)
		}
	}()
	o := aitest.RunnerOptions{Key: key, UploadDir: env("UPLOAD_DIR", "./uploads"), ChromeURL: env("CHROME_URL", "http://chrome:9222"),
		ProxyURL: env("PROXY_URL", "http://tester:3128"), Browsers: browsers, Proxy: proxy}
	if apiKey != "" {
		o.Send = aitest.NewSender(apiKey)
	}
	slog.Info("tester running", "app_env", map[bool]string{true: "production", false: "development"}[prod], "proxy_addr", proxySrv.Addr,
		"proxy_url", o.ProxyURL, "chrome_url", o.ChromeURL, "browsers", browsers, "upload_dir", o.UploadDir,
		"anthropic_api_key", apiKey != "", "allow_private_sites", allowPrivate)
	aitest.NewRunner(pool, sysconfig.New(pool), o).Run(stop) // returns once running runs end their current step
	ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	_ = proxySrv.Shutdown(ctx)
	slog.Info("stopped")
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
