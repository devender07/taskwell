package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestToken(t *testing.T) {
	secret := []byte("test-secret")
	now := time.Now()
	tok, _ := signToken(secret, "user-1", "sid-1", now, ttl)
	if id, sid, err := parseToken(secret, tok); err != nil || id != "user-1" || sid != "sid-1" {
		t.Fatalf("valid token: got %q, %q, %v", id, sid, err)
	}
	// A user-service access token: same secret and issuer, no audience.
	userTok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: issuer, Subject: "user-1",
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))}).SignedString(secret)
	expired, _ := signToken(secret, "user-1", "sid-1", now.Add(-ttl-time.Minute), ttl)
	other, _ := signToken([]byte("other"), "user-1", "sid-1", now, ttl)
	noSession, _ := signToken(secret, "user-1", "", now, ttl)
	for name, bad := range map[string]string{"user token": userTok, "expired": expired, "wrong secret": other,
		"no session": noSession} {
		if _, _, err := parseToken(secret, bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
