package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/devender07/taskwell/backend/shared/totp"
	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/mail"
	"github.com/devender07/taskwell/backend/user/internal/secret"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
)

// TestTwoFactor walks two-factor through the API: setup needs the password, enable needs a code, then every sign-in
// method stops at mfa_required until POST /api/auth/mfa; codes can't be replayed, recovery codes work once, the
// pending sign-in expires and ends after mfaMaxFails wrong codes, and disabling takes a code.
func TestTwoFactor(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	cfg := sysconfig.New(pool)
	jwtKey := []byte("two-factor-test-secret-at-least-32-bytes")
	h := New(pool, cfg, mail.New(pool, cfg, time.Second, time.Second, 1), jwtKey, nil)
	h.MFAKey = totp.Key("test")
	mux := http.NewServeMux()
	h.Register(mux)

	userID := dbtest.User(t, pool, "Mfa")
	ph, _ := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	var email string
	if err := pool.QueryRow(ctx, `update users set password_hash = $2 where id = $1 returning email`, userID, string(ph)).Scan(&email); err != nil {
		t.Fatal(err)
	}
	resetFailures := func() {
		if _, err := pool.Exec(ctx, `delete from auth_failures where key in ('mfa-user:' || $1, 'pw:' || $2)`, userID, email); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(resetFailures)

	do := func(path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		method := "POST"
		if form == nil {
			method = "GET"
		}
		r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.RemoteAddr = fmt.Sprintf("203.0.113.%d:1234", rand.IntN(250)+1) // spread over the per-IP limit
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
	signin := func() *httptest.ResponseRecorder {
		return do("/api/auth/signin", url.Values{"email": {email}, "password": {"correct horse"}})
	}

	w := signin()
	access := cookie(w, accessCookie)
	if w.Code != 200 || access == nil {
		t.Fatalf("sign-in before two-factor: %d %s", w.Code, w.Body)
	}
	if w := do("/api/me/mfa/setup", url.Values{"password": {"wrong"}}, access); w.Code != 400 || !strings.Contains(w.Body.String(), "Wrong password.") {
		t.Fatalf("setup with a wrong password: %d %s", w.Code, w.Body)
	}
	w = do("/api/me/mfa/setup", url.Values{"password": {"correct horse"}}, access)
	var setup struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &setup); err != nil || w.Code != 200 || setup.Secret == "" {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	if w := do("/api/me/mfa/enable", url.Values{"code": {"000000"}}, access); w.Code != 400 {
		t.Fatalf("enable with a wrong code: %d %s", w.Code, w.Body)
	}
	code, _ := totp.Code(setup.Secret, time.Now().Unix()/30)
	w = do("/api/me/mfa/enable", url.Values{"code": {code}}, access)
	var rec struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rec); err != nil || w.Code != 200 || len(rec.RecoveryCodes) != totp.RecoveryCount {
		t.Fatalf("enable: %d %s", w.Code, w.Body)
	}
	if w := do("/api/me/mfa/setup", url.Values{"password": {"correct horse"}}, access); w.Code != 409 {
		t.Fatalf("setup while on: %d %s", w.Code, w.Body)
	}
	if w := do("/api/me/mfa", nil, access); !strings.Contains(w.Body.String(), `"enabled":true`) || !strings.Contains(w.Body.String(), `"recovery_left":10`) {
		t.Fatalf("status: %s", w.Body)
	}

	// Password sign-in now stops at the second step, with no session.
	w = signin()
	pending := cookie(w, mfaCookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mfa_required":true`) || pending == nil || cookie(w, accessCookie) != nil {
		t.Fatalf("sign-in with two-factor: %d %s", w.Code, w.Body)
	}
	if c, err := parseAccess(jwtKey, pending.Value); err == nil {
		t.Fatalf("the pending sign-in token works as an access token: %+v", c)
	}
	if w := do("/api/auth/mfa", url.Values{"code": {code}}); w.Code != 401 {
		t.Fatalf("no pending sign-in: %d", w.Code)
	}
	if w := do("/api/auth/mfa", url.Values{"code": {code}}, pending); w.Code != 400 {
		t.Fatalf("replayed code: %d %s", w.Code, w.Body)
	}
	w = do("/api/auth/mfa", url.Values{"recovery_code": {strings.ToUpper(rec.RecoveryCodes[0])}}, pending)
	if w.Code != 200 || cookie(w, accessCookie) == nil || !strings.Contains(w.Body.String(), userID) {
		t.Fatalf("recovery code: %d %s", w.Code, w.Body)
	}
	if w := do("/api/auth/mfa", url.Values{"recovery_code": {rec.RecoveryCodes[0]}}, cookie(signin(), mfaCookie)); w.Code != 400 {
		t.Fatalf("recovery code used twice: %d %s", w.Code, w.Body)
	}
	if _, err := pool.Exec(ctx, `update user_mfa set last_step = 0 where user_id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	w = do("/api/auth/mfa", url.Values{"code": {code}}, cookie(signin(), mfaCookie))
	other := cookie(w, accessCookie)
	if w.Code != 200 || other == nil {
		t.Fatalf("app code: %d %s", w.Code, w.Body)
	}

	// The email-code sign-in stops too.
	if err := h.model.replaceToken(ctx, userID, purposeCode, secret.Hash("123456"), 10); err != nil {
		t.Fatal(err)
	}
	if w := do("/api/auth/otp/verify", url.Values{"email": {email}, "code": {"123456"}}); !strings.Contains(w.Body.String(), `"mfa_required":true`) ||
		cookie(w, accessCookie) != nil {
		t.Fatalf("email code with two-factor: %d %s", w.Code, w.Body)
	}

	// An expired pending sign-in, and one with too many wrong codes.
	expired, _ := signMFA(jwtKey, userID, false, -time.Second)
	if w := do("/api/auth/mfa", url.Values{"recovery_code": {rec.RecoveryCodes[1]}}, &http.Cookie{Name: mfaCookie, Value: expired}); w.Code != 401 {
		t.Fatalf("expired: %d", w.Code)
	}
	pending = cookie(signin(), mfaCookie)
	for i := range mfaMaxFails {
		w := do("/api/auth/mfa", url.Values{"code": {"000000"}}, pending)
		if want := map[bool]int{true: 401, false: 400}[i == mfaMaxFails-1]; w.Code != want {
			t.Fatalf("wrong code %d: %d, want %d", i+1, w.Code, want)
		}
	}
	if w := do("/api/auth/mfa", url.Values{"recovery_code": {rec.RecoveryCodes[1]}}, pending); w.Code != 401 {
		t.Fatalf("after the attempt cap: %d", w.Code)
	}

	resetFailures()
	if w := do("/api/me/mfa/recovery", url.Values{"code": {"000000"}}, access); w.Code != 400 {
		t.Fatalf("new recovery codes with a wrong code: %d", w.Code)
	}
	if w := do("/api/me/mfa/disable", url.Values{"recovery_code": {"nope-nope-nope"}}, access); w.Code != 400 {
		t.Fatalf("disable with a wrong code: %d", w.Code)
	}
	if w := do("/api/me/mfa/disable", url.Values{"recovery_code": {rec.RecoveryCodes[2]}}, access); w.Code != 204 {
		t.Fatalf("disable: %d %s", w.Code, w.Body)
	}
	if w := do("/api/me/mfa", nil, access); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("this session after disabling: %d %s", w.Code, w.Body)
	}
	if w := do("/api/me/mfa", nil, other); w.Code != 401 {
		t.Fatalf("another session after disabling: %d", w.Code)
	}
	if w := signin(); w.Code != 200 || cookie(w, accessCookie) == nil {
		t.Fatalf("sign-in after turning it off: %d %s", w.Code, w.Body)
	}
}
