package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
)

var googleClient = &http.Client{Timeout: 10 * time.Second}

type googleConfig struct {
	clientID, clientSecret, redirectURI string
}

type googleIdentity struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Aud           string `json:"aud"`
	Iss           string `json:"iss"`
	Exp           int64  `json:"exp"`
}

// googleConsentURL is where the browser goes to pick a Google account (authorization code flow with PKCE).
func googleConsentURL(cfg googleConfig, state, verifier string) string {
	q := url.Values{
		"client_id":             {cfg.clientID},
		"redirect_uri":          {cfg.redirectURI},
		"response_type":         {"code"},
		"scope":                 {"openid email profile"},
		"state":                 {state},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
		"prompt":                {"select_account"},
	}
	return googleAuthURL + "?" + q.Encode()
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// exchangeGoogleCode trades the callback code for the user's identity. The ID token comes straight from Google's
// token endpoint over TLS, so per OIDC its claims are checked (aud, iss, exp) but its signature need not be.
func exchangeGoogleCode(ctx context.Context, cfg googleConfig, code, verifier string) (googleIdentity, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {cfg.clientID},
		"client_secret": {cfg.clientSecret},
		"redirect_uri":  {cfg.redirectURI},
		"grant_type":    {"authorization_code"},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return googleIdentity{}, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := googleClient.Do(req)
	if err != nil {
		return googleIdentity{}, fmt.Errorf("google token request: %w", err)
	}
	defer res.Body.Close()
	var body struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return googleIdentity{}, fmt.Errorf("decode google token response: %w", err)
	}
	if res.StatusCode != http.StatusOK || body.IDToken == "" {
		return googleIdentity{}, fmt.Errorf("google token exchange: status %d, error %q", res.StatusCode, body.Error)
	}
	return parseGoogleIDToken(body.IDToken, cfg.clientID, time.Now())
}

func parseGoogleIDToken(token, clientID string, now time.Time) (googleIdentity, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return googleIdentity{}, errors.New("id token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return googleIdentity{}, fmt.Errorf("decode id token: %w", err)
	}
	var id googleIdentity
	if err := json.Unmarshal(payload, &id); err != nil {
		return googleIdentity{}, fmt.Errorf("parse id token: %w", err)
	}
	switch {
	case id.Aud != clientID:
		return googleIdentity{}, errors.New("id token audience mismatch")
	case id.Iss != "https://accounts.google.com" && id.Iss != "accounts.google.com":
		return googleIdentity{}, errors.New("id token issuer mismatch")
	case now.Unix() >= id.Exp:
		return googleIdentity{}, errors.New("id token expired")
	case id.Sub == "" || id.Email == "":
		return googleIdentity{}, errors.New("id token missing sub or email")
	}
	return id, nil
}
