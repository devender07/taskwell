package aitest

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/devender07/tether/backend/user/internal/doc"
)

// Browser sessions: an MCP client drives the tester's browser one action at a time, the tester records each
// action that works as a replayable step, and the client saves the steps on a test case (browser-sessions-plan.md).
// Sessions live in the tester's memory and are served on its internal port, next to the live view.

const (
	sessionIdle   = 10 * time.Minute
	sessionMax    = 30 * time.Minute
	sessionHeader = "X-Tether-Session-Token"
	actionTimeout = time.Minute
	maxPageText   = 4000

	msgSessionEnded = "This browser session ended. Open a new one."
	msgBrowserBusy  = "The test browser is busy. Try again in a minute."
	msgSessionURL   = "Use one of this workspace's test sites."
)

type session struct {
	id, token string
	j         *job // the workspace's sites and accounts; runID is the session id, which the proxy knows it by
	created   time.Time
	used      time.Time // guarded by Runner.sessMu

	mu       sync.Mutex // one action at a time
	pg       *page
	closed   bool
	steps    []scriptStep
	elements []string // the page state's, to describe a ref
}

// sessionAction is a request to POST /sessions/{id}/actions.
type sessionAction struct {
	Action  string `json:"action"`
	Failing bool   `json:"failing"` // an assertion that's expected to fail until a bug is fixed: recorded anyway
	toolInput
}

type sessionPage struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Elements string `json:"elements"` // numbered interactive elements and the visible text, as AI runs give Claude
}

// sessionAnswer is the page after an action, the action's failure (nothing recorded) and the steps so far.
type sessionAnswer struct {
	ID    string       `json:"id,omitempty"`
	Token string       `json:"token,omitempty"`
	Page  sessionPage  `json:"page"`
	Error string       `json:"error,omitempty"`
	Steps []scriptStep `json:"steps"`
}

// RegisterSessions mounts the browser session API on the tester's internal mux. The user API calls it for the
// session's owner; every call but POST /sessions needs the session's token.
func (r *Runner) RegisterSessions(mux *http.ServeMux) {
	mux.HandleFunc("POST /sessions", r.openSession)
	mux.HandleFunc("POST /sessions/{id}/actions", r.sessionAct)
	mux.HandleFunc("GET /sessions/{id}/screenshot", r.sessionScreenshot)
	mux.HandleFunc("POST /sessions/{id}/close", r.sessionClose)
}

// openSession opens a page for the workspace on a browser slot, through the proxy with the workspace's sites, and
// navigates it to url (recorded as the first step).
func (r *Runner) openSession(w http.ResponseWriter, req *http.Request) {
	var in struct {
		WorkspaceID string `json:"workspace_id"`
		URL         string `json:"url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, maxBody)).Decode(&in); err != nil || !isUUID(in.WorkspaceID) {
		writeErr(w, http.StatusBadRequest, "Send workspace_id and url.")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), actionTimeout)
	defer cancel()
	j := &job{runID: newUUID(), wsID: in.WorkspaceID}
	var err error
	if j.sites, err = r.model.sites(ctx, j.wsID); err == nil {
		j.accounts, err = r.model.accountRefs(ctx, j.wsID)
	}
	if err != nil {
		r.failed(w, http.StatusInternalServerError, "", err)
		return
	}
	hosts := siteHosts(j.sites)
	if u, err := url.Parse(in.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || !allowedHost(u.Hostname(), hosts) {
		writeErr(w, http.StatusBadRequest, msgSessionURL)
		return
	}
	room, err := r.model.sessionRoom(ctx, j.wsID, r.workspaceCap(ctx))
	if err != nil {
		r.failed(w, http.StatusInternalServerError, "", err)
		return
	}
	if !room || !r.takeSlot() {
		writeErr(w, http.StatusConflict, msgBrowserBusy)
		return
	}
	now := time.Now()
	s := &session{id: j.runID, token: rand.Text(), j: j, created: now, used: now, steps: []scriptStep{}}
	j.token = rand.Text() // the proxy's, never leaves the tester
	r.o.Proxy.open(s.id, j.token, hosts)
	c, err := r.browser(ctx)
	if err == nil {
		s.pg, err = c.newPage(ctx, r.o.ProxyURL, s.id, j.token, r.o.IgnoreCertErrors, func([]byte) {})
	}
	if err != nil {
		r.o.Proxy.close(s.id)
		<-r.slots
		r.failed(w, http.StatusInternalServerError, s.id, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := r.step(ctx, s, sessionAction{Action: "navigate", toolInput: toolInput{URL: in.URL}})
	if err == nil {
		r.sessMu.Lock()
		if r.stopping {
			err = errStopping
		} else {
			r.sessions[s.id] = s
		}
		r.sessMu.Unlock()
	}
	if err != nil {
		r.end(s)
		r.failed(w, http.StatusInternalServerError, s.id, err)
		return
	}
	slog.Info("browser session opened", "session", s.id, "workspace", j.wsID)
	a.ID, a.Token = s.id, s.token
	writeJSON(w, http.StatusCreated, a)
}

func (r *Runner) sessionAct(w http.ResponseWriter, req *http.Request) {
	s := r.lookup(w, req)
	if s == nil {
		return
	}
	var a sessionAction
	if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, maxBody)).Decode(&a); err != nil {
		writeErr(w, http.StatusBadRequest, "Send the action as JSON.")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), actionTimeout)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		writeErr(w, http.StatusNotFound, msgSessionEnded)
		return
	}
	slog.Info("browser session step", "session", s.id, "action", a.Action, "ref", a.Ref) // never the typed text
	ans, err := r.step(ctx, s, a)
	if err != nil {
		r.drop(s.id)
		r.end(s)
		r.failed(w, http.StatusNotFound, s.id, err)
		return
	}
	writeJSON(w, http.StatusOK, ans)
}

func (r *Runner) sessionScreenshot(w http.ResponseWriter, req *http.Request) {
	s := r.lookup(w, req)
	if s == nil {
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), actionTimeout)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		writeErr(w, http.StatusNotFound, msgSessionEnded)
		return
	}
	shot, err := s.pg.screenshot(ctx)
	if err != nil {
		r.drop(s.id)
		r.end(s)
		r.failed(w, http.StatusNotFound, s.id, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	_, _ = w.Write(shot)
}

// sessionClose ends the session and answers its steps.
func (r *Runner) sessionClose(w http.ResponseWriter, req *http.Request) {
	s := r.lookup(w, req)
	if s == nil {
		return
	}
	if r.drop(s.id) == nil { // a sweep ended it meanwhile
		writeErr(w, http.StatusNotFound, msgSessionEnded)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r.end(s)
	writeJSON(w, http.StatusOK, map[string][]scriptStep{"steps": s.steps})
}

// failed logs err and answers status: 404 says the session ended (its browser broke), anything else that it
// couldn't open. A broken Chrome connection is dropped so the next session dials a new one.
func (r *Runner) failed(w http.ResponseWriter, status int, id string, err error) {
	slog.Error("browser session", "session", id, "err", err)
	r.mu.Lock()
	if r.chrome != nil && r.chrome.broken() != nil {
		r.chrome = nil
	}
	r.mu.Unlock()
	if status == http.StatusNotFound {
		writeErr(w, status, msgSessionEnded)
		return
	}
	writeErr(w, status, "Something went wrong. Try again.")
}

// step runs one action on the session's page (the caller holds s.mu) and records its step when it worked. An action
// that fails answers its error and records nothing; err is a broken browser.
func (r *Runner) step(ctx context.Context, s *session, a sessionAction) (sessionAnswer, error) {
	var ans sessionAnswer
	var notes []string
	switch a.Action {
	case "snapshot":
	case "assert_text", "assert_no_text":
		text := strings.TrimSpace(a.Text)
		js, msg := hasTextJS(text), fmt.Sprintf("Expected %q on the page.", text)
		if a.Action == "assert_no_text" {
			js, msg = noTextJS(text), fmt.Sprintf("Expected %q not to be on the page.", text)
		}
		if text == "" || doc.Len(text) > maxCheckLen {
			ans.Error = "Give the text to look for, up to 200 characters."
			break
		}
		found, err := s.pg.waitFor(ctx, js, 5*time.Second)
		switch {
		case err != nil:
			return ans, err
		case !found && !a.Failing:
			ans.Error = msg
		case len(s.steps) >= maxScriptSteps:
			ans.Error = "A case records up to 60 steps. Save it."
		default:
			s.steps = append(s.steps, scriptStep{Tool: a.Action, Text: text})
			if !found {
				notes = append(notes, "Recorded as currently failing: "+msg+" Replays fail here until it holds.")
			}
		}
	default:
		if len(s.steps) >= maxScriptSteps {
			ans.Error = "A case records up to 60 steps. Save it."
			break
		}
		onElement := a.Action == "click" || a.Action == "type" || a.Action == "select" || a.Action == "fill_password"
		step := scriptStep{Tool: a.Action, URL: a.URL, Text: a.Text, Value: a.Value, Key: a.Key, Account: a.Account}
		if a.Action == "wait" {
			step.Seconds = min(max(a.Seconds, 1), 5)
		}
		if onElement {
			step.Target = s.pg.locate(ctx, a.Ref)
		}
		note, _, err := r.act(ctx, s.pg, s.j, a.Action, a.toolInput, s.elements)
		switch {
		case err != nil:
			return ans, err
		case note != "":
			ans.Error = note
		case onElement && step.Target == nil:
			ans.Error = fmt.Sprintf("No element [%d] on the page. Use a ref from the latest page.", a.Ref)
		default:
			s.steps = append(s.steps, step)
		}
	}
	seen := map[string]bool{}
	for _, host := range r.o.Proxy.takeBlocked(s.id) {
		if !seen[host] && s.pg.asked(host) {
			seen[host] = true
			notes = append(notes, "Blocked: "+host+" isn't a registered test site.")
		}
	}
	st, err := s.pg.state(ctx)
	if err != nil && s.pg.c.broken() != nil {
		return ans, err
	}
	s.elements = st.Elements
	if t := []rune(st.Text); len(t) > maxPageText {
		st.Text = string(t[:maxPageText])
	}
	ans.Page = sessionPage{URL: st.URL, Title: st.Title, Elements: stateText(strings.Join(notes, "\n"), st)}
	ans.Steps = s.steps
	return ans, nil
}

// lookup returns the path's session when the request carries its token, marking it used; otherwise it answers 404.
func (r *Runner) lookup(w http.ResponseWriter, req *http.Request) *session {
	r.sessMu.Lock()
	s := r.sessions[req.PathValue("id")]
	if s != nil && subtle.ConstantTimeCompare([]byte(req.Header.Get(sessionHeader)), []byte(s.token)) == 1 {
		s.used = time.Now()
	} else {
		s = nil
	}
	r.sessMu.Unlock()
	if s == nil {
		writeErr(w, http.StatusNotFound, msgSessionEnded)
	}
	return s
}

// drop takes the session out of the list, so no new request reaches it; nil when it's gone already.
func (r *Runner) drop(id string) *session {
	r.sessMu.Lock()
	defer r.sessMu.Unlock()
	s := r.sessions[id]
	delete(r.sessions, id)
	return s
}

// end closes the session's page and frees its slot (the caller holds s.mu, and dropped it from the list).
func (r *Runner) end(s *session) {
	if s.closed {
		return
	}
	s.closed = true
	s.pg.close()
	r.o.Proxy.close(s.id)
	<-r.slots
	if err := r.model.closeSession(context.Background(), s.id); err != nil {
		slog.Error("browser session", "session", s.id, "err", err)
	}
	slog.Info("browser session ended", "session", s.id, "steps", len(s.steps))
}

// sweep ends the sessions idle for sessionIdle or open for sessionMax at now.
func (r *Runner) sweep(now time.Time) {
	r.sessMu.Lock()
	var ended []*session
	for id, s := range r.sessions {
		if now.Sub(s.used) >= sessionIdle || now.Sub(s.created) >= sessionMax {
			ended = append(ended, s)
			delete(r.sessions, id)
		}
	}
	r.sessMu.Unlock()
	for _, s := range ended {
		s.mu.Lock()
		r.end(s)
		s.mu.Unlock()
	}
}

// endSessions ends every session and refuses new ones, as the tester stops.
func (r *Runner) endSessions() {
	r.sessMu.Lock()
	r.stopping = true
	r.sessMu.Unlock()
	r.sweep(time.Now().Add(sessionMax))
}

// newUUID is a random (version 4) UUID.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // never fails
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// siteHosts is the sites' hosts, which pages may visit.
func siteHosts(sites []Site) []string {
	hosts := make([]string, len(sites))
	for i, s := range sites {
		hosts[i] = s.Host
	}
	return hosts
}
