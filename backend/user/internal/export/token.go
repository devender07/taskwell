package export

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// tokenTTL is how long a download link works.
const tokenTTL = 10 * time.Minute

// sign returns a download token for the user's export of the workspace that works until exp:
// base64url("<workspace id>.<user id>.<unix exp>") "." base64url(HMAC-SHA256 of that).
func sign(key []byte, wsID, userID string, exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(wsID + "." + userID + "." + strconv.FormatInt(exp.Unix(), 10)))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac(key, payload))
}

// verify returns the workspace and user a token names; ok=false when it's malformed, forged or expired at now.
func verify(key []byte, token string, now time.Time) (wsID, userID string, ok bool) {
	payload, sig, found := strings.Cut(token, ".")
	if !found {
		return "", "", false
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, mac(key, payload)) {
		return "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(string(raw), ".")
	if len(parts) != 3 {
		return "", "", false
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || now.Unix() >= exp {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func mac(key []byte, payload string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(payload))
	return m.Sum(nil)
}
