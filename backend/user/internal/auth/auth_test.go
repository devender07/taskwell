package auth

import (
	"bytes"
	"encoding/base64"
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
	admin := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: issuer, Subject: "user-1",
		Audience: jwt.ClaimStrings{"admin"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))})
	adminTok, _ := admin.SignedString(secret)
	for name, bad := range map[string]string{
		"wrong secret": func() string { s, _ := signAccess([]byte("other"), "user-1", time.Minute); return s }(),
		"expired":      expired,
		"alg none":     noneTok,
		"alg HS512":    hs512Tok,
		"admin aud":    adminTok,
		"tampered":     tok[:len(tok)-2] + "xx",
		"garbage":      "not.a.jwt",
	} {
		if _, err := parseAccess(secret, bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestGoogleIDToken(t *testing.T) {
	now := time.Unix(1_000, 0)
	enc := func(claims string) string {
		return "h." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".s"
	}
	good := `{"sub":"1","email":"a@b.co","email_verified":true,"aud":"cid","iss":"https://accounts.google.com","exp":2000}`
	if id, err := parseGoogleIDToken(enc(good), "cid", now); err != nil || id.Sub != "1" || !id.EmailVerified {
		t.Fatalf("valid token: %+v, %v", id, err)
	}
	for name, claims := range map[string]string{
		"wrong aud": `{"sub":"1","email":"a@b.co","aud":"other","iss":"accounts.google.com","exp":2000}`,
		"wrong iss": `{"sub":"1","email":"a@b.co","aud":"cid","iss":"https://evil.example","exp":2000}`,
		"expired":   `{"sub":"1","email":"a@b.co","aud":"cid","iss":"accounts.google.com","exp":999}`,
		"no email":  `{"sub":"1","aud":"cid","iss":"accounts.google.com","exp":2000}`,
	} {
		if _, err := parseGoogleIDToken(enc(claims), "cid", now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// RFC 7636 appendix B test vector.
	if got := pkceChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Errorf("pkce challenge = %s", got)
	}
}
