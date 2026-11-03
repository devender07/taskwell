package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/devender07/tether/backend/user/internal/auth"
	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
)

// TestFlow runs register, authorize (check, deny, allow) and the token exchange against the scratch database (see
// dbtest), and signs in with the token it returns.
func TestFlow(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann := dbtest.User(t, pool, "Ann")
	cfg := sysconfig.New(pool)
	mux := http.NewServeMux()
	New(pool, cfg, dbtest.Auth).Register(mux)
	const ip = "192.0.2.77"
	clearLimit := func() { _, _ = pool.Exec(ctx, `delete from rate_limits where key = $1`, "oauth_register:"+ip) }
	clearLimit()
	t.Cleanup(clearLimit)

	send := func(r *http.Request, want int, into any) {
		t.Helper()
		r.RemoteAddr = ip + ":1234"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s, want %d", r.Method, r.URL, w.Code, w.Body, want)
		}
		if into != nil {
			if err := json.Unmarshal(w.Body.Bytes(), into); err != nil {
				t.Fatal(err)
			}
		}
	}
	register := func(body string, want int) map[string]any {
		t.Helper()
		var out map[string]any
		send(httptest.NewRequest("POST", "/api/oauth/register", strings.NewReader(body)), want, &out)
		if id, _ := out["client_id"].(string); id != "" {
			t.Cleanup(func() { _, _ = pool.Exec(ctx, `delete from oauth_clients where id = $1`, id) })
		}
		return out
	}
	token := func(form url.Values, want int) map[string]string {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/oauth/token", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		var out map[string]string
		send(r, want, &out)
		return out
	}

	var meta map[string]any
	send(httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil), 200, &meta)
	if base, _ := cfg.Get(ctx, "app_base_url"); meta["token_endpoint"] != strings.TrimRight(base, "/")+"/api/oauth/token" {
		t.Errorf("metadata: %v", meta)
	}

	for body, code := range map[string]string{
		`{"redirect_uris":[]}`:                           "invalid_redirect_uri",
		`{"redirect_uris":["http://example.com/cb"]}`:    "invalid_redirect_uri",
		`{"redirect_uris":["https://example.com/cb#x"]}`: "invalid_redirect_uri",
		`{"client_name":"` + strings.Repeat("x", 101) + `","redirect_uris":["https://example.com/cb"]}`: "invalid_client_metadata",
		`nope`: "invalid_client_metadata",
	} {
		if got := register(body, 400)["error"]; got != code {
			t.Errorf("register %s: %v, want %s", body, got, code)
		}
	}
	const cb = "http://127.0.0.1:33418/callback"
	c := register(`{"client_name":"Claude Code","redirect_uris":["`+cb+`","https://claude.ai/api/mcp/auth_callback"]}`, 201)
	clientID := c["client_id"].(string)
	if c["client_name"] != "Claude Code" || c["token_endpoint_auth_method"] != "none" {
		t.Fatalf("register: %v", c)
	}

	verifier := "a-verifier-that-is-at-least-forty-three-characters-long"
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {cb},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "state": {"xyz"}}
	authz := func(method string, edit func(url.Values), want int) map[string]string {
		t.Helper()
		v := url.Values{}
		for k, vs := range q {
			v[k] = vs
		}
		edit(v)
		var r *http.Request
		if method == "GET" {
			r = httptest.NewRequest("GET", "/api/oauth/authorize?"+v.Encode(), nil)
		} else {
			r = httptest.NewRequest("POST", "/api/oauth/authorize", strings.NewReader(v.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		r.Header.Set("X-Test-User", ann)
		var out map[string]string
		send(r, want, &out)
		return out
	}
	none := func(url.Values) {}
	if got := authz("GET", none, 200); got["client_name"] != "Claude Code" || got["redirect_uri"] != cb {
		t.Fatalf("check: %v", got)
	}
	for name, edit := range map[string]func(url.Values){
		"unknown client":  func(v url.Values) { v.Set("client_id", "00000000-0000-0000-0000-000000000000") },
		"other redirect":  func(v url.Values) { v.Set("redirect_uri", "http://127.0.0.1:1/callback") },
		"response type":   func(v url.Values) { v.Set("response_type", "token") },
		"no challenge":    func(v url.Values) { v.Del("code_challenge") },
		"plain challenge": func(v url.Values) { v.Set("code_challenge_method", "plain") },
	} {
		if authz("GET", edit, 400)["error"] == "" {
			t.Errorf("%s: no error", name)
		}
	}
	if got := authz("POST", func(v url.Values) { v.Set("allow", "false") }, 200)["redirect"]; got != cb+"?error=access_denied&state=xyz" {
		t.Fatalf("deny: %s", got)
	}
	redeemable := func() string {
		t.Helper()
		u, err := url.Parse(authz("POST", func(v url.Values) { v.Set("allow", "true") }, 200)["redirect"])
		if err != nil || u.Query().Get("state") != "xyz" || u.Query().Get("code") == "" {
			t.Fatalf("allow: %v %v", u, err)
		}
		return u.Query().Get("code")
	}
	form := func(code string, edit func(url.Values)) url.Values {
		v := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {cb}, "client_id": {clientID},
			"code_verifier": {verifier}}
		edit(v)
		return v
	}

	code := redeemable()
	if got := token(form(code, func(v url.Values) { v.Set("code_verifier", verifier+"x") }), 400); got["error"] != "invalid_grant" {
		t.Errorf("bad verifier: %v", got)
	}
	if got := token(form(code, func(v url.Values) { v.Set("redirect_uri", "https://claude.ai/api/mcp/auth_callback") }), 400); got["error"] != "invalid_grant" {
		t.Errorf("wrong redirect_uri: %v", got)
	}
	if got := token(form(code, func(v url.Values) { v.Set("grant_type", "password") }), 400); got["error"] != "unsupported_grant_type" {
		t.Errorf("grant type: %v", got)
	}
	if got := token(form(code, func(v url.Values) { v.Del("code_verifier") }), 400); got["error"] != "invalid_request" {
		t.Errorf("no verifier: %v", got)
	}
	got := token(form(code, none), 200)
	if !strings.HasPrefix(got["access_token"], "tth_") || got["token_type"] != "Bearer" {
		t.Fatalf("token: %v", got)
	}
	if again := token(form(code, none), 400); again["error"] != "invalid_grant" {
		t.Errorf("reused code: %v", again)
	}

	r := httptest.NewRequest("GET", "/api/me", nil)
	r.Header.Set("Authorization", "Bearer "+got["access_token"])
	if id, err := auth.New(pool, cfg, nil, nil, nil).Authenticate(r); err != nil || id != ann {
		t.Fatalf("token signs in as %q, %v", id, err)
	}
	var name string
	if err := pool.QueryRow(ctx, `select name from api_tokens where user_id = $1`, ann).Scan(&name); err != nil || name != "Claude Code" {
		t.Errorf("token name %q, %v", name, err)
	}
	// A personal API token can't authorize more.
	r = httptest.NewRequest("GET", "/api/oauth/authorize?"+q.Encode(), nil)
	r.Header.Set("Authorization", "Bearer "+got["access_token"])
	r.Header.Set("X-Test-User", ann)
	send(r, 401, nil)
}
