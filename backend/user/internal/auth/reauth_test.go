package auth

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/mail"
	"github.com/devender07/taskwell/backend/user/internal/secret"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
)

// TestReauth: creating an API token needs a fresh proof (403 reauth without one), a wrong password is a 401 and an
// account event, the right one or an emailed code sets the proof and the token is created, and the proof expires.
// Turning two-factor off is refused while a workspace the user is in requires it.
func TestReauth(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	cfg := sysconfig.New(pool)
	jwtKey := []byte("reauth-test-secret-at-least-32-bytes!!")
	h := New(pool, cfg, mail.New(pool, cfg, time.Second, time.Second, 1), jwtKey, nil)
	mux := http.NewServeMux()
	h.Register(mux)

	userID := dbtest.User(t, pool, "Reauth")
	ph, _ := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	var email string
	if err := pool.QueryRow(ctx, `update users set password_hash = $2 where id = $1 returning email`, userID, string(ph)).Scan(&email); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from auth_failures where key in ('pw:' || $1, 'code:' || $1)`, email)
		_, _ = pool.Exec(context.Background(), `delete from outbound_mail where recipient = $1`, email)
	})

	do := func(method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.RemoteAddr = fmt.Sprintf("203.0.113.%d:1234", rand.IntN(250)+1)
		for _, c := range cookies {
			if c != nil {
				r.AddCookie(c)
			}
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	cookie := func(w *httptest.ResponseRecorder, name string) *http.Cookie {
		for _, c := range w.Result().Cookies() {
			if c.Name == name && c.MaxAge >= 0 {
				return c
			}
		}
		return nil
	}
	access := cookie(do("POST", "/api/auth/signin", url.Values{"email": {email}, "password": {"correct horse"}}), accessCookie)
	if access == nil {
		t.Fatal("no access cookie")
	}
	token := func(proof *http.Cookie) *httptest.ResponseRecorder {
		return do("POST", "/api/me/tokens", url.Values{"name": {"CI"}}, access, proof)
	}

	if w := do("GET", "/api/me/reauth", nil, access); w.Code != 200 || !strings.Contains(w.Body.String(), `{"methods":["password","email"]}`) {
		t.Fatalf("methods: %d %s", w.Code, w.Body)
	}
	if w := token(nil); w.Code != 403 || !strings.Contains(w.Body.String(), `"reauth":true`) {
		t.Fatalf("token without proof: %d %s", w.Code, w.Body)
	}
	w := do("POST", "/api/auth/reauth", url.Values{"password": {"wrong"}}, access)
	if w.Code != 401 || !strings.Contains(w.Body.String(), "That password is incorrect.") || cookie(w, reauthCookie) != nil {
		t.Fatalf("wrong password: %d %s", w.Code, w.Body)
	}
	w = do("POST", "/api/auth/reauth", url.Values{"password": {"correct horse"}}, access)
	proof := cookie(w, reauthCookie)
	if w.Code != 204 || proof == nil || proof.Path != "/api" || proof.MaxAge != 600 || !proof.HttpOnly {
		t.Fatalf("right password: %d %s %+v", w.Code, w.Body, proof)
	}
	if w := token(proof); w.Code != 201 {
		t.Fatalf("token with proof: %d %s", w.Code, w.Body)
	}
	if _, err := parseAccess(jwtKey, proof.Value); err == nil {
		t.Fatal("the proof works as an access token")
	}

	expired, _ := signReauth(jwtKey, userID, -time.Second)
	if w := token(&http.Cookie{Name: reauthCookie, Value: expired}); w.Code != 403 {
		t.Fatalf("expired proof: %d %s", w.Code, w.Body)
	}
	someoneElse, _ := signReauth(jwtKey, dbtest.User(t, pool, "Other"), time.Minute)
	if w := token(&http.Cookie{Name: reauthCookie, Value: someoneElse}); w.Code != 403 {
		t.Fatalf("another user's proof: %d %s", w.Code, w.Body)
	}

	if err := h.model.replaceToken(ctx, userID, purposeReauth, secret.Hash("123456"), 10); err != nil {
		t.Fatal(err)
	}
	if w := do("POST", "/api/auth/reauth", url.Values{"email_code": {"654321"}}, access); w.Code != 401 ||
		!strings.Contains(w.Body.String(), `"email_code":"That code is incorrect."`) {
		t.Fatalf("wrong email code: %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/api/auth/reauth", url.Values{"email_code": {"123456"}}, access); w.Code != 204 || cookie(w, reauthCookie) == nil {
		t.Fatalf("email code: %d %s", w.Code, w.Body)
	}

	var ok, failed int
	if err := pool.QueryRow(ctx, `select count(*) filter (where action = 'reauth'), count(*) filter (where action = 'reauth_failed')
		from account_events where user_id = $1`, userID).Scan(&ok, &failed); err != nil || ok != 2 || failed != 2 {
		t.Fatalf("account events: %d reauth, %d reauth_failed, %v", ok, failed, err)
	}

	// Two-factor can't be turned off while a workspace the user is in requires it.
	if _, err := pool.Exec(ctx, `insert into user_mfa (user_id, secret, recovery_codes, enabled_at) values ($1, 'x', '{}', now())`, userID); err != nil {
		t.Fatal(err)
	}
	var wsID string
	if err := pool.QueryRow(ctx, `insert into workspaces (name, slug, created_by, require_mfa) values ('Strict', $1, $2, true)
		returning id::text`, dbtest.Name("strict-"), userID).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into workspace_roles (workspace_id, name, builtin, permissions) values ($1, 'Admin', 'admin', '{}')`, wsID); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, userID, "admin")
	if w := do("GET", "/api/me/reauth", nil, access); !strings.Contains(w.Body.String(), `["password","totp","email"]`) {
		t.Fatalf("methods with two-factor: %s", w.Body)
	}
	if w := do("POST", "/api/me/mfa/disable", url.Values{"code": {"123456"}}, access); w.Code != 409 ||
		!strings.Contains(w.Body.String(), "Strict requires two-factor sign-in.") {
		t.Fatalf("disable while required: %d %s", w.Code, w.Body)
	}
}
