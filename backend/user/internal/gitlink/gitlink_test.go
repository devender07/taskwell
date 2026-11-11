package gitlink

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/issue"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// TestWebhooks sends signed GitHub deliveries against the scratch database (see dbtest): opening moves a closing
// link's issue to the open status, redelivery changes nothing, merging moves it to the first done status with no
// actor (notifying watchers), a body reference never moves, and dropping a mention unlinks.
func TestWebhooks(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob")
	var wsID, slug string
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key) select id, 'Web', 'WEB' from w)
			select id::text, slug from w`, ann).Scan(&wsID, &slug)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, wsID, bob, "member")

	mux := http.NewServeMux()
	wsH := workspace.New(pool, dbtest.Auth, nil, nil)
	issue.New(pool, dbtest.Auth, wsH.Member).Register(mux)
	key, _ := Key("", "test jwt secret", false)
	New(pool, key, func(context.Context) (string, error) { return "https://taskwell.test", nil }, dbtest.Auth, wsH.Member).Register(mux)
	expect := func(step string, want int, user, method, path string, form url.Values, out any) {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, "/api/workspaces/"+slug+path, form)
		if code != want {
			t.Fatalf("%s: got %d %s, want %d", step, code, body, want)
		}
		if out != nil {
			if err := json.Unmarshal([]byte(body), out); err != nil {
				t.Fatalf("%s: %v in %s", step, err, body)
			}
		}
	}
	expect("issue 1", 201, ann, "POST", "/projects/WEB/issues", url.Values{"title": {"Login"}}, nil)
	expect("issue 2", 201, ann, "POST", "/projects/WEB/issues", url.Values{"title": {"Docs"}}, nil)
	var statusID string
	if err := pool.QueryRow(ctx, `select id::text from statuses where workspace_id = $1 and name = 'In progress'`, wsID).Scan(&statusID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into issue_watchers (issue_id, user_id) select id, $2 from issues where workspace_id = $1 and number = 1`,
		wsID, bob); err != nil {
		t.Fatal(err)
	}

	expect("a member can't list", 403, bob, "GET", "/integrations", nil, nil)
	var list []Integration
	expect("list", 200, ann, "GET", "/integrations", nil, &list)
	if len(list) != 2 || list[0].Connected || list[1].Provider != gitlab {
		t.Fatalf("list %+v", list)
	}
	expect("unknown provider", 404, ann, "POST", "/integrations/bitbucket", nil, nil)
	expect("settings before connect", 404, ann, "POST", "/integrations/github/settings", url.Values{"open_status": {statusID}}, nil)
	var conn struct {
		Integration Integration
		Secret      string
	}
	expect("connect", 200, ann, "POST", "/integrations/github", nil, &conn)
	hookURL := strings.TrimPrefix(*conn.Integration.WebhookURL, "https://taskwell.test")
	expect("bad status", 400, ann, "POST", "/integrations/github/settings", url.Values{"open_status": {"nope"}}, nil)
	var in Integration
	expect("settings", 200, ann, "POST", "/integrations/github/settings", url.Values{"open_status": {statusID}}, &in)
	if !strings.Contains(string(in.OpenStatus), "In progress") || string(in.MergeStatus) != "null" {
		t.Fatalf("settings %+v", in)
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from workspace_audit where workspace_id = $1 and action = 'integration.updated' and target = 'github'`, wsID).Scan(&n); err != nil || n != 1 {
		t.Errorf("settings audited %d times %v", n, err)
	}

	deliver := func(step string, want int, sig string, body string) string {
		t.Helper()
		r := httptest.NewRequest("POST", hookURL, strings.NewReader(body))
		r.Header.Set("X-GitHub-Event", "pull_request")
		if sig == "" {
			mac := hmac.New(sha256.New, []byte(conn.Secret))
			mac.Write([]byte(body))
			sig = "sha256=" + hex.EncodeToString(mac.Sum(nil))
		}
		r.Header.Set("X-Hub-Signature-256", sig)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: got %d %s, want %d", step, w.Code, w.Body, want)
		}
		return w.Body.String()
	}
	payload := func(action, state, branch, title string, merged bool) string {
		return fmt.Sprintf(`{"action":%q,"pull_request":{"number":12,"html_url":"https://github.com/acme/web/pull/12","title":%q,
			"body":"Refs WEB-2","state":%q,"merged":%t,"draft":false,"head":{"ref":%q},"user":{"login":"ann"}},
			"repository":{"full_name":"acme/web"}}`, action, title, state, merged, branch)
	}
	type state struct {
		status, actor string
		prs, events   int
	}
	look := func(n int) state {
		t.Helper()
		var s state
		if err := pool.QueryRow(ctx,
			`select st.name, (select count(*) from issue_pull_requests where issue_id = i.id),
			        (select count(*) from issue_events where issue_id = i.id and kind in ('status', 'pr_linked', 'pr_unlinked')),
			        coalesce((select actor_id::text from issue_events where issue_id = i.id and kind = 'status' order by id desc limit 1), 'none')
			 from issues i join statuses st on st.id = i.status_id where i.workspace_id = $1 and i.number = $2`, wsID, n).
			Scan(&s.status, &s.prs, &s.events, &s.actor); err != nil {
			t.Fatal(err)
		}
		return s
	}
	todo := look(1).status

	deliver("bad signature", 401, "sha256=00", payload("opened", "open", "web-1-login", "Login", false))
	deliver("missing signature", 401, "-", payload("opened", "open", "web-1-login", "Login", false))
	deliver("unknown action", 200, "", `{"action":"labeled"}`)

	opened := payload("opened", "open", "web-1-login", "Login", false)
	deliver("opened", 200, "", opened)
	if s := look(1); s.status != "In progress" || s.prs != 1 || s.events != 2 {
		t.Fatalf("after open WEB-1 %+v", s)
	}
	if s := look(2); s.status != todo || s.prs != 1 || s.events != 1 {
		t.Fatalf("a body reference moved WEB-2: %+v", s)
	}
	deliver("redelivered", 200, "", opened)
	if s := look(1); s.events != 2 {
		t.Fatalf("redelivery logged events: %+v", s)
	}
	var detail struct {
		PullRequests []struct {
			Repo   string
			Number int
			State  string
			Closes bool
		} `json:"pull_requests"`
	}
	expect("issue json", 200, ann, "GET", "/issues/WEB-1", nil, &detail)
	if len(detail.PullRequests) != 1 || detail.PullRequests[0].Repo != "acme/web" || detail.PullRequests[0].State != "open" || !detail.PullRequests[0].Closes {
		t.Fatalf("pull_requests %+v", detail.PullRequests)
	}

	merged := payload("closed", "closed", "web-1-login", "Login", true)
	deliver("merged", 200, "", merged)
	if s := look(1); s.status != "Done" || s.actor != "none" && s.actor != "" || s.events != 3 {
		t.Fatalf("after merge WEB-1 %+v", s)
	}
	var via string
	if err := pool.QueryRow(ctx, `select coalesce(e.data #>> '{via,repo}', '') || '#' || coalesce(e.data #>> '{via,number}', '') ||
		' ' || coalesce(e.actor_id::text, 'null')
		from issue_events e join issues i on i.id = e.issue_id where i.workspace_id = $1 and i.number = 1 and e.kind = 'status'
		order by e.id desc limit 1`, wsID).Scan(&via); err != nil || via != "acme/web#12 null" {
		t.Fatalf("status event via %q %v", via, err)
	}
	var notes int
	if err := pool.QueryRow(ctx, `select count(*) from notifications where user_id = $1 and kind = 'status' and actor_id is null`, bob).
		Scan(&notes); err != nil || notes == 0 {
		t.Fatalf("watcher notifications %d %v", notes, err)
	}
	if s := look(2); s.status != todo {
		t.Fatalf("merge moved a body reference: %+v", s)
	}
	deliver("merged again", 200, "", merged)
	if s := look(1); s.events != 3 {
		t.Fatalf("merge redelivery logged events: %+v", s)
	}

	deliver("mention dropped", 200, "", payload("edited", "closed", "other", "Login", true))
	if s := look(1); s.prs != 0 || s.events != 4 || s.status != "Done" {
		t.Fatalf("after unlink WEB-1 %+v", s)
	}

	r := httptest.NewRequest("POST", hookURL, strings.NewReader(strings.Repeat("x", maxDelivery+1)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized: %d", w.Code)
	}
	expect("regenerate", 200, ann, "POST", "/integrations/github", nil, nil)
	deliver("old url", 404, "", opened)
	expect("disconnect", 204, ann, "DELETE", "/integrations/github", nil, nil)
	expect("disconnect again", 404, ann, "DELETE", "/integrations/github", nil, nil)
}
