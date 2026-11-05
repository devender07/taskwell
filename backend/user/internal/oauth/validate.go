package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/url"
	"strings"
)

// redirectOK reports whether a client may register uri: http on a loopback host (any port and path), or any https
// URL, without a fragment.
func redirectOK(uri string) bool {
	u, err := url.Parse(uri)
	if err != nil || u.Host == "" || u.Opaque != "" || strings.Contains(uri, "#") || len(uri) > 2000 {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		h := u.Hostname()
		return h == "localhost" || h == "127.0.0.1" || h == "::1"
	}
	return false
}

// pkceOK reports whether verifier hashes to challenge under S256, in constant time.
func pkceOK(verifier, challenge string) bool {
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

// withQuery appends v to uri's query string.
func withQuery(uri string, v url.Values) string {
	if strings.Contains(uri, "?") {
		return uri + "&" + v.Encode()
	}
	return uri + "?" + v.Encode()
}
