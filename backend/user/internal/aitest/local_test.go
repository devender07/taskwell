package aitest

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/file"
	"github.com/devender07/taskwell/backend/user/internal/storage"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// setServerRuns sets the ai_tests_server_runs switch for the test and puts it back after.
func setServerRuns(t *testing.T, pool *pgxpool.Pool, on bool) {
	t.Helper()
	ctx := context.Background()
	var old string
	if err := pool.QueryRow(ctx, `select value from system_config where key = 'ai_tests_server_runs'`).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update system_config set value = $1 where key = 'ai_tests_server_runs'`, strconv.FormatBool(on)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `update system_config set value = $1 where key = 'ai_tests_server_runs'`, old)
	})
}

// TestLocalRun walks a local run: the switch refusing server runs, create, steps with and without a screenshot, the
// video through a direct upload, marks with reproduction steps to the finish, then a stale run and a cancelled one.
// The screenshots and the video go to the dev S3 store from `make up`.
func TestLocalRun(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, wsID, issueID := testIssue(t, pool)
	bob := dbtest.User(t, pool, "Bob")
	dbtest.Join(t, pool, wsID, bob, "member")
	setServerRuns(t, pool, false)
	m := &model{db: pool}
	var slug string
	if err := pool.QueryRow(ctx, `select slug from workspaces where id = $1`, wsID).Scan(&slug); err != nil {
		t.Fatal(err)
	}

	cfg := sysconfig.New(pool)
	files := storage.New(cfg)
	member := workspace.New(pool, dbtest.Auth, nil, nil).Member
	fileH := file.New(pool, files, dbtest.Auth, member, cfg, nil)
	h := New(pool, cfg, Options{}, dbtest.Auth, member)
	h.Files, h.FinishVideo = files, fileH.FinishVideo
	mux := http.NewServeMux()
	h.Register(mux)
	fileH.Register(mux)
	base := "/api/workspaces/" + slug

	// post sends a multipart form as Ann, with data as the file part named field when it isn't nil.
	post := func(path string, fields map[string]string, field string, data []byte, want int, into any) string {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for k, v := range fields {
			if err := mw.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
		if data != nil {
			fw, err := mw.CreateFormFile(field, "shot")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fw.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, base+path, &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-Test-User", ann)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return check(t, "POST "+path, rec.Code, rec.Body.String(), want, into)
	}
	do := func(user, method, path string, form url.Values, want int, into any) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		return check(t, method+" "+path, code, body, want, into)
	}
	has := func(body, msg string) {
		t.Helper()
		if !strings.Contains(body, msg) {
			t.Errorf("missing %q in %s", msg, body)
		}
	}
	// upload starts a direct upload, PUTs data to the bucket and returns the upload's id.
	upload := func(name, contentType string, data []byte) string {
		t.Helper()
		var s struct {
			ID, URL string
			Headers map[string]string
		}
		do(ann, "POST", "/uploads", url.Values{"name": {name}, "size": {strconv.Itoa(len(data))}, "type": {contentType}}, 201, &s)
		req, err := http.NewRequest(http.MethodPut, s.URL, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range s.Headers {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("PUT %s = %d", name, res.StatusCode)
		}
		return s.ID
	}
	local := url.Values{"kind": {"local"}, "client": {"Claude Code"}}
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 100)...)
	webm := append([]byte("\x1a\x45\xdf\xa3"), bytes.Repeat([]byte{0}, 200)...)

	// The switch: server runs are refused and the app is told.
	for _, kind := range []string{"ai", "replay"} {
		has(do(ann, "POST", "/issues/ENG-1/test-runs", url.Values{"kind": {kind}}, 409, nil), msgLocalOnly)
	}
	has(do(ann, "POST", "/issues/ENG-1/browser-sessions", url.Values{}, 409, nil), msgLocalOnly)
	has(do(ann, "GET", "/ai-usage", nil, 200, nil), `"server_runs":false`)

	do(ann, "POST", "/issues/ENG-1/test-runs", local, 409, nil) // no cases yet
	var caseIDs [2]string
	for i, title := range []string{"Sign in", "Sign out"} {
		id, err := m.createCase(ctx, issueID, ann, caseFields{Title: title, Steps: []string{"Open the page"}, Expected: "It works"})
		if err != nil {
			t.Fatal(err)
		}
		caseIDs[i] = id
	}

	// A manual run takes no steps.
	var manual RunDetail
	do(ann, "POST", "/issues/ENG-1/test-runs", url.Values{"kind": {"manual"}}, 201, &manual)
	has(do(ann, "POST", "/test-runs/"+manual.ID+"/results/"+manual.Results[0].ID+"/steps", url.Values{"action": {"Clicked"}}, 409, nil),
		"Only local runs take steps.")
	do(ann, "POST", "/test-runs/"+manual.ID+"/cancel", nil, 200, nil)

	// Create.
	has(do(ann, "POST", "/issues/ENG-1/test-runs", url.Values{"kind": {"local"}}, 400, nil), `"client"`)
	has(do(ann, "POST", "/issues/ENG-1/test-runs", url.Values{"kind": {"local"}, "client": {strings.Repeat("x", 41)}}, 400, nil), `"client"`)
	do(bob, "POST", "/issues/ENG-1/test-runs", local, 403, nil)
	do(ann, "POST", "/issues/ENG-1/test-runs", url.Values{"kind": {"local"}, "client": {"Cursor"}, "case_id": {wsID}}, 404, nil)
	var run RunDetail
	do(ann, "POST", "/issues/ENG-1/test-runs", local, 201, &run)
	if run.Kind != "local" || run.Status != "running" || run.Client == nil || *run.Client != "Claude Code" || run.CostUSD != nil ||
		len(run.Results) != 2 || run.Results[0].Status != "pending" || run.Results[0].VideoURL != nil {
		t.Fatalf("run %+v %+v", run.Run, run.Results)
	}
	has(do(ann, "POST", "/issues/ENG-1/test-runs", local, 409, nil), msgRunActive)
	rp := "/test-runs/" + run.ID + "/results/"
	first, second := rp+run.Results[0].ID, rp+run.Results[1].ID

	// Steps.
	var pos struct{ Position int }
	do(ann, "POST", first+"/steps", url.Values{"action": {"Opened the sign-in page"}, "url": {"http://localhost:3000/signin"}}, 201, &pos)
	if pos.Position != 0 {
		t.Errorf("first step at %d", pos.Position)
	}
	post(first+"/steps", map[string]string{"action": "Clicked Sign in"}, "screenshot", png, 201, &pos)
	if pos.Position != 1 {
		t.Errorf("second step at %d", pos.Position)
	}
	has(do(ann, "POST", first+"/steps", url.Values{"action": {" "}, "url": {strings.Repeat("u", 2001)}}, 400, nil), `"action"`)
	has(do(ann, "POST", first+"/steps", url.Values{"action": {strings.Repeat("a", 301)}}, 400, nil), `"action"`)
	has(post(first+"/steps", map[string]string{"action": "Clicked"}, "screenshot", []byte("GIF89a..."), 400, nil), `"screenshot"`)
	post(first+"/steps", map[string]string{"action": "Clicked"}, "screenshot", append(png, make([]byte, maxStepBody)...), 413, nil)
	do(bob, "POST", first+"/steps", url.Values{"action": {"Clicked"}}, 403, nil)
	do(ann, "POST", rp+run.ID+"/steps", url.Values{"action": {"Clicked"}}, 404, nil)
	do(ann, "GET", "/test-runs/"+run.ID, nil, 200, &run)
	x := run.Results[0]
	if x.Status != "running" || len(x.Log) != 2 || x.Log[0].ScreenshotURL != nil || *x.Log[0].URL != "http://localhost:3000/signin" ||
		x.Log[1].ScreenshotURL == nil || !strings.HasSuffix(*x.Log[1].ScreenshotURL, "/step-2.png") ||
		run.LatestScreenshotURL == nil || run.Counts.Pending != 2 {
		t.Fatalf("steps %+v", x)
	}
	if code, _ := dbtest.Do(t, mux, ann, "GET", *x.Log[1].ScreenshotURL, nil); code != http.StatusFound {
		t.Errorf("screenshot = %d", code)
	}

	// Video.
	has(do(ann, "POST", first+"/video", url.Values{"upload_id": {run.ID}}, 400, nil), `"upload_id"`)
	has(do(ann, "POST", first+"/video", url.Values{"upload_id": {upload("shot.png", "image/png", png)}}, 400, nil), "MP4 or WebM")
	do(ann, "POST", first+"/video", url.Values{"upload_id": {upload("sign in.webm", "video/webm", webm)}}, 200, &run)
	if v := run.Results[0].VideoURL; v == nil || !strings.HasSuffix(*v, "/sign%20in.webm") {
		t.Fatalf("video %v", v)
	} else if code, _ := dbtest.Do(t, mux, ann, "GET", *v, nil); code != http.StatusFound {
		t.Errorf("video = %d", code)
	}

	// Marks.
	has(do(ann, "POST", first, url.Values{"status": {"failed"}, "reproduction": slices.Repeat([]string{"Click"}, 31)}, 400, nil), `"reproduction"`)
	do(ann, "POST", first, url.Values{"status": {"failed"}, "note": {"The button does nothing"}, "reproduction": {"Open /signin", " ", "Click Sign in"}}, 200, &run)
	x = run.Results[0]
	if run.Status != "running" || x.Status != "failed" || *x.Summary != "The button does nothing" || len(x.Reproduction) != 2 ||
		x.MarkedBy.ID != ann || x.VideoURL == nil {
		t.Fatalf("marked %+v", x)
	}
	has(do(ann, "POST", first+"/steps", url.Values{"action": {"Clicked"}}, 409, nil), "This result is already marked.")
	has(do(ann, "POST", first+"/video", url.Values{"upload_id": {upload("late.webm", "video/webm", webm)}}, 409, nil), "This result is already marked.")
	do(ann, "POST", second, url.Values{"status": {"passed"}}, 200, &run)
	if run.Status != "failed" || run.FinishedAt == nil || run.Counts.Passed != 1 || run.Counts.Failed != 1 {
		t.Fatalf("finished %+v", run.Run)
	}
	has(do(ann, "POST", second+"/steps", url.Values{"action": {"Clicked"}}, 409, nil), errRunFinished)
	var event string
	if err := pool.QueryRow(ctx, `select data::text from issue_events where kind = 'test_run' and data->>'run_id' = $1`, run.ID).Scan(&event); err != nil {
		t.Fatal(err)
	}
	has(event, `"kind": "local"`)

	// A runner that went quiet: the run of one case ends as an error.
	var stale RunDetail
	do(ann, "POST", "/issues/ENG-1/test-runs", url.Values{"kind": {"local"}, "client": {"Cursor"}, "case_id": {caseIDs[1]}}, 201, &stale)
	if len(stale.Results) != 1 || stale.Results[0].CaseID != caseIDs[1] {
		t.Fatalf("one case %+v", stale.Results)
	}
	do(ann, "POST", "/test-runs/"+stale.ID+"/results/"+stale.Results[0].ID+"/steps", url.Values{"action": {"Opened"}}, 201, nil)
	if err := m.failStale(ctx); err != nil {
		t.Fatal(err)
	}
	do(ann, "GET", "/test-runs/"+stale.ID, nil, 200, &stale)
	if stale.Status != "running" {
		t.Fatalf("fresh run ended: %+v", stale.Run)
	}
	if _, err := pool.Exec(ctx, `update test_runs set claimed_at = now() - interval '16 minutes' where id = $1`, stale.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.failStale(ctx); err != nil {
		t.Fatal(err)
	}
	do(ann, "GET", "/test-runs/"+stale.ID, nil, 200, &stale)
	if stale.Status != "error" || stale.Error == nil || *stale.Error != msgStaleLocal || stale.Results[0].Status != "skipped" {
		t.Fatalf("stale %+v %+v", stale.Run, stale.Results[0])
	}

	// The 60-step cap, then cancel: the runner's next call gets the 409.
	var third RunDetail
	do(ann, "POST", "/issues/ENG-1/test-runs", local, 201, &third)
	steps := "/test-runs/" + third.ID + "/results/" + third.Results[0].ID + "/steps"
	if _, err := pool.Exec(ctx, `insert into test_steps (result_id, position, action) select $1, n, 'Clicked' from generate_series(0, $2 - 1) n`,
		third.Results[0].ID, maxLocalSteps); err != nil {
		t.Fatal(err)
	}
	has(do(ann, "POST", steps, url.Values{"action": {"Clicked"}}, 409, nil), "A test case can have 60 steps.")
	do(ann, "POST", "/test-runs/"+third.ID+"/cancel", nil, 200, &third)
	if third.Status != "cancelled" || third.Results[0].Status != "skipped" || third.Results[1].Status != "skipped" {
		t.Fatalf("cancelled %+v %+v", third.Run, third.Results)
	}
	has(do(ann, "POST", steps, url.Values{"action": {"Clicked"}}, 409, nil), errRunFinished)
}

// check fails the test unless code is want, and decodes body into into when it isn't nil.
func check(t *testing.T, what string, code int, body string, want int, into any) string {
	t.Helper()
	if code != want {
		t.Fatalf("%s: %d %s, want %d", what, code, body, want)
	}
	if into != nil {
		if err := json.Unmarshal([]byte(body), into); err != nil {
			t.Fatal(err)
		}
	}
	return body
}
