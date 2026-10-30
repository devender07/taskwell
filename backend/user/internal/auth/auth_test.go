package auth

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestValidation(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{checkEmail(""), "Email is required."},
		{checkEmail("  "), "Email is required."},
		{checkEmail("a@b"), "Enter a valid email address."},
		{checkEmail("a b@c.d"), "Enter a valid email address."},
		{checkEmail(strings.Repeat("a", 250) + "@b.co"), "Enter a valid email address."},
		{checkEmail(" Ann@Example.com "), ""},
		{checkName(""), "Name is required."},
		{checkName(strings.Repeat("é", 101)), "Name must be 100 characters or fewer."},
		{checkName(strings.Repeat("é", 100)), ""},
		{checkPassword(""), "Password is required."},
		{checkPassword("1234567"), "Password must be at least 8 characters."},
		{checkPassword(strings.Repeat("é", 37)), "Password is too long."}, // 37 chars, 74 bytes
		{checkPassword(strings.Repeat("a", 72)), ""},
		{checkCode("12345"), "Enter the 6-digit code."},
		{checkCode("12345a"), "Enter the 6-digit code."},
		{checkCode("012345"), ""},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
}

func TestTokens(t *testing.T) {
	a, b := newToken(), newToken()
	if a == b || len(a) != 43 {
		t.Fatalf("tokens not unique or wrong length: %q %q", a, b)
	}
	if !bytes.Equal(hash(a), hash(a)) || bytes.Equal(hash(a), hash(b)) || len(hash(a)) != 32 {
		t.Fatal("hash is not a deterministic sha256")
	}
	for range 100 {
		c, err := newCode()
		if err != nil || checkCode(c) != "" {
			t.Fatalf("bad code %q: %v", c, err)
		}
	}
}

func TestAccessToken(t *testing.T) {
	secret := []byte("test-secret")
	tok, err := signAccess(secret, "user-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := parseAccess(secret, tok); err != nil || id != "user-1" {
		t.Fatalf("valid token: got %q, %v", id, err)
	}
	expired, _ := signAccess(secret, "user-1", -time.Minute)
	none := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{Issuer: issuer, Subject: "user-1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))})
	noneTok, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	hs512 := jwt.NewWithClaims(jwt.SigningMethodHS512, jwt.RegisteredClaims{Issuer: issuer, Subject: "user-1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))})
	hs512Tok, _ := hs512.SignedString(secret)
	for name, bad := range map[string]string{
		"wrong secret": func() string { s, _ := signAccess([]byte("other"), "user-1", time.Minute); return s }(),
		"expired":      expired,
		"alg none":     noneTok,
		"alg HS512":    hs512Tok,
		"tampered":     tok[:len(tok)-2] + "xx",
		"garbage":      "not.a.jwt",
	} {
		if _, err := parseAccess(secret, bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
