package aitest

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/devender07/taskwell/backend/user/internal/netguard"
)

// Proxy is the egress proxy Chrome sends every request through (--proxy-server). A request is let out only for a
// run it knows (Proxy-Authorization is the run id and its random token), to a host that is, or is under, one of the
// workspace's test sites, and to addresses that aren't private or local. It resolves names itself and dials the
// checked address, so DNS rebinding can't swap in another. Redirects are new requests, checked the same way.
type Proxy struct {
	allowPrivate bool // AI_TEST_ALLOW_PRIVATE, dev only: sites on this machine or the LAN
	resolve      func(ctx context.Context, host string) ([]netip.Addr, error)

	mu   sync.Mutex
	runs map[string]*proxyRun
}

type proxyRun struct {
	token     string
	hosts     []string
	transport *http.Transport
	blocked   []string // hosts refused since the runner last asked
}

const blockedMsg = "Blocked: host isn't a registered test site"

func NewProxy(allowPrivate bool) *Proxy {
	return &Proxy{allowPrivate: allowPrivate, runs: map[string]*proxyRun{},
		resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}}
}

// open lets the run's requests out to hosts until close.
func (p *Proxy) open(runID, token string, hosts []string) {
	run := &proxyRun{token: token, hosts: hosts}
	run.transport = &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return p.dial(ctx, run, addr)
		},
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 30 * time.Second,
	}
	p.mu.Lock()
	p.runs[runID] = run
	p.mu.Unlock()
}

func (p *Proxy) close(runID string) {
	p.mu.Lock()
	run := p.runs[runID]
	delete(p.runs, runID)
	p.mu.Unlock()
	if run != nil {
		run.transport.CloseIdleConnections()
	}
}

// takeBlocked returns the hosts refused for the run since the last call.
func (p *Proxy) takeBlocked(runID string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	run := p.runs[runID]
	if run == nil {
		return nil
	}
	b := run.blocked
	run.blocked = nil
	return b
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	run, ok := p.authorize(r)
	if !ok {
		w.Header().Set("Proxy-Authenticate", `Basic realm="taskwell"`)
		http.Error(w, "Proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if run == nil {
		http.Error(w, "Run finished", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodConnect {
		p.connect(w, r, run)
		return
	}
	if r.URL.Scheme != "http" || r.URL.Host == "" {
		http.Error(w, "Only http and https are proxied", http.StatusBadRequest)
		return
	}
	if !p.allowed(run, r.URL.Hostname()) {
		http.Error(w, blockedMsg, http.StatusForbidden)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, h := range []string{"Proxy-Authorization", "Proxy-Connection", "Connection", "Keep-Alive", "Te", "Trailer", "Upgrade"} {
		out.Header.Del(h)
	}
	resp, err := run.transport.RoundTrip(out)
	if err != nil {
		var be blockedError
		if errors.As(err, &be) {
			http.Error(w, blockedMsg, http.StatusForbidden)
			return
		}
		http.Error(w, "Couldn't reach the site", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body) // the browser or the site went away
}

func (p *Proxy) connect(w http.ResponseWriter, r *http.Request, run *proxyRun) {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil || !p.allowed(run, host) {
		http.Error(w, blockedMsg, http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	upstream, err := p.dial(ctx, run, r.Host)
	cancel()
	if err != nil {
		var be blockedError
		if errors.As(err, &be) {
			http.Error(w, blockedMsg, http.StatusForbidden)
			return
		}
		http.Error(w, "Couldn't reach the site", http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "Can't tunnel", http.StatusInternalServerError)
		return
	}
	client, buf, err := hj.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); err != nil {
		client.Close()
		upstream.Close()
		return
	}
	go pipe(upstream, buf.Reader, client) // buf holds anything the client sent after CONNECT
	pipe(client, upstream, upstream)
}

// pipe copies src to dst, then closes both ends' connections so the other direction ends too.
func pipe(dst io.Writer, src io.Reader, closer io.Closer) {
	_, _ = io.Copy(dst, src)
	if c, ok := dst.(io.Closer); ok {
		c.Close()
	}
	closer.Close()
}

// authorize returns the run the request's Proxy-Authorization names; ok=false when it has none (ask for it),
// nil when it names no open run or the token is wrong.
func (p *Proxy) authorize(r *http.Request) (*proxyRun, bool) {
	enc, ok := strings.CutPrefix(r.Header.Get("Proxy-Authorization"), "Basic ")
	if !ok {
		return nil, false
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, true
	}
	runID, token, _ := strings.Cut(string(raw), ":")
	p.mu.Lock()
	run := p.runs[runID]
	p.mu.Unlock()
	if run == nil || subtle.ConstantTimeCompare([]byte(token), []byte(run.token)) != 1 {
		return nil, true
	}
	return run, true
}

// allowed checks host against the run's sites, noting it for the step log when it's refused.
func (p *Proxy) allowed(run *proxyRun, host string) bool {
	if allowedHost(host, run.hosts) {
		return true
	}
	p.block(run, host)
	return false
}

func (p *Proxy) block(run *proxyRun, host string) {
	p.mu.Lock()
	run.blocked = append(run.blocked, strings.ToLower(host))
	p.mu.Unlock()
}

type blockedError struct{ host string }

func (e blockedError) Error() string { return "blocked " + e.host }

// dial connects to addr (host:port) for the run after checking the host and every address it resolves to.
func (p *Proxy) dial(ctx context.Context, run *proxyRun, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if !allowedHost(host, run.hosts) {
		p.block(run, host)
		return nil, blockedError{host}
	}
	ips, err := netguard.Addrs(ctx, p.resolve, host, p.allowPrivate)
	if errors.As(err, new(netguard.PrivateError)) {
		slog.Warn("proxy refused a private address", "host", host)
		p.block(run, host)
		return nil, blockedError{host}
	}
	if err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	return d.DialContext(ctx, "tcp", net.JoinHostPort(ips[0].Unmap().String(), port))
}

// allowedHost reports whether host is one of sites or a subdomain of one (sites are lowercased hosts).
func allowedHost(host string, sites []string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	for _, s := range sites {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}
