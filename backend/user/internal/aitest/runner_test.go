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

// TestRunner runs an AI run end to end against a local site in real Chrome with a scripted Claude: generated
// cases, a sign-in that passes with the sealed password typed by the tester, and a redirect to the metadata
// address that's blocked and fails. Needs CHROME_URL (e.g. google-chrome --headless=new --remote-debugging-port=9333),
// or CHROME_URL=http://127.0.0.1:9222 CHROME_IN_DOCKER=1 with the dev compose chrome.
func TestRunner(t *testing.T) {
	chromeURL := os.Getenv("CHROME_URL")
	if chromeURL == "" {
		t.Skip("CHROME_URL not set")
	}
	if resp, err := http.Get(chromeURL + "/json/version"); err != nil {
		t.Skipf("chrome not reachable: %v", err)
	} else {
		resp.Body.Close()
	}
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, wsID, issueID := testIssue(t, pool)
	m := &model{db: pool}
	key, _ := Key("", false)

	const password = "s3cret-pw"
	var mu sync.Mutex
	var gotPassword string
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
			fmt.Fprint(w, `<title>Sign in</title><form method=post action=/login><input name=email aria-label=Email>
				<input name=password type=password aria-label=Password><button>Sign in</button></form>`)
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
	if _, err := m.saveAccount(ctx, key, wsID, "x", ann, "", "Admin", "ann@acme.test", password); err != nil {
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
			return tool("finish", map[string]any{"result": result, "summary": "Signed in.", "reproduction": []string{}}), nil
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
	proxy := NewProxy(true) // the site is on 127.0.0.1
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
	defer psrv.Close()
	proxyURL := psrv.URL
	if docker {
		proxyURL = "http://host.docker.internal:" + strconv.Itoa(psrv.Listener.Addr().(*net.TCPAddr).Port)
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
