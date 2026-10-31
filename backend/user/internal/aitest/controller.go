package aitest

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/doc"
	"github.com/devender07/tether/backend/user/internal/sysconfig"
	"github.com/devender07/tether/backend/user/internal/workspace"
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
)

type Handler struct {
	model        *model
	cfg          *sysconfig.Store
	opts         Options
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
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
	return &Handler{model: &model{db: db}, cfg: cfg, opts: opts, authenticate: authenticate, member: member}
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
	writeJSON(w, http.StatusOK, cs)
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
		writeErr(w, http.StatusConflict, "An issue can have at most 50 test cases.")
		return nil
	}
	if err != nil {
		return err
	}
	return h.respondCase(w, r, http.StatusCreated, ws.ID, id)
}

func (h *Handler) updateCase(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	id := r.PathValue("id")
	if !isUUID(id) {
		writeErr(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	f, ok := readCase(w, r)
	if !ok {
		return nil
	}
	err = h.model.updateCase(r.Context(), ws.ID, id, userID, f)
	if errors.Is(err, errNotFound) {
		writeErr(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	if err != nil {
		return err
	}
	return h.respondCase(w, r, http.StatusOK, ws.ID, id)
}

func (h *Handler) deleteCase(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	id := r.PathValue("id")
	if isUUID(id) {
		err = h.model.deleteCase(r.Context(), ws.ID, id, userID)
	} else {
		err = errNotFound
	}
	if errors.Is(err, errNotFound) {
		writeErr(w, http.StatusNotFound, errCaseNotFound)
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
		if !isUUID(id) {
			err = errBadOrder
		}
	}
	if err == nil {
		err = h.model.orderCases(r.Context(), i.id, userID, ids)
	}
	if errors.Is(err, errBadOrder) {
		writeErr(w, http.StatusBadRequest, "The list changed. Reload and try again.")
		return nil
	}
	if err != nil {
		return err
	}
	cs, err := h.model.cases(r.Context(), i.id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, cs)
	return nil
}

// readCase returns the form's cleaned case, or writes a 400 with field errors and returns ok=false.
func readCase(w http.ResponseWriter, r *http.Request) (caseFields, bool) {
	_ = r.ParseMultipartForm(maxBody)
	f := cleanCase(caseFields{Title: r.PostForm.Get("title"), Steps: r.PostForm["steps"], Expected: r.PostForm.Get("expected")})
	return f, checkFields(w, checkCase(f))
}

func (h *Handler) respondCase(w http.ResponseWriter, r *http.Request, status int, workspaceID, id string) error {
	c, err := h.model.testCase(r.Context(), workspaceID, id)
	if err != nil {
		return err
	}
	if c == nil {
		writeErr(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	writeJSON(w, status, c)
	return nil
}

// --- runs ---

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request, userID string) error {
	_, i, ok, err := h.issue(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	rs, err := h.model.runs(r.Context(), i.id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rs)
	return nil
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	return h.respondRun(w, r, http.StatusOK, ws.ID, r.PathValue("id"))
}

// live proxies the tester's MJPEG stream of a running AI run's browser to a workspace member.
func (h *Handler) live(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	id := r.PathValue("id")
	tester := ""
	if isUUID(id) {
		if tester, err = h.model.liveTester(r.Context(), ws.ID, id); err != nil {
			return err
		}
	}
	target, err := url.Parse(tester)
	if tester == "" || err != nil {
		writeErr(w, http.StatusNotFound, "This test run isn't running.")
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
			writeErr(w, http.StatusBadGateway, "The live view isn't available. Try again.")
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
	}
	if _, _, ok, err := h.issue(w, r, userID, ""); !ok || err != nil {
		return err
	}
	writeFields(w, http.StatusBadRequest, map[string]string{"kind": "Choose ai or manual."})
	return nil
}

func (h *Handler) createManualRun(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	id, err := h.model.createManual(r.Context(), ws.ID, i.id, userID)
	if errors.Is(err, errNoCases) {
		writeErr(w, http.StatusConflict, "Add a test case first.")
		return nil
	}
	if err != nil {
		return err
	}
	return h.respondRun(w, r, http.StatusCreated, ws.ID, id)
}

func (h *Handler) markResult(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermTestsManage)
	if !ok || err != nil {
		return err
	}
	runID, resultID := r.PathValue("id"), r.PathValue("result_id")
	if !isUUID(runID) {
		writeErr(w, http.StatusNotFound, errRunNotFound)
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
	if !checkFields(w, errs) {
		return nil
	}
	var notePtr *string
	if note != "" {
		notePtr = &note
	}
	if isUUID(resultID) {
		err = h.model.mark(r.Context(), ws.ID, runID, resultID, userID, status, notePtr)
	} else {
		err = errNotFound
	}
	switch {
	case errors.Is(err, errNotFound):
		writeErr(w, http.StatusNotFound, errResultNotFound)
	case errors.Is(err, errAIRun):
		writeErr(w, http.StatusConflict, "AI runs are marked by the test runner.")
	case errors.Is(err, errFinished):
		writeErr(w, http.StatusConflict, errRunFinished)
	case err != nil:
		return err
	default:
		return h.respondRun(w, r, http.StatusOK, ws.ID, runID)
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
	if isUUID(id) {
		if kind, err = h.model.runKind(r.Context(), ws.ID, id); err != nil {
			return err
		}
	}
	perm := workspace.PermTestsManage
	if kind == "ai" {
		perm = workspace.PermAITestsRun
	}
	switch {
	case kind == "":
		writeErr(w, http.StatusNotFound, errRunNotFound)
		return nil
	case !workspace.Has(ws.Permissions, perm):
		writeErr(w, http.StatusForbidden, errForbidden)
		return nil
	}
	err = h.model.cancel(r.Context(), ws.ID, id, userID)
	switch {
	case errors.Is(err, errNotFound):
		writeErr(w, http.StatusNotFound, errRunNotFound)
	case errors.Is(err, errFinished):
		writeErr(w, http.StatusConflict, errRunFinished)
	case err != nil:
		return err
	default:
		return h.respondRun(w, r, http.StatusOK, ws.ID, id)
	}
	return nil
}

func (h *Handler) respondRun(w http.ResponseWriter, r *http.Request, status int, workspaceID, id string) error {
	var d *RunDetail
	var err error
	if isUUID(id) {
		if d, err = h.model.run(r.Context(), workspaceID, id); err != nil {
			return err
		}
	}
	if d == nil {
		writeErr(w, http.StatusNotFound, errRunNotFound)
		return nil
	}
	writeJSON(w, status, d)
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
	if key, number, ok := parseIdentifier(r.PathValue("identifier")); ok {
		if i, err = h.model.issue(r.Context(), ws.ID, key, number); err != nil {
			return nil, nil, false, err
		}
	}
	switch {
	case i == nil:
		writeErr(w, http.StatusNotFound, errIssueNotFound)
		return nil, nil, false, nil
	case perm != "" && !workspace.Has(ws.Permissions, perm):
		writeErr(w, http.StatusForbidden, errForbidden)
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
		writeErr(w, http.StatusNotFound, errWorkspaceNotFound)
		return nil, false, nil
	case perm != "" && !workspace.Has(ws.Permissions, perm):
		writeErr(w, http.StatusForbidden, errForbidden)
		return nil, false, nil
	}
	return ws, true, nil
}

// checkFields drops empty messages and writes a 400 with the rest, returning false when there were any.
func checkFields(w http.ResponseWriter, msgs map[string]string) bool {
	maps.DeleteFunc(msgs, func(_, msg string) bool { return msg == "" })
	if len(msgs) > 0 {
		writeFields(w, http.StatusBadRequest, msgs)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeFields(w http.ResponseWriter, status int, errs map[string]string) {
	writeJSON(w, status, map[string]map[string]string{"errors": errs})
}

// --- AI runs ---

func (h *Handler) createAIRun(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, i, ok, err := h.issue(w, r, userID, workspace.PermAITestsRun)
	if !ok || err != nil {
		return err
	}
	alive, err := h.model.testerAlive(r.Context())
	if err != nil {
		return err
	}
	if !alive {
		writeErr(w, http.StatusServiceUnavailable, "AI testing isn't set up on this server.")
		return nil
	}
	limit, err := limitUSD(r.Context(), h.cfg)
	if err != nil {
		return err
	}
	caseID := r.PostForm.Get("case_id")
	if caseID != "" && !isUUID(caseID) {
		writeErr(w, http.StatusNotFound, errCaseNotFound)
		return nil
	}
	id, err := h.model.createAI(r.Context(), ws.ID, i.id, userID, caseID, caseID == "" && r.PostForm.Get("regenerate") == "true", int64(limit*1e6))
	switch {
	case errors.Is(err, errNotFound):
		writeErr(w, http.StatusNotFound, errCaseNotFound)
	case errors.Is(err, errNoSites):
		writeErr(w, http.StatusConflict, "Add a test site in Settings → AI testing first.")
	case errors.Is(err, errLimit):
		writeErr(w, http.StatusConflict, msgLimit)
	case errors.Is(err, errRunActive):
		writeErr(w, http.StatusConflict, "A test is already running on this issue.")
	case err != nil:
		return err
	default:
		return h.respondRun(w, r, http.StatusCreated, ws.ID, id)
	}
	return nil
}

// limitUSD is ai_test_monthly_limit_usd; a missing or broken value means no AI spend.
func limitUSD(ctx context.Context, cfg *sysconfig.Store) (float64, error) {
	v, err := cfg.Get(ctx, "ai_test_monthly_limit_usd")
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return 0, nil
	}
	return f, nil
}

func (h *Handler) usage(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	n, err := spent(r.Context(), h.model.db, ws.ID)
	if err != nil {
		return err
	}
	limit, err := limitUSD(r.Context(), h.cfg)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"spent_usd": float64(n) / 1e6, "limit_usd": limit,
		"resets_at": monthStart(time.Now()).AddDate(0, 1, 0)})
	return nil
}

// --- sites ---

func (h *Handler) listSites(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	ss, err := h.model.sites(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ss)
	return nil
}

func (h *Handler) createSite(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermAITestsManage)
	if !ok || err != nil {
		return err
	}
	base, host, msg := checkBaseURL(r.FormValue("base_url"), h.opts.AllowHTTP, h.opts.AllowPrivate)
	if !checkFields(w, map[string]string{"base_url": msg}) {
		return nil
	}
	s, err := h.model.createSite(r.Context(), ws.ID, ws.Slug, userID, base, host)
	if errors.Is(err, errTaken) {
		writeFields(w, http.StatusConflict, map[string]string{"base_url": "This site is already added."})
		return nil
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, s)
	return nil
}

func (h *Handler) deleteSite(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermAITestsManage)
	if !ok || err != nil {
		return err
	}
	err = errNotFound
	if id := r.PathValue("id"); isUUID(id) {
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
	as, err := h.model.accounts(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, as)
	return nil
}

// saveAccount creates an account (POST /test-accounts) or updates one (POST /test-accounts/{id}).
func (h *Handler) saveAccount(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermAITestsManage)
	if !ok || err != nil {
		return err
	}
	id := r.PathValue("id")
	if id != "" && !isUUID(id) {
		writeErr(w, http.StatusNotFound, errAccountNotFound)
		return nil
	}
	label, username, password := strings.TrimSpace(r.FormValue("label")), strings.TrimSpace(r.FormValue("username")), r.FormValue("password")
	if !checkFields(w, checkAccount(label, username, password, id == "")) {
		return nil
	}
	a, err := h.model.saveAccount(r.Context(), h.opts.Key, ws.ID, ws.Slug, userID, id, label, username, password)
	switch {
	case errors.Is(err, errTaken):
		writeFields(w, http.StatusConflict, map[string]string{"label": "An account with this label already exists."})
	case errors.Is(err, errNotFound):
		writeErr(w, http.StatusNotFound, errAccountNotFound)
	case err != nil:
		return err
	case id == "":
		writeJSON(w, http.StatusCreated, a)
	default:
		writeJSON(w, http.StatusOK, a)
	}
	return nil
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermAITestsManage)
	if !ok || err != nil {
		return err
	}
	err = errNotFound
	if id := r.PathValue("id"); isUUID(id) {
		err = h.model.deleteAccount(r.Context(), ws.ID, ws.Slug, userID, id)
	}
	return noContent(w, err, errAccountNotFound)
}

// noContent writes a 204, or a 404 with notFound for errNotFound; other errors are returned.
func noContent(w http.ResponseWriter, err error, notFound string) error {
	switch {
	case errors.Is(err, errNotFound):
		writeErr(w, http.StatusNotFound, notFound)
	case err != nil:
		return err
	default:
		w.WriteHeader(http.StatusNoContent)
	}
	return nil
}
