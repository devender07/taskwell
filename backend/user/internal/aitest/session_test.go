package aitest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestSession drives a tester session in real Chrome against a local site: it signs in with the sealed password,
// refuses the password in a text field, keeps only the assertions that hold, refuses a wrong token and a second
// session past the browser slots, closes with replayable steps, and ends an idle session. Needs CHROME_URL, like
// TestRunner.
func TestSession(t *testing.T) {
	chromeURL, proxy, proxyURL := testBrowser(t)
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, wsID, _ := testIssue(t, pool)
	m := &model{db: pool}
	key, _ := Key("", false)

	const password = "s3cret-pw"
	var mu sync.Mutex
	var gotPassword string
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login" && r.Method == http.MethodPost:
			mu.Lock()
			gotPassword = r.FormValue("password")
			mu.Unlock()
			http.Redirect(w, r, "/home", http.StatusSeeOther)
		case r.URL.Path == "/login":
			fmt.Fprint(w, `<title>Sign in</title><form method=post action=/login><input name=email aria-label=Email>
				<input name=password type=password aria-label=Password><button>Sign in</button></form>`)
		case r.URL.Path == "/home":
			// Fields apps build without a plain input: a plaintext-only editable, and a placeholder that turns into an
			// input when focused.
			fmt.Fprint(w, `<title>Home</title><p>Welcome Ann</p><div contenteditable=plaintext-only aria-label=Subject></div>
				<div tabindex=0 onfocus="const i = document.createElement('input'); i.ariaLabel = 'To'; this.replaceWith(i); i.focus()">Email ID</div><div contenteditable></div>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()
	if _, err := m.createSite(ctx, wsID, "x", ann, site.URL, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.saveAccount(ctx, key, wsID, "x", ann, "", "Admin", "ann@acme.test", password, ""); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(pool, sysconfig.New(pool), RunnerOptions{Key: key, ChromeURL: chromeURL, ProxyURL: proxyURL, Browsers: 1, Proxy: proxy})
	mux := http.NewServeMux()
	r.RegisterSessions(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer r.endSessions()

	send := func(method, path, token string, body any, want int) sessionAnswer {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
		req.Header.Set(sessionHeader, token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var a sessionAnswer
		_ = json.NewDecoder(res.Body).Decode(&a)
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d %+v, want %d", method, path, res.StatusCode, a, want)
		}
		return a
	}
	open := map[string]string{"workspace_id": wsID, "url": site.URL + "/login"}
	send("POST", "/sessions", "", map[string]string{"workspace_id": wsID, "url": "https://evil.test"}, 400)
	s := send("POST", "/sessions", "", open, 201)
	if !strings.Contains(s.Page.Elements, `[2] input type=password "Password"`) || len(s.Steps) != 1 || s.Steps[0].Tool != "navigate" {
		t.Fatalf("opened %+v", s)
	}
	send("POST", "/sessions", "", open, 409) // one browser, taken
	path := "/sessions/" + s.ID
	send("POST", path+"/actions", "wrong", map[string]any{"action": "snapshot"}, 404)

	act := func(action map[string]any, wantErr string) sessionAnswer {
		t.Helper()
		a := send("POST", path+"/actions", s.Token, action, 200)
		if a.Error != wantErr {
			t.Fatalf("%v: error %q, want %q", action, a.Error, wantErr)
		}
		return a
	}
	act(map[string]any{"action": "type", "ref": 1, "text": "ann@acme.test"}, "")
	act(map[string]any{"action": "fill_password", "ref": 1, "account": "Admin"}, "Passwords go only into password fields.")
	act(map[string]any{"action": "fill_password", "ref": 2, "account": "Admin"}, "")
	act(map[string]any{"action": "click", "ref": 3}, "")
	act(map[string]any{"action": "assert_no_text", "text": "Welcome Ann"}, `Expected "Welcome Ann" not to be on the page.`)
	act(map[string]any{"action": "assert_text", "text": "Welcome Ann"}, "")
	a := act(map[string]any{"action": "assert_no_text", "text": "Sign in"}, "")
	if len(a.Steps) != 6 || !strings.Contains(a.Page.Elements, "Welcome Ann") {
		t.Fatalf("after sign-in %+v", a)
	}
	if !strings.Contains(a.Page.Elements, `[1] div "Subject"`) || !strings.Contains(a.Page.Elements, `[2] div "Email ID"`) {
		t.Fatalf("custom fields not listed: %s", a.Page.Elements)
	}
	act(map[string]any{"action": "type", "ref": 1, "text": "Hello"}, "")
	a = act(map[string]any{"action": "type", "ref": 2, "text": "bo@acme.test"}, "")
	if !strings.Contains(a.Page.Elements, `input type=text "To" value="bo@acme.test"`) || !strings.Contains(a.Page.Elements, "Hello") {
		t.Fatalf("typing into custom fields %+v", a)
	}
	a = act(map[string]any{"action": "type", "ref": 3, "text": "Body"}, "") // no name: found by its path
	if !strings.Contains(a.Page.Elements, `div "Body"`) {
		t.Fatalf("typing into an unnamed field %+v", a)
	}
	n := len(a.Steps)
	a = act(map[string]any{"action": "assert_no_text", "text": "Welcome Ann", "failing": true}, "")
	if len(a.Steps) != n+1 || !strings.HasPrefix(a.Page.Elements, "Recorded as currently failing") {
		t.Fatalf("failing assertion %+v", a)
	}
	if gotPassword != password {
		t.Errorf("site got password %q", gotPassword)
	}
	steps := send("POST", path+"/close", s.Token, nil, 200).Steps
	if b, _ := json.Marshal(steps); bytes.Contains(b, []byte(password)) {
		t.Errorf("password in the steps: %s", b)
	}
	if steps[2].Tool != "fill_password" || steps[2].Account != "Admin" || steps[2].Target == nil || steps[2].Target.Name != "Password" {
		t.Errorf("fill_password step %+v", steps[2])
	}
	if _, msg := checkScript(script{Steps: steps, Check: "Welcome Ann"}, []string{"127.0.0.1"}, []string{"Admin"}); msg != "" {
		t.Errorf("recorded steps don't pass checkScript: %s", msg)
	}
	send("POST", path+"/actions", s.Token, map[string]any{"action": "snapshot"}, 404)

	s = send("POST", "/sessions", "", open, 201) // the slot is free again
	r.sweep(time.Now().Add(sessionIdle))
	send("POST", "/sessions/"+s.ID+"/actions", s.Token, map[string]any{"action": "snapshot"}, 404)
	if len(r.slots) != 0 {
		t.Errorf("%d slots taken after the idle session ended", len(r.slots))
	}
}

// TestSessionAPI checks the user API's browser sessions against a fake tester: the URL must be a test site, only
// the user who opened a session can use it, save goes through checkScript (keeping the session open when it
// fails) and closes it, and a session the tester lost answers 404.
func TestSessionAPI(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, wsID, issueID := testIssue(t, pool)
	bob := dbtest.User(t, pool, "Bob")
	dbtest.Join(t, pool, wsID, bob, "admin") // may manage tests, but not use Ann's session
	m := &model{db: pool}
	var slug string
	if err := pool.QueryRow(ctx, `select slug from workspaces where id = $1`, wsID).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	caseID, err := m.createCase(ctx, issueID, ann, caseFields{Title: "Sign in", Steps: []string{"Sign in"}, Expected: "Home"})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var opened string
	closed := map[string]bool{}
	steps := `[{"tool":"click","target":{"css":"","tag":"button","name":"Go"}}]`
	ids := []string{newUUID(), newUUID()}
	tester := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method+" "+r.URL.Path == "POST /sessions" {
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			opened = in["url"]
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"id":%q,"token":"tok","page":{"url":%q,"title":"","elements":""},"steps":[]}`, ids[0], in["url"])
			ids = ids[1:]
			return
		}
		id, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/sessions/"), "/")
		if r.Header.Get(sessionHeader) != "tok" || closed[id] {
			http.Error(w, `{"error":"gone"}`, http.StatusNotFound)
			return
		}
		switch rest {
		case "actions":
			fmt.Fprintf(w, `{"page":{"url":"","title":"","elements":"[1] button \"Go\""},"steps":%s}`, steps)
		case "close":
			closed[id] = true
			fmt.Fprintf(w, `{"steps":%s}`, steps)
		}
	}))
	defer tester.Close()
	if err := m.heartbeat(ctx, tester.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := m.createSite(ctx, wsID, slug, ann, "https://app.acme.test", "app.acme.test"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	New(pool, sysconfig.New(pool), Options{}, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	base := "/api/workspaces/" + slug
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, code, body, want)
		}
		return body
	}
	if body := do(ann, "POST", "/issues/ENG-1/browser-sessions", url.Values{"url": {"https://evil.test"}}, 400); !strings.Contains(body, msgSessionURL) {
		t.Errorf("other host: %s", body)
	}
	var s struct{ ID string }
	if err := json.Unmarshal([]byte(do(ann, "POST", "/issues/ENG-1/browser-sessions", url.Values{}, 201)), &s); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if opened != "https://app.acme.test" {
		t.Errorf("opened %q, want the first site", opened)
	}
	mu.Unlock()
	path := "/browser-sessions/" + s.ID
	if body := do(bob, "POST", path+"/actions", url.Values{"action": {"snapshot"}}, 404); !strings.Contains(body, msgSessionEnded) {
		t.Errorf("another user: %s", body)
	}
	do(ann, "POST", path+"/actions", url.Values{"action": {"fly"}}, 400)
	if body := do(ann, "POST", path+"/actions", url.Values{"action": {"click"}, "ref": {"1"}}, 200); !strings.Contains(body, `"steps":1`) {
		t.Errorf("action: %s", body)
	}
	if body := do(ann, "POST", path+"/save", url.Values{"case_id": {caseID}, "check": {"Home"}}, 400); !strings.Contains(body, "Step 1: start with a navigate step.") {
		t.Errorf("bad steps: %s", body)
	}
	do(ann, "POST", path+"/save", url.Values{"case_id": {caseID}}, 400)
	mu.Lock()
	if closed[s.ID] {
		t.Fatal("a failed save closed the session")
	}
	steps = `[{"tool":"navigate","url":"https://app.acme.test/login"},{"tool":"assert_no_text","text":"Error"}]`
	mu.Unlock()
	if body := do(ann, "POST", path+"/save", url.Values{"case_id": {caseID}, "check": {"Home"}}, 200); !strings.Contains(body, `"recorded":true`) {
		t.Errorf("save: %s", body)
	}
	mu.Lock()
	if !closed[s.ID] {
		t.Error("save left the session open on the tester")
	}
	mu.Unlock()
	do(ann, "POST", path+"/actions", url.Values{"action": {"snapshot"}}, 404)

	if err := json.Unmarshal([]byte(do(ann, "POST", "/issues/ENG-1/browser-sessions", url.Values{}, 201)), &s); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	closed[s.ID] = true // the tester ended it (idle)
	mu.Unlock()
	do(ann, "POST", "/browser-sessions/"+s.ID+"/actions", url.Values{"action": {"snapshot"}}, 404)
	var open bool
	if err := pool.QueryRow(ctx, `select closed_at is null from browser_sessions where id = $1`, s.ID).Scan(&open); err != nil || open {
		t.Errorf("lost session still open here: %v %v", open, err)
	}
}
