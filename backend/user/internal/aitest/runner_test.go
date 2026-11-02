package aitest

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/dbtest"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

// testIssue makes a user with a workspace holding issue ENG-1 and returns the user, workspace and issue ids.
func testIssue(t *testing.T, pool *pgxpool.Pool) (userID, wsID, issueID string) {
	t.Helper()
	ctx := context.Background()
	userID = dbtest.User(t, pool, "Ann")
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := workspace.CreateDefault(ctx, tx, userID, "Ann"); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `with w as (select id from workspaces where created_by = $1),
			p as (insert into projects (workspace_id, name, key) select id, 'Engineering', 'ENG' from w returning id),
			s as (select id from statuses where workspace_id = (select id from w) order by position limit 1),
			i as (insert into issues (workspace_id, project_id, number, title, status_id)
			      select (select id from w), p.id, 1, 'Sign in works', (select id from s) from p returning id)
			select (select id::text from w), (select id::text from i)`, userID).Scan(&wsID, &issueID)
	}); err != nil {
		t.Fatal(err)
	}
	return userID, wsID, issueID
}

// TestClaim checks the per-workspace cap on running AI runs and the takeover of a run a crashed tester left.
func TestClaim(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, wsID, issueID := testIssue(t, pool)
	m := &model{db: pool}
	var stale string
	if err := pool.QueryRow(ctx, `insert into test_runs (workspace_id, issue_id, kind, started_by, status, claimed_at)
		values ($1, $2, 'ai', $3, 'running', now() - interval '20 minutes') returning id::text`, wsID, issueID, ann).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	ours := map[string]bool{}
	for range 3 {
		var id string
		if err := pool.QueryRow(ctx, `insert into test_runs (workspace_id, issue_id, kind, started_by) values ($1, $2, 'ai', $3)
			returning id::text`, wsID, issueID, ann).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ours[id] = true
	}
	claimAll := func() int {
		n := 0
		for {
			id, err := m.claim(ctx, 2, "")
			if err != nil {
				t.Fatal(err)
			}
			if id == "" {
				return n
			}
			if ours[id] {
				n++
			}
		}
	}
	if n := claimAll(); n != 1 { // the stale run still counts as running
		t.Fatalf("claimed %d with the stale run running, want 1", n)
	}
	if err := m.failStale(ctx); err != nil {
		t.Fatal(err)
	}
	var status, msg string
	if err := pool.QueryRow(ctx, `select status, error from test_runs where id = $1`, stale).Scan(&status, &msg); err != nil {
		t.Fatal(err)
	}
	if status != "error" || msg != msgStale {
		t.Fatalf("stale run: %s %q", status, msg)
	}
	if n := claimAll(); n != 1 {
		t.Fatalf("claimed %d more, want 1 (cap 2)", n)
	}
	var generating int
	if err := pool.QueryRow(ctx, `select count(*) from test_runs where issue_id = $1 and status = 'generating'`, issueID).Scan(&generating); err != nil {
		t.Fatal(err)
	}
	if generating != 2 {
		t.Fatalf("%d generating, want 2 (no saved cases)", generating)
	}
}

// TestRecordedRuns saves a script through the API, exports it, and starts a replay run with the AI limit at 0: the
// run is queued and claimed straight into running, and it blocks a second run on the issue.
func TestRecordedRuns(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, wsID, issueID := testIssue(t, pool)
	m := &model{db: pool}
	var slug, caseID string
	if err := pool.QueryRow(ctx, `select slug from workspaces where id = $1`, wsID).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	id, err := m.createCase(ctx, issueID, ann, caseFields{Title: "Sign in", Steps: []string{"Sign in"}, Expected: "Home"})
	if err != nil {
		t.Fatal(err)
	}
	caseID = id
	if _, err := m.createSite(ctx, wsID, slug, ann, "https://app.acme.test", "app.acme.test"); err != nil {
		t.Fatal(err)
	}
	var limit string
	if err := pool.QueryRow(ctx, `select value from system_config where key = 'ai_test_monthly_limit_usd'`).Scan(&limit); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update system_config set value = '0' where key = 'ai_test_monthly_limit_usd'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `update system_config set value = $1 where key = 'ai_test_monthly_limit_usd'`, limit)
	})
	if err := m.heartbeat(ctx, "http://tester.test"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	New(pool, sysconfig.New(pool), Options{}, dbtest.Auth, workspace.New(pool, dbtest.Auth, nil, nil).Member).Register(mux)
	base := "/api/workspaces/" + slug
	do := func(method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, ann, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, code, body, want)
		}
		return body
	}
	replay := url.Values{"kind": {"replay"}}
	if body := do("POST", "/issues/ENG-1/test-runs", replay, 409); !strings.Contains(body, "Record a test case first.") {
		t.Errorf("unrecorded: %s", body)
	}
	if body := do("POST", "/test-cases/"+caseID+"/script", url.Values{"script": {"{"}}, 400); !strings.Contains(body, "Send the script as JSON.") {
		t.Errorf("bad json: %s", body)
	}
	if body := do("POST", "/test-cases/"+caseID+"/script", url.Values{"script": {`{"steps":[{"tool":"navigate","url":"https://evil.test"}],"check":"x"}`}}, 400); !strings.Contains(body, "Step 1: https://evil.test isn't a registered test site.") {
		t.Errorf("other host: %s", body)
	}
	sc := `{"steps":[{"tool":"navigate","url":"https://app.acme.test/login"},{"tool":"assert_no_text","text":"Error"}],"check":"Home"}`
	if body := do("POST", "/test-cases/"+caseID+"/script", url.Values{"script": {sc}, "playwright": {"await page.locator('#password').fill('x');"}}, 400); !strings.Contains(body, "Use process.env for passwords") {
		t.Errorf("literal password: %s", body)
	}
	body := do("POST", "/test-cases/"+caseID+"/script", url.Values{"script": {sc}, "playwright": {"await page.goto('https://app.acme.test');"}}, 200)
	if !strings.Contains(body, `"recorded":true`) || !strings.Contains(body, `"has_playwright":true`) {
		t.Errorf("saved case: %s", body)
	}
	body = do("POST", "/test-cases/"+caseID+"/script", url.Values{"script": {sc}}, 200) // no playwright field keeps the code
	if !strings.Contains(body, `"has_playwright":true`) {
		t.Errorf("code dropped: %s", body)
	}
	code, spec := dbtest.Do(t, mux, ann, "GET", base+"/issues/ENG-1/playwright", nil)
	if code != 200 || !strings.Contains(spec, `test("Sign in", async ({ page }) => {`+"\nawait page.goto('https://app.acme.test');") {
		t.Errorf("export %d:\n%s", code, spec)
	}

	if body := do("POST", "/issues/ENG-1/test-runs", url.Values{"kind": {"ai"}}, 409); !strings.Contains(body, msgLimit) {
		t.Errorf("ai run at limit 0: %s", body)
	}
	var run Run
	if err := json.Unmarshal([]byte(do("POST", "/issues/ENG-1/test-runs", replay, 201)), &run); err != nil {
		t.Fatal(err)
	}
	if run.Kind != "replay" || run.Status != "queued" || run.CostUSD != nil {
		t.Errorf("replay run %+v", run)
	}
	if body := do("POST", "/issues/ENG-1/test-runs", replay, 409); !strings.Contains(body, "A test is already running on this issue.") {
		t.Errorf("second run: %s", body)
	}
	for {
		id, err := m.claim(ctx, 2, "")
		if err != nil {
			t.Fatal(err)
		}
		if id == run.ID {
			break
		}
		if id == "" {
			t.Fatal("replay run not claimed")
		}
	}
	j, err := m.job(ctx, run.ID)
	if err != nil || j.kind != "replay" || j.status != "running" {
		t.Fatalf("claimed job %+v %v", j, err)
	}
	if body := do("POST", "/test-runs/"+run.ID+"/cancel", url.Values{}, 200); !strings.Contains(body, `"kind":"replay"`) {
		t.Errorf("cancel: %s", body)
	}
	do("DELETE", "/test-cases/"+caseID+"/script", nil, 204)
	if c, err := m.testCase(ctx, wsID, caseID); err != nil || c.Recorded || c.Playwright {
		t.Errorf("cleared case %+v %v", c, err)
	}
}

// TestRunner runs an AI run end to end against a local site in real Chrome with a scripted Claude: generated
// cases, a sign-in that passes with the sealed password typed by the tester (and is recorded), a redirect to the
// metadata address that's blocked and fails, then a run of just the sign-in case that replays it without Claude. Needs CHROME_URL (e.g. google-chrome --headless=new --remote-debugging-port=9333),
// or CHROME_URL=http://127.0.0.1:9222 CHROME_IN_DOCKER=1 with the dev compose chrome.
func TestRunner(t *testing.T) {
	chromeURL, proxy, proxyURL := testBrowser(t)
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, wsID, issueID := testIssue(t, pool)
	m := &model{db: pool}
	key, _ := Key("", false)

	const password = "s3cret-pw"
	var mu sync.Mutex
	var gotPassword string
	var views int // the button's name counts sign-in page views, so a replay sees a different number than the recording
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			if r.Method == http.MethodPost {
				mu.Lock()
				gotPassword = r.FormValue("password")
				mu.Unlock()
				if r.FormValue("password") == password {
					http.Redirect(w, r, "/home", http.StatusSeeOther)
					return
				}
			}
			mu.Lock()
			views++
			n := views
			mu.Unlock()
			fmt.Fprintf(w, `<title>Sign in</title><form method=post action=/login><input name=email aria-label=Email>
				<input name=password type=password aria-label=Password><button>Sign in %d</button></form>`, n)
		case "/home":
			fmt.Fprint(w, `<title>Home</title><p>Welcome Ann</p>`)
		case "/evil":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
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

	tool := func(name string, input any) *anthropic.BetaMessage {
		in, _ := json.Marshal(input)
		return message(t, fmt.Sprintf(`{"type":"tool_use","id":"tu_%d","name":%q,"input":%s}`, time.Now().UnixNano(), name, in), "tool_use")
	}
	var calls int
	var cacheMarks []int
	send := func(_ context.Context, p anthropic.BetaMessageNewParams) (*anthropic.BetaMessage, error) {
		body, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		if strings.Contains(string(body), password) {
			t.Error("the password reached Claude")
		}
		mu.Lock()
		calls++
		mu.Unlock()
		if p.OutputConfig.Format.Schema != nil {
			if !strings.Contains(string(body), "Admin") || !strings.Contains(string(body), site.URL) {
				t.Error("generation input lacks the account label or site")
			}
			cases, _ := json.Marshal(map[string]any{"cases": []caseFields{
				{Title: "Sign in", Steps: []string{"Open /login", "Sign in as Admin"}, Expected: "Home says Welcome Ann"},
				{Title: "Evil link", Steps: []string{"Open /evil"}, Expected: "Stays on the site"},
			}})
			text, _ := json.Marshal(string(cases))
			return message(t, `{"type":"text","text":`+string(text)+`}`, "end_turn"), nil
		}
		step := (len(p.Messages) - 1) / 2
		if step > 0 {
			cacheMarks = append(cacheMarks, strings.Count(string(body), `"cache_control"`))
		}
		intro := p.Messages[0].OfUser.Content[0].OfText.Text
		last := ""
		if tr := p.Messages[len(p.Messages)-1].OfUser.Content[0].OfToolResult; tr != nil {
			last = tr.Content[0].OfText.Text
		}
		if strings.HasPrefix(intro, "Test case: Sign in") {
			switch step {
			case 0:
				return tool("navigate", map[string]string{"url": site.URL + "/login"}), nil
			case 1:
				return tool("type", map[string]any{"ref": 1, "text": "ann@acme.test"}), nil
			case 2:
				return tool("fill_password", map[string]any{"ref": 2, "account": "Admin"}), nil
			case 3:
				return tool("click", map[string]any{"ref": 3}), nil
			}
			result := "fail"
			if strings.Contains(last, "Welcome Ann") {
				result = "pass"
			}
			return tool("finish", map[string]any{"result": result, "summary": "Signed in.", "reproduction": []string{}, "check": "Welcome Ann"}), nil
		}
		if step == 0 {
			return tool("navigate", map[string]string{"url": site.URL + "/evil"}), nil
		}
		return tool("finish", map[string]any{"result": "fail", "summary": "The link left the site.", "reproduction": []string{"Open /evil"}}), nil
	}

	var runID string
	if err := pool.QueryRow(ctx, `insert into test_runs (workspace_id, issue_id, kind, started_by, proxy_token) values ($1, $2, 'ai', $3, 'tok')
		returning id::text`, wsID, issueID, ann).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(pool, sysconfig.New(pool), RunnerOptions{Send: send, Key: key, UploadDir: t.TempDir(), ChromeURL: chromeURL,
		ProxyURL: proxyURL, Browsers: 1, Proxy: proxy})
	for {
		id, err := m.claim(ctx, 2, "")
		if err != nil {
			t.Fatal(err)
		}
		if id == runID {
			break
		}
		if id == "" {
			t.Fatal("run not claimed")
		}
	}
	r.execute(ctx, runID)

	d, err := m.run(ctx, wsID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "failed" || len(d.Results) != 2 {
		t.Fatalf("run %s %v: %+v", d.Status, d.Error, d.Results)
	}
	signIn, evil := d.Results[0], d.Results[1]
	if signIn.Status != "passed" || evil.Status != "failed" || len(evil.Reproduction) != 1 {
		t.Fatalf("results %+v / %+v", signIn, evil)
	}
	if gotPassword != password {
		t.Errorf("site got password %q", gotPassword)
	}
	var blocked bool
	for _, l := range evil.Log {
		blocked = blocked || l.Action == "Blocked: 169.254.169.254 isn't a registered test site"
	}
	if !blocked {
		t.Errorf("no blocked step in %+v", evil.Log)
	}
	if len(signIn.Log) != 4 || signIn.Log[0].ScreenshotURL == nil || d.LatestScreenshotURL == nil ||
		!strings.Contains(signIn.Log[2].Action, `fill the password of "Admin" into [2] input type=password`) {
		t.Errorf("sign-in log %+v", signIn.Log)
	}
	for _, l := range signIn.Log {
		if strings.Contains(l.Action, "ann@acme.test") {
			t.Errorf("typed text in the log: %s", l.Action)
		}
	}
	for i, n := range cacheMarks {
		if n != 2 {
			t.Errorf("request %d has %d cache breakpoints, want 2 (system, last tool result)", i, n)
		}
	}
	if d.CostUSD == nil || *d.CostUSD <= 0 {
		t.Errorf("cost %v", d.CostUSD)
	}
	cs, err := m.cases(ctx, issueID)
	if err != nil || len(cs) != 2 || cs[0].Source != "ai" || cs[0].LastResult == nil || cs[0].LastResult.Status != "passed" {
		t.Fatalf("saved cases %+v %v", cs, err)
	}
	var usageRows int
	if err := pool.QueryRow(ctx, `select count(*) from ai_usage where run_id = $1`, runID).Scan(&usageRows); err != nil || usageRows != calls {
		t.Errorf("%d usage rows for %d calls (%v)", usageRows, calls, err)
	}
	u, _ := url.Parse(*d.LatestScreenshotURL)
	if !strings.HasPrefix(u.Path, "/api/files/") {
		t.Errorf("screenshot url %s", u)
	}
	if !cs[0].Recorded || cs[1].Recorded {
		t.Fatalf("recorded: sign-in %v, evil %v; want only the passed case", cs[0].Recorded, cs[1].Recorded)
	}

	callsBefore := calls
	gotPassword = ""
	var replayID string
	if err := pool.QueryRow(ctx, `insert into test_runs (workspace_id, issue_id, kind, started_by, proxy_token, case_id)
		values ($1, $2, 'ai', $3, 'tok2', $4) returning id::text`, wsID, issueID, ann, cs[0].ID).Scan(&replayID); err != nil {
		t.Fatal(err)
	}
	if id, err := m.claim(ctx, 2, ""); err != nil || id != replayID {
		t.Fatalf("claim replay run: %q %v", id, err)
	}
	r.execute(ctx, replayID)
	d, err = m.run(ctx, wsID, replayID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "passed" || len(d.Results) != 1 || calls != callsBefore {
		t.Fatalf("replay run %s with %d results and %d Claude calls: %+v", d.Status, len(d.Results), calls-callsBefore, d.Results)
	}
	if gotPassword != password || !strings.HasPrefix(d.Results[0].Log[0].Action, "replay navigate") {
		t.Errorf("replay typed %q, log %+v", gotPassword, d.Results[0].Log)
	}

	// Replay runs never call Claude: the recorded case passes with its assertion, then fails on a negative one with
	// the reason, the steps so far and a screenshot of the page; the unrecorded case is skipped.
	sc, err := m.script(ctx, cs[0].ID)
	if err != nil || sc == nil {
		t.Fatalf("script %v %v", sc, err)
	}
	if last := sc.Steps[len(sc.Steps)-1]; last.Target == nil || last.Target.Name != "Sign in {n}" {
		t.Errorf("recorded the count: %+v", last.Target)
	}
	for _, c := range []struct{ tool, status string }{{"assert_text", "passed"}, {"assert_no_text", "failed"}} {
		withAssert := *sc
		withAssert.Steps = append(slices.Clone(sc.Steps), scriptStep{Tool: c.tool, Text: "Welcome Ann"})
		if err := m.setScript(ctx, wsID, cs[0].ID, ann, &withAssert, false, ""); err != nil {
			t.Fatal(err)
		}
		id, err := m.createReplay(ctx, wsID, issueID, ann)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := m.claim(ctx, 2, ""); err != nil || got != id {
			t.Fatalf("claim replay run: %q %v", got, err)
		}
		r.execute(ctx, id)
		d, err := m.run(ctx, wsID, id)
		if err != nil {
			t.Fatal(err)
		}
		if d.Kind != "replay" || d.Status != c.status || calls != callsBefore || d.CostUSD != nil || len(d.Results) != 2 {
			t.Fatalf("%s run %s, %d Claude calls: %+v", c.tool, d.Status, calls-callsBefore, d.Results)
		}
		x, skipped := d.Results[0], d.Results[1]
		if skipped.Status != "skipped" || skipped.Summary == nil || *skipped.Summary != msgNotRecorded {
			t.Errorf("unrecorded case %+v", skipped)
		}
		if c.status == "failed" {
			last := x.Log[len(x.Log)-1]
			if x.Summary == nil || *x.Summary != `Expected "Welcome Ann" not to be on the page.` ||
				len(x.Reproduction) != len(withAssert.Steps) || x.Reproduction[len(x.Reproduction)-1] != `check "Welcome Ann" isn't on the page` ||
				last.Action != *x.Summary || last.ScreenshotURL == nil {
				t.Errorf("failed result %+v", x)
			}
		}
	}
}

// testBrowser returns CHROME_URL and an egress proxy (private sites allowed: test sites are on 127.0.0.1) with the
// URL Chrome reaches it at, or skips the test without a reachable Chrome.
func testBrowser(t *testing.T) (chromeURL string, proxy *Proxy, proxyURL string) {
	t.Helper()
	chromeURL = os.Getenv("CHROME_URL")
	if chromeURL == "" {
		t.Skip("CHROME_URL not set")
	}
	if resp, err := http.Get(chromeURL + "/json/version"); err != nil {
		t.Skipf("chrome not reachable: %v", err)
	} else {
		resp.Body.Close()
	}
	proxy = NewProxy(true)
	psrv := httptest.NewUnstartedServer(proxy)
	docker := os.Getenv("CHROME_IN_DOCKER") != "" // the dev compose chrome: it reaches the proxy on the host
	if docker {
		l, err := net.Listen("tcp", "0.0.0.0:0")
		if err != nil {
			t.Fatal(err)
		}
		psrv.Listener = l
	}
	psrv.Start()
	t.Cleanup(psrv.Close)
	proxyURL = psrv.URL
	if docker {
		proxyURL = "http://host.docker.internal:" + strconv.Itoa(psrv.Listener.Addr().(*net.TCPAddr).Port)
	}
	return chromeURL, proxy, proxyURL
}

// message is a Claude response with one content block (JSON) and the stop reason.
func message(t *testing.T, block, stop string) *anthropic.BetaMessage {
	t.Helper()
	var m anthropic.BetaMessage
	if err := json.Unmarshal([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[`+block+
		`],"stop_reason":"`+stop+`","usage":{"input_tokens":1000,"output_tokens":100,"cache_read_input_tokens":500,"cache_creation_input_tokens":200}}`), &m); err != nil {
		t.Fatal(err)
	}
	return &m
}
