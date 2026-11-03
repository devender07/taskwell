package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/devender07/tether/backend/shared/cors"
	"github.com/devender07/tether/backend/user/internal/web"
)

// CheckOrigin rejects cross-site writes to /api: a request other than GET, HEAD or OPTIONS passes only when the
// browser says it is same-origin, or its Origin is app_base_url's (the API on its own subdomain).
func (h *Handler) CheckOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		base, err := h.cfg.Get(r.Context(), "app_base_url")
		if err != nil {
			slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
			web.Err(w, http.StatusInternalServerError, "Something went wrong. Try again.")
			return
		}
		if !sameOrigin(r, base) {
			web.Err(w, http.StatusForbidden, "Cross-site request blocked.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether a write request may proceed. Browsers send Sec-Fetch-Site (all current ones) and
// Origin (on every non-GET request, since 2020) and pages can't forge either. A request with neither isn't from a
// browser, so it carries no victim's cookies: curl, scripts and tests pass.
func sameOrigin(r *http.Request, base string) bool {
	site, origin := r.Header.Get("Sec-Fetch-Site"), r.Header.Get("Origin")
	if site == "same-origin" || site == "none" {
		return true
	}
	if origin != "" {
		return origin == cors.Origin(base)
	}
	return site == ""
}

// AppOrigin is app_base_url's origin, the web app's: it may call the API from its own subdomain. "" when unreadable.
func (h *Handler) AppOrigin(r *http.Request) string {
	base, err := h.cfg.Get(r.Context(), "app_base_url")
	if err != nil {
		return ""
	}
	return cors.Origin(base)
}
