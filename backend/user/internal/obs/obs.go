// Package obs is the service's observability: slog setup, the request-id and access-log middleware, and metrics in
// the Prometheus text format. The admin service has a copy.
package obs

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Setup makes slog write to stdout: JSON in production, text otherwise, or as LOG_FORMAT (json or text) says.
// Every record logged with a request's context carries its request_id.
func Setup(prod bool) {
	format := os.Getenv("LOG_FORMAT")
	if format == "" {
		format = "text"
		if prod {
			format = "json"
		}
	}
	var h slog.Handler = slog.NewTextHandler(os.Stdout, nil)
	if format == "json" {
		h = slog.NewJSONHandler(os.Stdout, nil)
	}
	slog.SetDefault(slog.New(requestIDHandler{h}))
}

type requestIDHandler struct{ slog.Handler }

func (h requestIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if info, ok := ctx.Value(infoKey{}).(*info); ok {
		r.AddAttrs(slog.String("request_id", info.id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h requestIDHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return requestIDHandler{h.Handler.WithAttrs(as)}
}

func (h requestIDHandler) WithGroup(name string) slog.Handler {
	return requestIDHandler{h.Handler.WithGroup(name)}
}

type infoKey struct{}

// info is what the middleware knows about a request, filled in as it runs.
type info struct {
	id     string
	userID atomic.Pointer[string]
}

// SetUser records the request's signed-in user for its access log line.
func SetUser(ctx context.Context, userID string) {
	if info, ok := ctx.Value(infoKey{}).(*info); ok && userID != "" {
		info.userID.Store(&userID)
	}
}

var (
	requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)
	requests    [6]atomic.Int64 // by status class: [2] is 2xx
)

// Middleware gives each request an id (the client's X-Request-Id when it's a short token, else a new one), returns
// it in X-Request-Id, puts it in the context for logs, counts the response by status class and logs one access line
// per request, /healthz excepted.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get("X-Request-Id")
		if !requestIDRe.MatchString(id) {
			id = rand.Text()
		}
		w.Header().Set("X-Request-Id", id)
		in := &info{id: id}
		ctx := context.WithValue(r.Context(), infoKey{}, in)
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r.WithContext(ctx))
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		requests[min(rec.status/100, 5)].Add(1)
		if r.URL.Path == "/healthz" {
			return
		}
		user := ""
		if u := in.userID.Load(); u != nil {
			user = *u
		}
		slog.InfoContext(ctx, "request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(), "bytes", rec.bytes, "user_id", user)
	})
}

// recorder notes the response's status and size. Unwrap lets http.ResponseController and the WebSocket upgrade
// reach the connection.
type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Hijack is the upgrade's way to the connection when it doesn't unwrap.
func (r *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(r.ResponseWriter).Hijack()
}

// Metric writes one sample in the Prometheus text format, with its HELP and TYPE lines when help isn't "".
// labels is a ready label set such as `class="2xx"`, or "".
func Metric(w io.Writer, name, typ, help, labels string, v float64) {
	if help != "" {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
	}
	if labels != "" {
		labels = "{" + labels + "}"
	}
	fmt.Fprintf(w, "%s%s %g\n", name, labels, v)
}

// WriteHTTP writes the request counts by status class.
func WriteHTTP(w io.Writer) {
	for class := 1; class <= 5; class++ {
		help := ""
		if class == 1 {
			help = "HTTP requests served, by status class."
		}
		Metric(w, "tether_http_requests_total", "counter", help, fmt.Sprintf(`class="%dxx"`, class), float64(requests[class].Load()))
	}
}

// WritePool writes the connection pool's stats.
func WritePool(w io.Writer, s *pgxpool.Stat) {
	Metric(w, "tether_db_pool_max_conns", "gauge", "Largest pool size.", "", float64(s.MaxConns()))
	Metric(w, "tether_db_pool_total_conns", "gauge", "Open pool connections.", "", float64(s.TotalConns()))
	Metric(w, "tether_db_pool_acquired_conns", "gauge", "Pool connections in use.", "", float64(s.AcquiredConns()))
	Metric(w, "tether_db_pool_idle_conns", "gauge", "Idle pool connections.", "", float64(s.IdleConns()))
	Metric(w, "tether_db_pool_acquires_total", "counter", "Connections acquired from the pool.", "", float64(s.AcquireCount()))
	Metric(w, "tether_db_pool_empty_acquires_total", "counter", "Acquires that had to wait or open a connection.", "", float64(s.EmptyAcquireCount()))
	Metric(w, "tether_db_pool_canceled_acquires_total", "counter", "Acquires canceled before getting a connection.", "", float64(s.CanceledAcquireCount()))
	Metric(w, "tether_db_pool_acquire_seconds_total", "counter", "Time spent acquiring connections.", "", s.AcquireDuration().Seconds())
}

// Healthcheck GETs http://<addr>/readyz, addr being a listen address such as ":9090" (loopback when the host is
// empty or unspecified), and returns the process exit code: 0 when ready, else 1. For Docker HEALTHCHECK on images
// without curl.
func Healthcheck(addr string) int {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	c := http.Client{Timeout: 3 * time.Second}
	res, err := c.Get("http://" + net.JoinHostPort(host, port) + "/readyz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck:", res.Status)
		return 1
	}
	return 0
}
