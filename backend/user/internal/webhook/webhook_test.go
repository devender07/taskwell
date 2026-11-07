package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/secret"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

func TestValidate(t *testing.T) {
	resolve := func(_ context.Context, host string) ([]netip.Addr, error) {
		switch host {
		case "internal.example":
			return []netip.Addr{netip.MustParseAddr("10.0.0.5")}, nil
		case "hooks.example":
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return nil, errors.New("no such host")
	}
	for _, c := range []struct {
		raw              string
		allowHTTP, allow bool
		msg              string
	}{
		{"  ", false, false, "Enter a URL."},
		{"not a url", false, false, "Enter a valid URL."},
		{"ftp://hooks.example", false, false, "Enter a valid URL."},
		{"https://user:pw@hooks.example", false, false, "Enter a valid URL."},
		{"https://" + strings.Repeat("a", 2000) + ".example", false, false, "Enter a valid URL."},
		{"http://hooks.example", false, false, "Use an https URL."},
		{"http://hooks.example", true, false, ""},
		{"https://localhost:3000/x", false, false, msgPrivate},
		{"https://127.0.0.1/x", false, false, msgPrivate},
		{"https://[::1]/x", false, false, msgPrivate},
		{"https://169.254.169.254/latest", false, false, msgPrivate},
		{"https://internal.example/hook", false, false, msgPrivate},
		{"https://internal.example/hook", false, true, ""},
		{" https://hooks.example/hook?x=1 ", false, false, ""},
		{"https://unresolvable.example/hook", false, false, ""},
	} {
		if _, msg := checkURL(context.Background(), resolve, c.raw, c.allowHTTP, c.allow); msg != c.msg {
			t.Errorf("checkURL(%q) = %q, want %q", c.raw, msg, c.msg)
		}
	}
	for _, c := range []struct {
		in   []string
		want string
		msg  string
	}{
		{nil, "", "Choose at least one event."},
		{[]string{" "}, "", "Choose at least one event."},
		{[]string{"issue.created", "ping"}, "", "Choose events from the list."},
		{[]string{"comment.created", "issue.created", "issue.created"}, "issue.created,comment.created", ""},
	} {
		got, msg := checkEvents(c.in)
		if msg != c.msg || strings.Join(got, ",") != c.want {
			t.Errorf("checkEvents(%v) = %v %q, want %s %q", c.in, got, msg, c.want, c.msg)
		}
	}
}

type fixture struct {
	pool                *pgxpool.Pool
	h                   *Handler
	mux                 *http.ServeMux
	ann, bob            string
	wsID, slug          string
	webIssue, leadIssue string
}

// setup makes Ann's workspace with a public WEB-1 and a private LEAD-1, and Bob, a member without
// integrations.manage.
func setup(t *testing.T) *fixture {
	t.Helper()
	pool := dbtest.Pool(t)
	ctx := context.Background()
	f := &fixture{pool: pool, ann: dbtest.User(t, pool, "Ann"), bob: dbtest.User(t, pool, "Bob")}
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateFirst(ctx, tx, f.ann, "Ann", ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id, slug from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key, private)
			      select id, n, k, pr from w, (values ('Web', 'WEB', false), ('Leadership', 'LEAD', true)) v(n, k, pr) returning id, key),
			s as (select id from statuses where workspace_id = (select id from w) order by position limit 1),
			i as (insert into issues (workspace_id, project_id, number, title, status_id, rank)
			      select (select id from w), p.id, 1, p.key || ' issue', (select id from s), 'V' from p returning id, title)
			select (select id::text from w), (select slug from w),
			       (select id::text from i where title = 'WEB issue'), (select id::text from i where title = 'LEAD issue')`, f.ann).
			Scan(&f.wsID, &f.slug, &f.webIssue, &f.leadIssue)
	}); err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, f.wsID, f.bob, "member")
	f.h = New(pool, Options{Key: secret.Key("test", "key"), AllowHTTP: true, AllowPrivate: true}, dbtest.Auth,
		workspace.New(pool, dbtest.Auth, nil, nil).Member)
	f.mux = http.NewServeMux()
	f.h.Register(f.mux)
	return f
}

func (f *fixture) do(t *testing.T, user, method, path string, form url.Values, want int, into any) string {
	t.Helper()
	code, body := dbtest.Do(t, f.mux, user, method, "/api/workspaces/"+f.slug+"/webhooks"+path, form)
	if code != want {
		t.Fatalf("%s %s as %s: %d %s, want %d", method, path, user, code, body, want)
	}
	if into != nil {
		if err := json.Unmarshal([]byte(body), into); err != nil {
			t.Fatalf("%s %s: %v in %s", method, path, err, body)
		}
	}
	return strings.TrimSpace(body)
}

func (f *fixture) create(t *testing.T, rawURL string, events ...string) (Webhook, string) {
	t.Helper()
	var out struct {
		Webhook Webhook `json:"webhook"`
		Secret  string  `json:"secret"`
	}
	f.do(t, f.ann, "POST", "", url.Values{"url": {rawURL}, "events": events}, 201, &out)
	return out.Webhook, out.Secret
}

// TestAPI: validation, permission, CRUD, the secret shown once and rolled, the 10-webhook limit and 404s.
func TestAPI(t *testing.T) {
	f := setup(t)
	f.do(t, f.bob, "GET", "", nil, 403, nil)
	f.do(t, f.bob, "POST", "", url.Values{"url": {"https://x.example"}, "events": {"issue.created"}}, 403, nil)
	if got := f.do(t, f.ann, "GET", "", nil, 200, nil); got != "[]" {
		t.Fatalf("empty list = %s", got)
	}
	got := f.do(t, f.ann, "POST", "", url.Values{"url": {"ftp://x"}, "events": {"nope"}}, 400, nil)
	if got != `{"errors":{"events":"Choose events from the list.","url":"Enter a valid URL."}}` {
		t.Fatalf("validation = %s", got)
	}

	wh, plain := f.create(t, "https://hooks.example/a", "issue.updated", "issue.created")
	if !strings.HasPrefix(plain, "whsec_") || len(plain) != 6+43 || !wh.Active || wh.LastDelivery != nil ||
		strings.Join(wh.Events, ",") != "issue.created,issue.updated" {
		t.Fatalf("created %+v %q", wh, plain)
	}
	if body := f.do(t, f.ann, "GET", "", nil, 200, nil); strings.Contains(body, plain) || strings.Contains(body, "secret") {
		t.Fatalf("list shows the secret: %s", body)
	}
	var sealed string
	if err := f.pool.QueryRow(context.Background(), `select secret from webhooks where id = $1`, wh.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if open, err := secret.Open(f.h.opts.Key, sealAD(f.wsID, wh.ID), sealed); err != nil || open != plain {
		t.Fatalf("sealed secret opens to %q, %v", open, err)
	}

	var up Webhook
	f.do(t, f.ann, "POST", "/"+wh.ID, url.Values{"active": {"false"}}, 200, &up)
	if up.Active || up.URL != "https://hooks.example/a" || len(up.Events) != 2 {
		t.Fatalf("deactivated %+v", up)
	}
	f.do(t, f.ann, "POST", "/"+wh.ID, url.Values{"url": {" https://hooks.example/b "}, "events": {"comment.created"}}, 200, &up)
	if up.Active || up.URL != "https://hooks.example/b" || strings.Join(up.Events, ",") != "comment.created" {
		t.Fatalf("updated %+v", up)
	}
	f.do(t, f.ann, "POST", "/"+wh.ID, url.Values{"events": {""}}, 400, nil)
	f.do(t, f.bob, "POST", "/"+wh.ID, url.Values{"active": {"true"}}, 403, nil)

	var rolled struct{ Secret string }
	f.do(t, f.ann, "POST", "/"+wh.ID+"/secret", nil, 200, &rolled)
	if rolled.Secret == plain || !strings.HasPrefix(rolled.Secret, "whsec_") {
		t.Fatalf("rolled secret %q", rolled.Secret)
	}

	missing := "/00000000-0000-7000-8000-000000000000"
	for _, c := range []struct{ method, path, msg string }{
		{"POST", missing, errNoWebhook},
		{"POST", "/nope", errNoWebhook},
		{"POST", missing + "/secret", errNoWebhook},
		{"POST", missing + "/test", errNoWebhook},
		{"DELETE", missing, errNoWebhook},
		{"GET", missing + "/deliveries", errNoWebhook},
		{"POST", "/" + wh.ID + "/deliveries" + missing + "/redeliver", errNoDelivery},
	} {
		if got := f.do(t, f.ann, c.method, c.path, url.Values{"active": {"true"}}, 404, nil); got != `{"error":"`+c.msg+`"}` {
			t.Errorf("%s %s = %s", c.method, c.path, got)
		}
	}

	for range maxWebhooks - 1 {
		f.create(t, "https://hooks.example/more", "issue.created")
	}
	if got := f.do(t, f.ann, "POST", "", url.Values{"url": {"https://hooks.example/c"}, "events": {"issue.created"}}, 409, nil); got != `{"error":"`+errTooMany+`"}` {
		t.Fatalf("over the limit = %s", got)
	}
	var list []Webhook
	f.do(t, f.ann, "GET", "", nil, 200, &list)
	if len(list) != maxWebhooks || list[0].ID != wh.ID {
		t.Fatalf("list has %d, first %s", len(list), list[0].ID)
	}
	f.do(t, f.ann, "DELETE", "/"+wh.ID, nil, 204, nil)
	f.do(t, f.ann, "GET", "/"+wh.ID+"/deliveries", nil, 404, nil)

	var audit string
	if err := f.pool.QueryRow(context.Background(), `select string_agg(action || ' ' || target, ', ' order by id) from workspace_audit
		where workspace_id = $1 and target = 'https://hooks.example' and action like 'webhook.%' and actor_id = $2`,
		f.wsID, f.ann).Scan(&audit); err != nil ||
		!strings.HasPrefix(audit, "webhook.created https://hooks.example, webhook.secret_rolled https://hooks.example, ") ||
		!strings.HasSuffix(audit, ", webhook.deleted https://hooks.example") { // the limit check's webhooks come between
		t.Errorf("audit log %q %v", audit, err)
	}
}

type queued struct {
	event   string
	payload map[string]any
}

// deliveries returns the webhook's deliveries, oldest first.
func (f *fixture) deliveries(t *testing.T, webhookID string) []queued {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `select event, payload from webhook_deliveries where webhook_id = $1 order by id`, webhookID)
	if err != nil {
		t.Fatal(err)
	}
	qs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (queued, error) {
		var q queued
		err := row.Scan(&q.event, &q.payload)
		return q, err
	})
	if err != nil {
		t.Fatal(err)
	}
	return qs
}

// TestTriggers: each event is queued with its payload for subscribed active webhooks, never for private projects.
func TestTriggers(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := f.create(t, "https://hooks.example/all", Events...)
	created, _ := f.create(t, "https://hooks.example/created", "issue.created")
	off, _ := f.create(t, "https://hooks.example/off", Events...)
	f.do(t, f.ann, "POST", "/"+off.ID, url.Values{"active": {"false"}}, 200, nil)

	exec(`insert into issue_events (issue_id, actor_id, kind) values ($1, $2, 'created'), ($3, $2, 'created')`, f.webIssue, f.ann, f.leadIssue)
	exec(`insert into issue_events (issue_id, actor_id, kind, data) values ($1, $2, 'priority', '{"from": 0, "to": 2}')`, f.webIssue, f.ann)
	exec(`insert into issue_events (issue_id, kind, data) values ($1, 'priority', '{}')`, f.leadIssue)
	var commentID string
	if err := f.pool.QueryRow(ctx, `insert into comments (issue_id, author_id, body) values ($1, $2, '{}') returning id::text`,
		f.webIssue, f.bob).Scan(&commentID); err != nil {
		t.Fatal(err)
	}
	exec(`insert into comments (issue_id, author_id, body) values ($1, $2, '{}')`, f.leadIssue, f.bob)
	exec(`delete from issues where id = any($1::uuid[])`, []string{f.webIssue, f.leadIssue})

	got := f.deliveries(t, all.ID)
	want := []string{"issue.created", "issue.updated", "comment.created", "issue.deleted"}
	if len(got) != len(want) {
		t.Fatalf("all got %d deliveries: %v", len(got), got)
	}
	for i, q := range got {
		p := q.payload
		issue, _ := p["issue"].(map[string]any)
		if q.event != want[i] || p["type"] != want[i] || p["workspace"] != f.slug || issue["identifier"] != "WEB-1" ||
			issue["id"] != f.webIssue || issue["title"] != "WEB issue" || p["id"] == nil {
			t.Errorf("delivery %d = %s %v", i, q.event, p)
		}
		if _, err := time.Parse(time.RFC3339, p["created_at"].(string)); err != nil {
			t.Errorf("created_at %v", p["created_at"])
		}
	}
	if a := got[0].payload["actor"].(map[string]any); a["id"] != f.ann || a["name"] != "Ann" {
		t.Errorf("actor %v", a)
	}
	if c := got[1].payload["change"].(map[string]any); c["kind"] != "priority" || c["data"].(map[string]any)["to"] != 2.0 {
		t.Errorf("change %v", c)
	}
	if c := got[2].payload["comment"].(map[string]any); c["id"] != commentID || got[2].payload["change"] != nil {
		t.Errorf("comment %v", got[2].payload)
	}
	if a, ok := got[3].payload["actor"]; !ok || a != nil {
		t.Errorf("deleted actor %v", a)
	}
	if got := f.deliveries(t, created.ID); len(got) != 1 || got[0].event != "issue.created" {
		t.Errorf("created-only got %v", got)
	}
	if got := f.deliveries(t, off.ID); len(got) != 0 {
		t.Errorf("inactive got %v", got)
	}
	f.do(t, f.ann, "POST", "/"+off.ID+"/test", nil, 202, nil)
	if got := f.deliveries(t, off.ID); len(got) != 1 || got[0].event != "ping" || got[0].payload["issue"] != nil ||
		got[0].payload["actor"].(map[string]any)["id"] != f.ann {
		t.Errorf("inactive ping %v", got)
	}
	// Deleting the workspace with queued deliveries and issues goes through.
	exec(`delete from workspaces where id = $1`, f.wsID)
}

// TestWorker: deliveries are signed and recorded, a failing receiver is retried on the schedule until failed,
// redeliver sends a copy, inactive webhooks wait, and private addresses are refused at dial time.
func TestWorker(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	type hit struct {
		header http.Header
		body   string
	}
	hits := make(chan hit, 20)
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		hits <- hit{r.Header, string(b)}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "nope\x00 \xff")
	}))
	defer srv.Close()

	wh, plain := f.create(t, srv.URL+"/hook", "issue.created")
	send := func(want bool) {
		t.Helper()
		sent, err := f.h.sendNext(ctx)
		if err != nil || sent != want {
			t.Fatalf("sendNext = %v, %v; want %v", sent, err, want)
		}
	}
	list := func() []Delivery {
		t.Helper()
		var ds []Delivery
		f.do(t, f.ann, "GET", "/"+wh.ID+"/deliveries", nil, 200, &ds)
		return ds
	}

	f.do(t, f.ann, "POST", "/"+wh.ID+"/test", nil, 202, nil)
	send(true)
	h := <-hits
	ds := list()
	if len(ds) != 1 || ds[0].Status != "delivered" || ds[0].Attempts != 1 || *ds[0].ResponseCode != 200 ||
		ds[0].ResponseError != nil || ds[0].DeliveredAt == nil || ds[0].Event != "ping" {
		t.Fatalf("delivered %+v", ds)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(h.body), &sent); err != nil || sent["id"] != ds[0].ID || sent["type"] != "ping" {
		t.Fatalf("body %s", h.body)
	}
	sig := h.header.Get("X-Taskwell-Signature")
	ts, _, _ := strings.Cut(strings.TrimPrefix(sig, "t="), ",")
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || time.Since(time.Unix(unix, 0)) > time.Minute {
		t.Fatalf("signature time %q", sig)
	}
	if Sign(plain, time.Unix(unix, 0), []byte(h.body)) != sig {
		t.Fatalf("signature %q doesn't verify", sig)
	}
	for k, v := range map[string]string{"Content-Type": "application/json", "User-Agent": "Taskwell-Webhooks/1",
		"X-Taskwell-Event": "ping", "X-Taskwell-Delivery": ds[0].ID} {
		if h.header.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, h.header.Get(k), v)
		}
	}
	var whs []Webhook
	f.do(t, f.ann, "GET", "", nil, 200, &whs)
	if whs[0].LastDelivery == nil || whs[0].LastDelivery.Status != "delivered" || *whs[0].LastDelivery.ResponseCode != 200 {
		t.Fatalf("last delivery %+v", whs[0].LastDelivery)
	}
	send(false)

	status = 500
	f.do(t, f.ann, "POST", "/"+wh.ID+"/deliveries/"+ds[0].ID+"/redeliver", nil, 202, nil)
	for i, wait := range append(retryWaits, 0) {
		send(true)
		<-hits
		d := list()[0]
		if d.Attempts != i+1 || *d.ResponseCode != 500 || d.ResponseError == nil || *d.ResponseError != "nope �" {
			t.Fatalf("attempt %d: %+v", i+1, d)
		}
		if wait == 0 {
			if d.Status != "failed" {
				t.Fatalf("after the last attempt: %s", d.Status)
			}
			break
		}
		if left := time.Until(d.NextAt); d.Status != "pending" || left < wait-time.Minute || left > wait {
			t.Fatalf("attempt %d: %s, next in %s, want %s", i+1, d.Status, left, wait)
		}
		send(false)
		if _, err := f.pool.Exec(ctx, `update webhook_deliveries set next_at = now() where webhook_id = $1`, wh.ID); err != nil {
			t.Fatal(err)
		}
	}
	var copied map[string]any
	if err := json.Unmarshal(list()[0].Payload, &copied); err != nil || copied["id"] != list()[0].ID || copied["type"] != "ping" {
		t.Fatalf("redelivered payload %v", copied)
	}

	f.do(t, f.ann, "POST", "/"+wh.ID, url.Values{"active": {"false"}}, 200, nil)
	f.do(t, f.ann, "POST", "/"+wh.ID+"/test", nil, 202, nil)
	send(false)
	status = 204
	f.do(t, f.ann, "POST", "/"+wh.ID, url.Values{"active": {"true"}}, 200, nil)
	send(true)
	<-hits
	if d := list()[0]; d.Status != "delivered" || *d.ResponseCode != 204 {
		t.Fatalf("resumed %+v", d)
	}

	f.h.client = newClient(false)
	f.do(t, f.ann, "POST", "/"+wh.ID+"/test", nil, 202, nil)
	send(true)
	if d := list()[0]; d.Status != "pending" || d.ResponseCode != nil || !strings.Contains(*d.ResponseError, "private address") {
		t.Fatalf("private dial %+v", d)
	}
	if len(hits) != 0 {
		t.Fatal("the private address was reached")
	}
}
