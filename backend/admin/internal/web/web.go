// Package web holds the JSON response helpers and handler wrappers every admin API package uses.
package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
)

const maxBody = 64 << 10

// Authenticator returns the signed-in admin's user ID, or "" when there is none.
type Authenticator func(*http.Request) (string, error)

// Public caps the body size and turns a returned error into a logged 500.
func Public(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if err := fn(w, r); err != nil {
			fail(w, r, err)
		}
	}
}

// Private is Public that first answers 401 unless an admin is signed in, and passes their user ID on.
func Private(auth Authenticator, fn func(w http.ResponseWriter, r *http.Request, userID string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		userID, err := auth(r)
		if err == nil && userID == "" {
			WriteErr(w, http.StatusUnauthorized, "Not signed in.")
			return
		}
		if err == nil {
			err = fn(w, r, userID)
		}
		if err != nil {
			fail(w, r, err)
		}
	}
}

// SameOrigin rejects cross-site writes: a request other than GET, HEAD or OPTIONS passes only when the browser says
// it is same-origin, or its Origin is this host. Browsers send Sec-Fetch-Site and Origin and pages can't forge them;
// a request with neither isn't from a browser, so it carries no victim's cookie (curl and the CLI pass).
func SameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site, origin := r.Header.Get("Sec-Fetch-Site"), r.Header.Get("Origin")
		ok := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions ||
			site == "same-origin" || site == "none" || (site == "" && origin == "")
		if !ok && origin != "" {
			u, err := url.Parse(origin)
			ok = err == nil && u.Host == r.Host
		}
		if !ok {
			WriteErr(w, http.StatusForbidden, "Cross-site request blocked.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func fail(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	WriteErr(w, http.StatusInternalServerError, "Something went wrong. Try again.")
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteErr(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

func WriteFields(w http.ResponseWriter, status int, errs map[string]string) {
	WriteJSON(w, status, map[string]map[string]string{"errors": errs})
}

// ParseAllowlist reads ADMIN_ALLOWED_IPS: comma-separated addresses and CIDR ranges. Empty allows every address (nil).
func ParseAllowlist(v string) ([]netip.Prefix, error) {
	var ps []netip.Prefix
	for _, f := range strings.Split(v, ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		if a, err := netip.ParseAddr(f); err == nil {
			a = a.Unmap()
			ps = append(ps, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(f)
		if err != nil {
			return nil, fmt.Errorf("%q is not an IP address or CIDR range", f)
		}
		ps = append(ps, p.Masked())
	}
	return ps, nil
}

// AllowIPs answers 403 to every request whose client address (ClientIP) is in none of allowed; nil allows all.
func AllowIPs(allowed []netip.Prefix, next http.Handler) http.Handler {
	if allowed == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, err := netip.ParseAddr(ClientIP(r))
		if err == nil && slices.ContainsFunc(allowed, func(p netip.Prefix) bool { return p.Contains(ip.Unmap()) }) {
			next.ServeHTTP(w, r)
			return
		}
		WriteErr(w, http.StatusForbidden, "Your network isn't allowed to use the admin app.")
	})
}

// ClientIP is the request's peer address, or, when the peer is a proxy on this host or a private network, the
// address that proxy appended last to X-Forwarded-For (Caddy and nginx's $proxy_add_x_forwarded_for do).
// A copy of the user service's auth.clientIP; keep the two in step.
// ponytail: any private-network peer is trusted as a proxy; pin a TRUSTED_PROXIES list if clients can reach the API
// from inside that network.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !(peer.IsLoopback() || peer.IsPrivate()) {
		return host
	}
	xff := r.Header.Values("X-Forwarded-For")
	if len(xff) == 0 {
		return host
	}
	parts := strings.Split(xff[len(xff)-1], ",")
	if last, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
		return last.String()
	}
	return host
}
