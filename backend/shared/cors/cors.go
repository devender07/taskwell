// Package cors lets a web app on its own subdomain call its API with the sign-in cookies.
package cors

import (
	"net/http"
	"net/url"
)

// Allow answers CORS for requests whose Origin is origin(r): preflights end here, the rest get the headers and go
// on. Any other origin passes through without them, so its page can't read the response.
func Allow(origin func(*http.Request) string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		o := r.Header.Get("Origin")
		if o == "" || o != origin(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", o)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Tether-Client")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Origin is the URL's scheme://host[:port], as browsers send it in Origin; "" when it has neither.
func Origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
