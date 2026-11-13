package aitest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/shared/doc"
	"github.com/devender07/taskwell/backend/user/internal/file"
	"github.com/devender07/taskwell/backend/user/internal/issue"
	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/storage"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

const (
	errWorkspaceNotFound = "Workspace not found."
	errIssueNotFound     = "Issue not found."
	errCaseNotFound      = "Test case not found."
	errRunNotFound       = "Test run not found."
	errResultNotFound    = "Test result not found."
	errForbidden         = "You don't have permission to do that."
	errRunFinished       = "This run is finished."
	errAccountNotFound   = "Test account not found."
	msgRunActive         = "A test is already running on this issue."
	msgLocalOnly         = "Tests now run on your own machine. Set up taskwell-tester in Settings → AI testing."
)

type Handler struct {
	model        *model
	cfg          *sysconfig.Store
	opts         Options
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
	tester       *http.Client // browser sessions

	// Local runs: where step screenshots go, and the file handler's FinishVideo and Room (the bytes the workspace's
	// storage quota still allows, with the message past it). Set before Register.
	Files       *storage.Store
	Room        func(ctx context.Context, workspaceID string) (int64, string, error)
	FinishVideo func(ctx context.Context, workspaceID, userID, uploadID string, limit int64) (*file.File, string, error)
}

// Options are the API's settings for test sites and accounts.
type Options struct {
	Key          []byte // from Key(TEST_SECRET_KEY), seals test account passwords
	AllowHTTP    bool   // http:// test sites, outside production
	AllowPrivate bool   // test sites on private or local addresses (AI_TEST_ALLOW_PRIVATE, dev only)
}

// New takes the auth handler's Authenticate and the workspace handler's Member.
func New(db *pgxpool.Pool, cfg *sysconfig.Store, opts Options, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, cfg: cfg, opts: opts, authenticate: authenticate, member: member,
		tester: &http.Client{Timeout: 2 * actionTimeout}}
}

// Run ends stale runs (failStale) every every until ctx ends. The tester does the same while it's up; local runs
// need it here, since they run with the tester off. Instances can run it side by side: each run is taken with for
// update skip locked.
func (h *Handler) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := h.model.failStale(ctx); err != nil && ctx.Err() == nil {
			slog.Error("stale test runs", "err", err)
		}
	}
}

// --- cases ---

func (h *Handler) listCases(w http.ResponseWriter, r *http.Request, userID string) error {
	_, i, ok, err := h.issue(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	cs, err := h.model.cases(r.Context(), i.id)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, cs)
	return nil
}

func (h *Handler) createCase(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	f, ok := readCase(w, r)
	if !ok {
		return nil
	}
	id, err := h.model.createCase(r.Context(), i.id, userID, f)
	if errors.Is(err, errTooMany) {
		web.Err(w, http.StatusConflict, "An issue can have at most 50 test cases.")
		return nil
	}
	if err != nil {
		return err
	}
	return h.respondCase(w, r, http.StatusCreated, ws.ID, id, userID)
}

func (h *Handler) updateCase(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	id := r.PathValue("id")
	if !web.IsUUID(id) {
		web.Err(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	f, ok := readCase(w, r)
	if !ok {
		return nil
	}
	err = h.model.updateCase(r.Context(), ws.ID, id, userID, f)
	if errors.Is(err, errNotFound) {
		web.Err(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	if err != nil {
		return err
	}
	return h.respondCase(w, r, http.StatusOK, ws.ID, id, userID)
}

func (h *Handler) deleteCase(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	id := r.PathValue("id")
	if web.IsUUID(id) {
		err = h.model.deleteCase(r.Context(), ws.ID, id, userID)
	} else {
		err = errNotFound
	}
	if errors.Is(err, errNotFound) {
		web.Err(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) orderCases(w http.ResponseWriter, r *http.Request, userID string) error {
	_, i, ok, err := h.issue(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	ids := r.PostForm["ids"]
	for _, id := range ids {
		if !web.IsUUID(id) {
			err = errBadOrder
		}
	}
	if err == nil {
		err = h.model.orderCases(r.Context(), i.id, userID, ids)
	}
	if errors.Is(err, errBadOrder) {
		web.Err(w, http.StatusBadRequest, "The list changed. Reload and try again.")
		return nil
	}
	if err != nil {
		return err
	}
	cs, err := h.model.cases(r.Context(), i.id)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, cs)
	return nil
}

// saveScript records a case's browser steps, written by an MCP client, for replay runs and the Playwright export.
func (h *Handler) saveScript(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	id := r.PathValue("id")
	if !web.IsUUID(id) {
		web.Err(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	var sc script
	if err := json.Unmarshal([]byte(r.FormValue("script")), &sc); err != nil {
		web.Fields(w, http.StatusBadRequest, map[string]string{"script": "Send the script as JSON."})
		return nil
	}
	hosts, labels, err := h.scriptScope(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	sc, msg := checkScript(sc, hosts, labels)
	_, setCode := r.PostForm["playwright"]
	code := strings.TrimSpace(r.PostForm.Get("playwright"))
	if !web.Check(w, map[string]string{"script": msg, "playwright": checkPlaywright(code)}) {
		return nil
	}
	err = h.model.setScript(r.Context(), ws.ID, id, userID, &sc, setCode, code)
	if errors.Is(err, errNotFound) {
		web.Err(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	if err != nil {
		return err
	}
	return h.respondCase(w, r, http.StatusOK, ws.ID, id, userID)
}

// scriptScope is what a script may use: the workspace's site hosts and account labels.
func (h *Handler) scriptScope(ctx context.Context, wsID string) (hosts, labels []string, err error) {
	sites, err := h.model.sites(ctx, wsID)
	if err != nil {
		return nil, nil, err
	}
	accounts, err := h.model.accounts(ctx, wsID)
	if err != nil {
		return nil, nil, err
	}
	labels = make([]string, len(accounts))
	for i, a := range accounts {
		labels[i] = a.Label
	}
	return siteHosts(sites), labels, nil
}

func (h *Handler) clearScript(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	err = errNotFound
	if id := r.PathValue("id"); web.IsUUID(id) {
		err = h.model.setScript(r.Context(), ws.ID, id, userID, nil, false, "")
	}
	return noContent(w, err, errCaseNotFound)
}

// playwright answers the issue's cases as a Playwright spec to download.
func (h *Handler) playwright(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	cs, err := h.model.exportCases(r.Context(), i.id)
	if err != nil {
		return err
	}
	sites, err := h.model.sites(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+i.identifier+`.spec.ts"`)
	_, _ = w.Write([]byte(playwrightSpec(i.identifier, i.title, sites, cs)))
	return nil
}

// readCase returns the form's cleaned case, or writes a 400 with field errors and returns ok=false.
func readCase(w http.ResponseWriter, r *http.Request) (caseFields, bool) {
	_ = r.ParseMultipartForm(maxBody)
	f := cleanCase(caseFields{Title: r.PostForm.Get("title"), Steps: r.PostForm["steps"], Expected: r.PostForm.Get("expected")})
	return f, web.Check(w, checkCase(f))
}

func (h *Handler) respondCase(w http.ResponseWriter, r *http.Request, status int, workspaceID, id, userID string) error {
	c, err := h.model.testCase(r.Context(), workspaceID, id, userID)
	if err != nil {
		return err
	}
	if c == nil {
		web.Err(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	web.JSON(w, status, c)
	return nil
}

// --- runs ---

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request, userID string) error {
	_, i, ok, err := h.issue(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	var before *time.Time
	if v := r.URL.Query().Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			web.Err(w, http.StatusBadRequest, "before must be an RFC 3339 time.")
			return nil
		}
		before = &t
	}
	rs, more, err := h.model.runs(r.Context(), i.id, before)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"runs": rs, "has_more": more})
	return nil
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	return h.respondRun(w, r, http.StatusOK, ws.ID, r.PathValue("id"), userID)
}

// live proxies the tester's MJPEG stream of a running AI run's browser to a workspace member.
func (h *Handler) live(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	id := r.PathValue("id")
	tester := ""
	if web.IsUUID(id) {
		if tester, err = h.model.liveTester(r.Context(), ws.ID, id, userID); err != nil {
			return err
		}
	}
	target, err := url.Parse(tester)
	if tester == "" || err != nil {
		web.Err(w, http.StatusNotFound, "This test run isn't running.")
		return nil
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{}) // the stream lasts as long as the run
	(&httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL = &url.URL{Scheme: target.Scheme, Host: target.Host, Path: "/live/" + id}
			pr.Out.Host = target.Host
			pr.Out.Header = http.Header{} // the member's cookies and tokens stay here
			pr.Out.Body, pr.Out.ContentLength = nil, 0
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			web.Err(w, http.StatusBadGateway, "The live view isn't available. Try again.")
		},
	}).ServeHTTP(w, r)
	return nil
}

func (h *Handler) createRun(w http.ResponseWriter, r *http.Request, userID string) error {
	_ = r.ParseMultipartForm(maxBody)
	switch r.PostForm.Get("kind") {
	case "manual":
		return h.createManualRun(w, r, userID)
	case "ai":
		return h.createAIRun(w, r, userID)
	case "replay":
		return h.createReplayRun(w, r, userID)
	case "local":
		return h.createLocalRun(w, r, userID)
	}
	if _, _, ok, err := h.issue(w, r, userID, ""); !ok || err != nil {
		return err
	}
	web.Fields(w, http.StatusBadRequest, map[string]string{"kind": "Choose ai, manual, replay or local."})
	return nil
}

func (h *Handler) createManualRun(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	id, err := h.model.createManual(r.Context(), ws.ID, i.id, userID)
	if errors.Is(err, errNoCases) {
		web.Err(w, http.StatusConflict, "Add a test case first.")
		return nil
	}
	if err != nil {
		return err
	}
	return h.respondRun(w, r, http.StatusCreated, ws.ID, id, userID)
}

// createLocalRun starts a run the caller's own agent drives on their machine through taskwell-tester: no test site,
// tester or spend check.
func (h *Handler) createLocalRun(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	client, caseID := strings.TrimSpace(r.PostForm.Get("client")), r.PostForm.Get("case_id")
	if n := doc.Len(client); n < 1 || n > maxClientLen {
		web.Fields(w, http.StatusBadRequest, map[string]string{"client": "Name the agent running the test in 1–40 characters."})
		return nil
	}
	if caseID != "" && !web.IsUUID(caseID) {
		web.Err(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	id, err := h.model.createLocal(r.Context(), ws.ID, i.id, userID, client, caseID)
	switch {
	case errors.Is(err, errNotFound):
		web.Err(w, http.StatusNotFound, errCaseNotFound)
	case errors.Is(err, errNoCases):
		web.Err(w, http.StatusConflict, "Add a test case first.")
	case errors.Is(err, errRunActive):
		web.Err(w, http.StatusConflict, msgRunActive)
	case err != nil:
		return err
	default:
		return h.respondRun(w, r, http.StatusCreated, ws.ID, id, userID)
	}
	return nil
}

// addStep logs a step taskwell-tester took in a local run's case, with its screenshot when there is one.
func (h *Handler) addStep(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	runID, resultID := r.PathValue("id"), r.PathValue("result_id")
	if !web.IsUUID(runID) {
		web.Err(w, http.StatusNotFound, errRunNotFound)
		return nil
	}
	if err := r.ParseMultipartForm(maxStepBody); errors.As(err, new(*http.MaxBytesError)) {
		web.Err(w, http.StatusRequestEntityTooLarge, "A step with its screenshot must be 2 MB or smaller.")
		return nil
	}
	action, pageURL := strings.TrimSpace(r.PostForm.Get("action")), strings.TrimSpace(r.PostForm.Get("url"))
	errs := map[string]string{}
	if n := doc.Len(action); n < 1 || n > maxActionLen {
		errs["action"] = "Describe the step in 1–300 characters."
	}
	if len(pageURL) > 2000 {
		errs["url"] = "URL must be 2000 characters or fewer."
	}
	var img []byte
	contentType := ""
	if f, _, err := r.FormFile("screenshot"); err == nil {
		img, err = io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			return fmt.Errorf("read screenshot: %w", err)
		}
		if contentType = http.DetectContentType(img); contentType != "image/jpeg" && contentType != "image/png" {
			errs["screenshot"] = "Send the screenshot as a JPEG or PNG."
		}
	}
	if !web.Check(w, errs) {
		return nil
	}
	if img != nil {
		room, full, err := h.Room(r.Context(), ws.ID)
		if err != nil {
			return err
		}
		if int64(len(img)) > room {
			web.Err(w, http.StatusRequestEntityTooLarge, full)
			return nil
		}
	}
	err = errNotFound
	position := 0
	if web.IsUUID(resultID) {
		position, err = h.model.addLocalStep(r.Context(), h.Files, ws.ID, runID, resultID, userID, action, pageURL, contentType, img)
	}
	if errors.Is(err, errStepLimit) {
		web.Err(w, http.StatusConflict, "A test case can have 60 steps.")
		return nil
	}
	if refusedLocal(w, err) {
		return nil
	}
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, map[string]int{"position": position})
	return nil
}

// addVideo attaches the recording of a local run's case: upload_id is a direct upload (POST .../uploads) the client
// has PUT.
func (h *Handler) addVideo(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	runID, resultID := r.PathValue("id"), r.PathValue("result_id")
	if !web.IsUUID(runID) {
		web.Err(w, http.StatusNotFound, errRunNotFound)
		return nil
	}
	if !web.IsUUID(resultID) {
		web.Err(w, http.StatusNotFound, errResultNotFound)
		return nil
	}
	f, msg, err := h.FinishVideo(r.Context(), ws.ID, userID, strings.TrimSpace(r.FormValue("upload_id")), maxVideo)
	if err != nil {
		return err
	}
	if !web.Check(w, map[string]string{"upload_id": msg}) {
		return nil
	}
	// A video the run refuses (it ended, or the result is marked) or replaces is referenced nowhere: the daily sweep
	// deletes it.
	err = h.model.setVideo(r.Context(), ws.ID, runID, resultID, userID, f.ID)
	if refusedLocal(w, err) {
		return nil
	}
	if err != nil {
		return err
	}
	return h.respondRun(w, r, http.StatusOK, ws.ID, runID, userID)
}

// refusedLocal writes the 404 or 409 for a step or video its local run can't take, and reports whether it did.
func refusedLocal(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, errNotFound):
		web.Err(w, http.StatusNotFound, errResultNotFound)
	case errors.Is(err, errNotLocal):
		web.Err(w, http.StatusConflict, "Only local runs take steps.")
	case errors.Is(err, errFinished):
		web.Err(w, http.StatusConflict, errRunFinished)
	case errors.Is(err, errMarked):
		web.Err(w, http.StatusConflict, "This result is already marked.")
	default:
		return false
	}
	return true
}

func (h *Handler) markResult(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	runID, resultID := r.PathValue("id"), r.PathValue("result_id")
	if !web.IsUUID(runID) {
		web.Err(w, http.StatusNotFound, errRunNotFound)
		return nil
	}
	_ = r.ParseMultipartForm(maxBody)
	status, note := r.PostForm.Get("status"), strings.TrimSpace(r.PostForm.Get("note"))
	errs := map[string]string{}
	if status != "passed" && status != "failed" && status != "skipped" {
		errs["status"] = "Choose passed, failed or skipped."
	}
	if doc.Len(note) > maxNoteLen {
		errs["note"] = "Note must be 2000 characters or fewer."
	}
	repro := []string{}
	for _, l := range r.PostForm["reproduction"] {
		if l = strings.TrimSpace(l); l != "" {
			repro = append(repro, l)
		}
	}
	if len(repro) > maxReproLines || slices.ContainsFunc(repro, func(l string) bool { return doc.Len(l) > maxReproLen }) {
		errs["reproduction"] = "Use 30 lines or fewer, each 300 characters or fewer."
	}
	if !web.Check(w, errs) {
		return nil
	}
	var notePtr *string
	if note != "" {
		notePtr = &note
	}
	if web.IsUUID(resultID) {
		err = h.model.mark(r.Context(), ws.ID, runID, resultID, userID, status, notePtr, repro)
	} else {
		err = errNotFound
	}
	switch {
	case errors.Is(err, errNotFound):
		web.Err(w, http.StatusNotFound, errResultNotFound)
	case errors.Is(err, errAIRun):
		web.Err(w, http.StatusConflict, "AI runs are marked by the test runner.")
	case errors.Is(err, errFinished):
		web.Err(w, http.StatusConflict, errRunFinished)
	case err != nil:
		return err
	default:
		return h.respondRun(w, r, http.StatusOK, ws.ID, runID, userID)
	}
	return nil
}

func (h *Handler) cancelRun(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	id := r.PathValue("id")
	kind := ""
	if web.IsUUID(id) {
		if kind, err = h.model.runKind(r.Context(), ws.ID, id, userID); err != nil {
			return err
		}
	}
	perm := workspace.PermTestsManage
	if kind == "ai" {
		perm = workspace.PermAITestsRun
	}
	switch {
	case kind == "":
		web.Err(w, http.StatusNotFound, errRunNotFound)
		return nil
	case !workspace.Has(ws.Permissions, perm):
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil
	}
	err = h.model.cancel(r.Context(), ws.ID, id, userID)
	switch {
	case errors.Is(err, errNotFound):
		web.Err(w, http.StatusNotFound, errRunNotFound)
	case errors.Is(err, errFinished):
		web.Err(w, http.StatusConflict, errRunFinished)
	case err != nil:
		return err
	default:
		return h.respondRun(w, r, http.StatusOK, ws.ID, id, userID)
	}
	return nil
}

func (h *Handler) respondRun(w http.ResponseWriter, r *http.Request, status int, workspaceID, id, userID string) error {
	var d *RunDetail
	var err error
	if web.IsUUID(id) {
		if d, err = h.model.run(r.Context(), workspaceID, id, userID); err != nil {
			return err
		}
	}
	if d == nil {
		web.Err(w, http.StatusNotFound, errRunNotFound)
		return nil
	}
	web.JSON(w, status, d)
	return nil
}

// --- shared ---

// issue returns the path's workspace and issue, or writes a 404 (or a 403 when perm is set and the caller lacks
// it) and returns ok=false.
func (h *Handler) issue(w http.ResponseWriter, r *http.Request, userID, perm string) (*workspace.Workspace, *issueRef, bool, error) {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return nil, nil, false, err
	}
	var i *issueRef
	if key, number, ok := issue.ParseIdentifier(r.PathValue("identifier")); ok {
		if i, err = h.model.issue(r.Context(), ws.ID, key, number, userID); err != nil {
			return nil, nil, false, err
		}
	}
	switch {
	case i == nil:
		web.Err(w, http.StatusNotFound, errIssueNotFound)
		return nil, nil, false, nil
	case perm != "" && !workspace.Has(ws.Permissions, perm):
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil, nil, false, nil
	case perm != "" && i.archived:
		web.Err(w, http.StatusForbidden, "This project is archived.")
		return nil, nil, false, nil
	}
	return ws, i, true, nil
}

// access returns the path's workspace. It writes a 404 when the caller isn't a member, or a 403 when perm is set
// and they lack it, and returns ok=false.
func (h *Handler) access(w http.ResponseWriter, r *http.Request, userID, perm string) (*workspace.Workspace, bool, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return nil, false, err
	case ws == nil:
		web.Err(w, http.StatusNotFound, errWorkspaceNotFound)
		return nil, false, nil
	case perm != "" && !workspace.Has(ws.Permissions, perm):
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil, false, nil
	}
	return ws, true, nil
}

// --- AI runs ---

func (h *Handler) createAIRun(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID, workspace.PermAITestsRun)
	if !ok || err != nil {
		return err
	}
	if on, err := serverRuns(r.Context(), h.cfg); err != nil {
		return err
	} else if !on {
		web.Err(w, http.StatusConflict, msgLocalOnly)
		return nil
	}
	tester, err := h.model.testerURL(r.Context())
	if err != nil {
		return err
	}
	if tester == "" {
		web.Err(w, http.StatusServiceUnavailable, "AI testing isn't set up on this server.")
		return nil
	}
	limit, err := limitUSD(r.Context(), h.cfg, h.model.db, ws.ID)
	if err != nil {
		return err
	}
	if limit == 0 {
		if p, err := plan.ForWorkspace(r.Context(), h.model.db, ws.ID); err != nil {
			return err
		} else if p.AIUSD == 0 {
			web.Err(w, http.StatusPaymentRequired, plan.Msg(msgNoAI, msgNoAIBeta))
			return nil
		}
	}
	caseID := r.PostForm.Get("case_id")
	if caseID != "" && !web.IsUUID(caseID) {
		web.Err(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	id, err := h.model.createAI(r.Context(), ws.ID, i.id, userID, caseID, caseID == "" && r.PostForm.Get("regenerate") == "true", int64(limit*1e6))
	switch {
	case errors.Is(err, errNotFound):
		web.Err(w, http.StatusNotFound, errCaseNotFound)
	case errors.Is(err, errNoSites):
		web.Err(w, http.StatusConflict, "Add a test site in Settings → AI testing first.")
	case errors.Is(err, errLimit):
		web.Err(w, http.StatusConflict, msgLimit)
	case errors.Is(err, errRunActive):
		web.Err(w, http.StatusConflict, msgRunActive)
	case err != nil:
		return err
	default:
		return h.respondRun(w, r, http.StatusCreated, ws.ID, id, userID)
	}
	return nil
}

// createReplayRun queues a run that replays the recorded cases: no Claude call and no spend-limit check.
func (h *Handler) createReplayRun(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	if on, err := serverRuns(r.Context(), h.cfg); err != nil {
		return err
	} else if !on {
		web.Err(w, http.StatusConflict, msgLocalOnly)
		return nil
	}
	tester, err := h.model.testerURL(r.Context())
	if err != nil {
		return err
	}
	if tester == "" {
		web.Err(w, http.StatusServiceUnavailable, "AI testing isn't set up on this server.")
		return nil
	}
	id, err := h.model.createReplay(r.Context(), ws.ID, i.id, userID)
	switch {
	case errors.Is(err, errNoSites):
		web.Err(w, http.StatusConflict, "Add a test site in Settings → AI testing first.")
	case errors.Is(err, errNoScript):
		web.Err(w, http.StatusConflict, "Record a test case first.")
	case errors.Is(err, errRunActive):
		web.Err(w, http.StatusConflict, msgRunActive)
	case err != nil:
		return err
	default:
		return h.respondRun(w, r, http.StatusCreated, ws.ID, id, userID)
	}
	return nil
}

// serverRuns is ai_tests_server_runs: whether AI runs, replays and browser sessions run on Taskwell's tester. Off,
// tests run on the user's machine (local runs).
func serverRuns(ctx context.Context, cfg *sysconfig.Store) (bool, error) {
	v, err := cfg.Get(ctx, "ai_tests_server_runs")
	return v == "true", err
}

// limitUSD is what the workspace's account may spend on AI tests a month: its plan's AIUSD, and never more than
// ai_test_monthly_limit_usd (the ceiling for any account); a missing or broken value means no AI spend.
func limitUSD(ctx context.Context, cfg *sysconfig.Store, db plan.Querier, wsID string) (float64, error) {
	v, err := cfg.Get(ctx, "ai_test_monthly_limit_usd")
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return 0, nil
	}
	p, err := plan.ForWorkspace(ctx, db, wsID)
	if err != nil {
		return 0, err
	}
	return min(f, p.AIUSD), nil
}

func (h *Handler) usage(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	if workspace.DenyGuest(w, ws) {
		return nil
	}
	n, err := spent(r.Context(), h.model.db, ws.ID)
	if err != nil {
		return err
	}
	limit, err := limitUSD(r.Context(), h.cfg, h.model.db, ws.ID)
	if err != nil {
		return err
	}
	on, err := serverRuns(r.Context(), h.cfg)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"spent_usd": float64(n) / 1e6, "limit_usd": limit,
		"resets_at": monthStart(time.Now()).AddDate(0, 1, 0), "server_runs": on})
	return nil
}

// --- browser sessions ---

var sessionActions = []string{"navigate", "click", "type", "select", "press", "wait", "fill_password", "assert_text", "assert_no_text", "snapshot"}

// browserSession is an open session in the tester at its URL; token is the tester's X-Taskwell-Session-Token.
type browserSession struct{ id, tester, token string }

// createSession opens a browser on the tester for the issue's test cases, at url or the first test site.
func (h *Handler) createSession(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	if on, err := serverRuns(r.Context(), h.cfg); err != nil {
		return err
	} else if !on {
		web.Err(w, http.StatusConflict, msgLocalOnly)
		return nil
	}
	tester, err := h.model.testerURL(r.Context())
	if err != nil {
		return err
	}
	if tester == "" {
		web.Err(w, http.StatusServiceUnavailable, "AI testing isn't set up on this server.")
		return nil
	}
	sites, err := h.model.sites(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	if len(sites) == 0 {
		web.Err(w, http.StatusConflict, "Add a test site in Settings → AI testing first.")
		return nil
	}
	start := strings.TrimSpace(r.FormValue("url"))
	if start == "" {
		start = sites[0].BaseURL
	}
	if u, err := url.Parse(start); err != nil || (u.Scheme != "https" && u.Scheme != "http") || !allowedHost(u.Hostname(), siteHosts(sites)) {
		web.Fields(w, http.StatusBadRequest, map[string]string{"url": msgSessionURL})
		return nil
	}
	code, data, err := h.callTester(r.Context(), http.MethodPost, tester+"/sessions", "", map[string]string{"workspace_id": ws.ID, "url": start})
	if err != nil {
		return err
	}
	switch code {
	case http.StatusCreated:
	case http.StatusConflict:
		web.Err(w, http.StatusConflict, msgBrowserBusy)
		return nil
	case http.StatusBadRequest:
		web.Fields(w, http.StatusBadRequest, map[string]string{"url": msgSessionURL})
		return nil
	default:
		return fmt.Errorf("open browser session: tester answered %d", code)
	}
	var a struct {
		ID    string          `json:"id"`
		Token string          `json:"token"`
		Page  json.RawMessage `json:"page"`
		Error string          `json:"error,omitempty"`
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("open browser session: %w", err)
	}
	if err := h.model.createSession(r.Context(), a.ID, ws.ID, i.id, userID, tester, a.Token); err != nil {
		_ = h.endSession(context.WithoutCancel(r.Context()), browserSession{a.ID, tester, a.Token}) // logged inside
		return err
	}
	web.JSON(w, http.StatusCreated, struct {
		ID    string          `json:"id"`
		Page  json.RawMessage `json:"page"`
		Error string          `json:"error,omitempty"`
	}{a.ID, a.Page, a.Error})
	return nil
}

func (h *Handler) sessionAction(w http.ResponseWriter, r *http.Request, userID string) error {
	_, s, ok, err := h.session(w, r, userID)
	if !ok || err != nil {
		return err
	}
	action := r.FormValue("action")
	if !slices.Contains(sessionActions, action) {
		web.Fields(w, http.StatusBadRequest, map[string]string{"action": "Choose an action."})
		return nil
	}
	ref, _ := strconv.Atoi(r.FormValue("ref")) // not a number: no element has it, and the tester says so
	seconds, _ := strconv.Atoi(r.FormValue("seconds"))
	data, ok, err := h.toTester(w, r, s, http.MethodPost, "/actions", map[string]any{"action": action, "ref": ref,
		"text": r.FormValue("text"), "url": strings.TrimSpace(r.FormValue("url")), "value": r.FormValue("value"),
		"key": r.FormValue("key"), "seconds": seconds, "account": r.FormValue("account"),
		"failing": r.FormValue("failing") == "true"})
	if !ok || err != nil {
		return err
	}
	var a struct {
		Page  json.RawMessage   `json:"page"`
		Error string            `json:"error,omitempty"`
		Steps []json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("browser session action: %w", err)
	}
	web.JSON(w, http.StatusOK, map[string]any{"page": a.Page, "error": a.Error, "steps": len(a.Steps)})
	return nil
}

func (h *Handler) sessionScreenshot(w http.ResponseWriter, r *http.Request, userID string) error {
	_, s, ok, err := h.session(w, r, userID)
	if !ok || err != nil {
		return err
	}
	data, ok, err := h.toTester(w, r, s, http.MethodGet, "/screenshot", nil)
	if !ok || err != nil {
		return err
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
	return nil
}

// saveSession stores the session's steps as the case's script, checked like a script an MCP client sends, and
// closes the session. A step or check that doesn't pass keeps the session open, so the client can fix it.
func (h *Handler) saveSession(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, s, ok, err := h.session(w, r, userID)
	if !ok || err != nil {
		return err
	}
	caseID, check := r.FormValue("case_id"), strings.TrimSpace(r.FormValue("check"))
	if !web.IsUUID(caseID) {
		web.Err(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	msg := ""
	switch {
	case check == "":
		msg = "Add a check: text that's on the page only when the expected result is met."
	case doc.Len(check) > maxCheckLen:
		msg = "Check must be 200 characters or fewer."
	}
	if !web.Check(w, map[string]string{"check": msg}) {
		return nil
	}
	data, ok, err := h.toTester(w, r, s, http.MethodPost, "/actions", map[string]string{"action": "snapshot"})
	if !ok || err != nil {
		return err
	}
	var a struct {
		Steps []scriptStep `json:"steps"`
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("browser session steps: %w", err)
	}
	hosts, labels, err := h.scriptScope(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	sc, msg := checkScript(script{Steps: a.Steps, Check: check}, hosts, labels)
	if !web.Check(w, map[string]string{"script": msg}) {
		return nil
	}
	err = h.model.setScript(r.Context(), ws.ID, caseID, userID, &sc, true, "") // the export generates code from the steps
	if errors.Is(err, errNotFound) {
		web.Err(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	if err != nil {
		return err
	}
	if err := h.endSession(r.Context(), s); err != nil {
		return err
	}
	return h.respondCase(w, r, http.StatusOK, ws.ID, caseID, userID)
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request, userID string) error {
	_, s, ok, err := h.session(w, r, userID)
	if !ok || err != nil {
		return err
	}
	if err := h.endSession(r.Context(), s); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// session returns the path's session when it's the caller's and open. It writes a 403 without tests.manage, or a
// 404, and returns ok=false.
func (h *Handler) session(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, browserSession, bool, error) {
	s := browserSession{id: r.PathValue("id")}
	ws, ok, err := h.access(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return nil, s, false, err
	}
	err = errNotFound
	if web.IsUUID(s.id) {
		s.tester, s.token, err = h.model.session(r.Context(), ws.ID, s.id, userID)
	}
	if errors.Is(err, errNotFound) {
		web.Err(w, http.StatusNotFound, msgSessionEnded)
		return nil, s, false, nil
	}
	return ws, s, err == nil, err
}

// toTester sends the request for the session to its tester and returns the 200 answer's body. When the tester
// doesn't have the session any more (idle, over time, restarted) or can't be reached, the session is closed here
// too and a 404 written, with ok=false.
func (h *Handler) toTester(w http.ResponseWriter, r *http.Request, s browserSession, method, path string, body any) ([]byte, bool, error) {
	code, data, err := h.callTester(r.Context(), method, s.tester+"/sessions/"+s.id+path, s.token, body)
	switch {
	case err == nil && code == http.StatusOK:
		return data, true, nil
	case err == nil && code != http.StatusNotFound:
		return nil, false, fmt.Errorf("browser session: tester answered %d", code)
	case err != nil && r.Context().Err() != nil: // the request ran out of time; the session may be fine
		return nil, false, err
	case err != nil:
		slog.WarnContext(r.Context(), "browser session: tester unreachable", "session", s.id, "err", err)
	}
	if err := h.model.closeSession(r.Context(), s.id); err != nil {
		return nil, false, err
	}
	web.Err(w, http.StatusNotFound, msgSessionEnded)
	return nil, false, nil
}

// endSession closes the session on its tester (already gone is fine) and here.
func (h *Handler) endSession(ctx context.Context, s browserSession) error {
	code, _, err := h.callTester(ctx, http.MethodPost, s.tester+"/sessions/"+s.id+"/close", s.token, nil)
	if err != nil || (code != http.StatusOK && code != http.StatusNotFound) {
		slog.WarnContext(ctx, "browser session: close on the tester", "session", s.id, "status", code, "err", err)
	}
	return h.model.closeSession(ctx, s.id)
}

// callTester sends body as JSON (when not nil) with the session token (when set) and returns the answer.
func (h *Handler) callTester(ctx context.Context, method, target, token string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encode tester request: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return 0, nil, fmt.Errorf("tester request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set(sessionHeader, token)
	}
	res, err := h.tester.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("call tester: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("read tester answer: %w", err)
	}
	return res.StatusCode, data, nil
}

// --- sites ---

func (h *Handler) listSites(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	if workspace.DenyGuest(w, ws) {
		return nil
	}
	ss, err := h.model.sites(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, ss)
	return nil
}

func (h *Handler) createSite(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermAITestsManage)
	if !ok || err != nil {
		return err
	}
	if workspace.DenyGuest(w, ws) {
		return nil
	}
	base, host, msg := checkBaseURL(r.FormValue("base_url"), h.opts.AllowHTTP, h.opts.AllowPrivate)
	if !web.Check(w, map[string]string{"base_url": msg}) {
		return nil
	}
	s, err := h.model.createSite(r.Context(), ws.ID, ws.Slug, userID, base, host)
	if errors.Is(err, errTaken) {
		web.Fields(w, http.StatusConflict, map[string]string{"base_url": "This site is already added."})
		return nil
	}
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, s)
	return nil
}

func (h *Handler) deleteSite(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermAITestsManage)
	if !ok || err != nil {
		return err
	}
	if workspace.DenyGuest(w, ws) {
		return nil
	}
	err = errNotFound
	if id := r.PathValue("id"); web.IsUUID(id) {
		err = h.model.deleteSite(r.Context(), ws.ID, ws.Slug, userID, id)
	}
	return noContent(w, err, "Test site not found.")
}

// --- accounts ---

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	if workspace.DenyGuest(w, ws) {
		return nil
	}
	as, err := h.model.accounts(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, as)
	return nil
}

const errChooseSite = "Choose one of this workspace's test sites."

// saveAccount creates an account (POST /test-accounts) or updates one (POST /test-accounts/{id}).
func (h *Handler) saveAccount(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermAITestsManage)
	if !ok || err != nil {
		return err
	}
	if workspace.DenyGuest(w, ws) {
		return nil
	}
	id := r.PathValue("id")
	if id != "" && !web.IsUUID(id) {
		web.Err(w, http.StatusNotFound, errAccountNotFound)
		return nil
	}
	label, username, password := strings.TrimSpace(r.FormValue("label")), strings.TrimSpace(r.FormValue("username")), r.FormValue("password")
	siteID := strings.TrimSpace(r.FormValue("site_id"))
	errs := checkAccount(label, username, password, id == "")
	if siteID != "" && !web.IsUUID(siteID) {
		errs["site_id"] = errChooseSite
	}
	if !web.Check(w, errs) {
		return nil
	}
	a, err := h.model.saveAccount(r.Context(), h.opts.Key, ws.ID, ws.Slug, userID, id, label, username, password, siteID)
	switch {
	case errors.Is(err, errSiteNotFound):
		web.Fields(w, http.StatusBadRequest, map[string]string{"site_id": errChooseSite})
	case errors.Is(err, errTaken):
		web.Fields(w, http.StatusConflict, map[string]string{"label": "An account with this label already exists."})
	case errors.Is(err, errNotFound):
		web.Err(w, http.StatusNotFound, errAccountNotFound)
	case err != nil:
		return err
	case id == "":
		web.JSON(w, http.StatusCreated, a)
	default:
		web.JSON(w, http.StatusOK, a)
	}
	return nil
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermAITestsManage)
	if !ok || err != nil {
		return err
	}
	if workspace.DenyGuest(w, ws) {
		return nil
	}
	err = errNotFound
	if id := r.PathValue("id"); web.IsUUID(id) {
		err = h.model.deleteAccount(r.Context(), ws.ID, ws.Slug, userID, id)
	}
	return noContent(w, err, errAccountNotFound)
}

// noContent writes a 204, or a 404 with notFound for errNotFound; other errors are returned.
func noContent(w http.ResponseWriter, err error, notFound string) error {
	switch {
	case errors.Is(err, errNotFound):
		web.Err(w, http.StatusNotFound, notFound)
	case err != nil:
		return err
	default:
		w.WriteHeader(http.StatusNoContent)
	}
	return nil
}
