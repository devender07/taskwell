package issue

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/devender07/taskwell/backend/user/internal/doc"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// Imports bring issues into a project from a CSV export (Linear, Jira or any CSV with a title column) or a GitHub
// repository. A dry run maps them and answers the summary; the import itself is one transaction (model.importIssues).

const (
	maxImportFile   = 10 << 20
	maxImportRows   = 5000
	maxGitHubIssues = 2000
	// defaultLabelColor is the app's default for a new label (frontend workflow/labels.tsx).
	defaultLabelColor = "#5e6ad2"
	// jiraTime is how Jira's CSV export writes times.
	jiraTime = "02/Jan/06 3:04 PM"
)

// githubAPI is GitHub's REST API; tests point it at an httptest server.
var githubAPI = "https://api.github.com"

var repoRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$`)

// importError is a failure the client is told about: a field's message, or the request's when field is "".
type importError struct {
	status     int
	field, msg string
}

func (e *importError) Error() string { return e.msg }

var (
	errNoTitleColumn = &importError{http.StatusBadRequest, "file", "No title column found. Name one Title or Summary."}
	errBadCSV        = &importError{http.StatusBadRequest, "file", "This file isn't a valid CSV."}
	errTooManyRows   = &importError{http.StatusBadRequest, "file", "Over 5,000 rows. Split the file."}
	errNoRepo        = &importError{http.StatusBadRequest, "repo", "Repository not found or not accessible."}
	errBadToken      = &importError{http.StatusBadRequest, "token", "This token isn't valid."}
	errRateLimit     = &importError{http.StatusTooManyRequests, "", "GitHub's rate limit was hit. Add a token or try again later."}
	errGitHubDown    = &importError{http.StatusBadGateway, "", "GitHub didn't answer. Try again."}
)

// sourceIssue is an issue as a CSV row or GitHub has it, before it's mapped onto the workspace.
type sourceIssue struct {
	row                                          int // the 1-based CSV data row, or the GitHub issue number
	title, body, status, priority, assignee, due string
	// category is the status category GitHub's state maps to ("default": the default status); "" maps status by name.
	category string
	footer   string // "Imported from …", appended to the description
	labels   []string
	comments []sourceComment
	// commentCount counts comments that weren't fetched (a GitHub dry run).
	commentCount int
}

type sourceComment struct{ author, date, body string }

// summary is what an import did or, on a dry run, would do.
type summary struct {
	Source             string      `json:"source"`
	Total              int         `json:"total"`
	Statuses           []statusMap `json:"statuses"`
	NewLabels          []string    `json:"new_labels"`
	UnmatchedAssignees []string    `json:"unmatched_assignees"`
	Comments           int         `json:"comments"`
	Skipped            []skipped   `json:"skipped"`
	Imported           *int        `json:"imported,omitempty"`
	First              string      `json:"first,omitempty"`
}

type statusMap struct {
	From  string `json:"from"`
	To    Person `json:"to"`
	Count int    `json:"count"`
}

type skipped struct {
	Row   int    `json:"row"`
	Error string `json:"error"`
}

// plannedIssue is a mapped issue, ready to insert.
type plannedIssue struct {
	title       string
	description json.RawMessage
	statusID    string
	assigneeID  *string
	priority    int
	due         *string
	labels      []string // names; new ones are created
	comments    []string // documents
}

// importRefs are the workspace's statuses (by position), labels (lowercase name → id) and the members who can see
// the project.
type importRefs struct {
	statuses []Status
	labels   map[string]string
	members  []importMember
}

type importMember struct{ id, email, name string } // email and name lowercase

// --- handlers ---

func (h *Handler) importCSV(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermIssuesCreate)
	if !ok || err != nil {
		return err
	}
	err = r.ParseMultipartForm(1 << 20) // the file spills to disk past 1 MB
	errs := map[string]string{}
	file, header, ferr := r.FormFile("file")
	switch {
	case errors.As(err, new(*http.MaxBytesError)) || ferr == nil && header.Size > maxImportFile:
		errs["file"] = "The file is over 10 MB."
	case ferr != nil:
		errs["file"] = "Choose a CSV file."
	}
	if ferr == nil {
		defer file.Close()
	}
	key := normKey(r.PostFormValue("project"))
	if key == "" {
		errs["project"] = "Choose a project."
	}
	if !web.Check(w, errs) {
		return nil
	}
	projectID, ok, err := h.importProject(w, r, ws.ID, key, userID)
	if !ok || err != nil {
		return err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read upload: %w", err)
	}
	source, issues, err := parseCSV(data)
	if err != nil {
		return writeImportError(w, err)
	}
	return h.runImport(w, r, ws, userID, projectID, source, issues, r.PostFormValue("dry_run") == "1")
}

func (h *Handler) importGitHub(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, workspace.PermIssuesCreate)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	repo, okRepo := parseRepo(r.PostForm.Get("repo"))
	key := normKey(r.PostForm.Get("project"))
	errs := map[string]string{}
	if !okRepo {
		errs["repo"] = "Enter a repository like owner/name."
	}
	if key == "" {
		errs["project"] = "Choose a project."
	}
	if !web.Check(w, errs) {
		return nil
	}
	projectID, ok, err := h.importProject(w, r, ws.ID, key, userID)
	if !ok || err != nil {
		return err
	}
	dryRun := r.PostForm.Get("dry_run") == "1"
	issues, err := fetchGitHub(r.Context(), repo, strings.TrimSpace(r.PostForm.Get("token")), r.PostForm.Get("closed") == "1", !dryRun)
	if err != nil {
		return writeImportError(w, err)
	}
	return h.runImport(w, r, ws, userID, projectID, "github", issues, dryRun)
}

// importProject returns the id of the project with the key, or writes a 404 when the caller can't see it or a 409
// when it's archived and returns ok=false.
func (h *Handler) importProject(w http.ResponseWriter, r *http.Request, workspaceID, key, userID string) (string, bool, error) {
	id, archived, err := h.model.importProject(r.Context(), workspaceID, key, userID)
	switch {
	case err != nil:
		return "", false, err
	case id == "":
		web.Err(w, http.StatusNotFound, errProjectNotFound)
		return "", false, nil
	case archived:
		web.Err(w, http.StatusConflict, errArchivedProject)
		return "", false, nil
	}
	return id, true, nil
}

// runImport maps the issues onto the workspace and answers the summary: 200 on a dry run, else 201 once imported.
func (h *Handler) runImport(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, userID, projectID, source string,
	issues []sourceIssue, dryRun bool) error {
	refs, err := h.model.importRefs(r.Context(), ws.ID, projectID)
	if err != nil {
		return err
	}
	s, planned := planImport(source, issues, refs)
	if len(planned) == 0 {
		web.Err(w, http.StatusBadRequest, "Nothing to import.")
		return nil
	}
	if dryRun {
		web.JSON(w, http.StatusOK, s)
		return nil
	}
	first, err := h.model.importIssues(r.Context(), ws.ID, projectID, userID, planned, s.NewLabels)
	if errors.Is(err, errArchived) {
		web.Err(w, http.StatusConflict, errArchivedProject)
		return nil
	}
	if err != nil {
		return err
	}
	n := len(planned)
	s.Imported, s.First = &n, first
	web.JSON(w, http.StatusCreated, s)
	return nil
}

// writeImportError answers an importError; any other error is returned for a 500.
func writeImportError(w http.ResponseWriter, err error) error {
	var ie *importError
	if !errors.As(err, &ie) {
		return err
	}
	if ie.field != "" {
		web.Fields(w, ie.status, map[string]string{ie.field: ie.msg})
	} else {
		web.Err(w, ie.status, ie.msg)
	}
	return nil
}

// --- CSV ---

// parseCSV reads a CSV export: its source ("linear", "jira" or "csv") and one issue per data row. Headers match
// case-insensitively and repeated columns are all read.
func parseCSV(data []byte) (string, []sourceIssue, error) {
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", nil, errBadCSV
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\uFEFF"))))
	r.LazyQuotes, r.FieldsPerRecord = true, -1
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return "", nil, errNoTitleColumn
	}
	if err != nil {
		return "", nil, errBadCSV
	}
	cols := map[string][]int{}
	for i, h := range header {
		k := strings.ToLower(strings.TrimSpace(h))
		cols[k] = append(cols[k], i)
	}
	if len(cols["title"])+len(cols["summary"])+len(cols["name"]) == 0 {
		return "", nil, errNoTitleColumn
	}
	source, label := "csv", "CSV"
	switch {
	case cols["issue key"] != nil && cols["summary"] != nil:
		source, label = "jira", "Jira"
	case cols["id"] != nil && cols["title"] != nil && cols["team"] != nil:
		source, label = "linear", "Linear"
	}
	var issues []sourceIssue
	for n := 1; ; n++ {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, errBadCSV
		}
		if n > maxImportRows {
			return "", nil, errTooManyRows
		}
		// all is every non-empty cell of the columns, in their order; first is the first of them.
		all := func(names ...string) []string {
			var out []string
			for _, name := range names {
				for _, i := range cols[name] {
					if i < len(rec) && strings.TrimSpace(rec[i]) != "" {
						out = append(out, strings.TrimSpace(rec[i]))
					}
				}
			}
			return out
		}
		first := func(names ...string) string {
			if vs := all(names...); len(vs) > 0 {
				return vs[0]
			}
			return ""
		}
		s := sourceIssue{row: n, title: first("title", "summary", "name"), body: first("description", "body"),
			status: first("status"), priority: first("priority"), assignee: first("assignee", "assignee email"),
			due: first("due date", "due")}
		if ref := first("issue key", "id"); ref != "" {
			s.footer = "Imported from " + label + " " + ref + "."
		}
		for _, cell := range all("labels", "label") {
			s.labels = append(s.labels, strings.Split(cell, ",")...)
		}
		for _, cell := range all("comment") {
			s.comments = append(s.comments, parseComment(cell))
		}
		issues = append(issues, s)
	}
	return source, issues, nil
}

// parseComment reads a comment cell: Jira's "date;author;body", else the whole cell is the body.
func parseComment(cell string) sourceComment {
	if parts := strings.SplitN(cell, ";", 3); len(parts) == 3 {
		if _, err := time.Parse(jiraTime, strings.TrimSpace(parts[0])); err == nil {
			return sourceComment{author: strings.TrimSpace(parts[1]), date: strings.TrimSpace(parts[0]), body: parts[2]}
		}
	}
	return sourceComment{body: cell}
}

// --- GitHub ---

type ghIssue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	State       string `json:"state"`
	StateReason string `json:"state_reason"`
	Labels      []struct{ Name string }
	Comments    int             `json:"comments"`
	PullRequest json.RawMessage `json:"pull_request"`
}

type ghComment struct {
	Body      string `json:"body"`
	User      struct{ Login string }
	CreatedAt time.Time `json:"created_at"`
}

// parseRepo returns "owner/name" from that or a github.com URL; ok=false when it's neither.
func parseRepo(s string) (string, bool) {
	s = strings.TrimSpace(s)
	for _, p := range []string{"https://", "http://", "www.", "github.com/"} {
		s = strings.TrimPrefix(s, p)
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	name := strings.TrimSuffix(parts[1], ".git")
	repo := parts[0] + "/" + name
	return repo, repoRe.MatchString(repo) && name != "." && name != ".."
}

// fetchGitHub reads up to maxGitHubIssues of the repository's open issues (all with closed), oldest first, skipping
// pull requests, and with comments their comments.
func fetchGitHub(ctx context.Context, repo, token string, closed, comments bool) ([]sourceIssue, error) {
	state := "open"
	if closed {
		state = "all"
	}
	var issues []sourceIssue
	for page := 1; len(issues) < maxGitHubIssues; page++ {
		var batch []ghIssue
		if err := githubGet(ctx, fmt.Sprintf("%s/repos/%s/issues?state=%s&per_page=100&page=%d", githubAPI, repo, state, page),
			token, &batch); err != nil {
			return nil, err
		}
		for _, gi := range batch {
			if len(gi.PullRequest) > 0 && string(gi.PullRequest) != "null" || len(issues) == maxGitHubIssues {
				continue
			}
			s := sourceIssue{row: gi.Number, title: gi.Title, body: gi.Body, commentCount: gi.Comments,
				footer: fmt.Sprintf("Imported from GitHub %s#%d.", repo, gi.Number)}
			switch {
			case gi.State == "open":
				s.status, s.category = "Open", "default"
			case gi.StateReason == "not_planned":
				s.status, s.category = "Not planned", "canceled"
			default:
				s.status, s.category = "Closed", "completed"
			}
			for _, l := range gi.Labels {
				s.labels = append(s.labels, l.Name)
			}
			issues = append(issues, s)
		}
		if len(batch) < 100 {
			break
		}
	}
	slices.Reverse(issues) // GitHub lists newest first; the oldest gets the lowest number
	if !comments {
		return issues, nil
	}
	// ponytail: each issue's first 100 comments; page them if long threads get cut.
	var wg sync.WaitGroup
	sem, errs := make(chan struct{}, 8), make([]error, len(issues))
	for i := range issues {
		if issues[i].commentCount == 0 {
			continue
		}
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			var cs []ghComment
			errs[i] = githubGet(ctx, fmt.Sprintf("%s/repos/%s/issues/%d/comments?per_page=100", githubAPI, repo, issues[i].row), token, &cs)
			for _, c := range cs {
				issues[i].comments = append(issues[i].comments, sourceComment{author: c.User.Login, date: c.CreatedAt.Format(time.DateOnly), body: c.Body})
			}
			issues[i].commentCount = 0
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return issues, nil
}

// githubGet decodes the JSON at url into v, with the token when there's one. GitHub's failures are importErrors.
func githubGet(ctx context.Context, url, token string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("github request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Taskwell")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return errGitHubDown
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusUnauthorized && token != "":
		return errBadToken
	case res.StatusCode == http.StatusTooManyRequests,
		res.StatusCode == http.StatusForbidden && res.Header.Get("X-RateLimit-Remaining") == "0":
		return errRateLimit
	case res.StatusCode == http.StatusNotFound, res.StatusCode == http.StatusUnauthorized, res.StatusCode == http.StatusForbidden,
		res.StatusCode == http.StatusGone, res.StatusCode == http.StatusUnavailableForLegalReasons:
		return errNoRepo
	case res.StatusCode != http.StatusOK:
		return errGitHubDown
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(v); err != nil {
		return errGitHubDown
	}
	return nil
}

// --- mapping ---

// planImport maps the source issues onto the workspace: the summary, and the issues that aren't skipped.
func planImport(source string, issues []sourceIssue, refs importRefs) (summary, []plannedIssue) {
	s := summary{Source: source, Total: len(issues), Statuses: []statusMap{}, NewLabels: []string{},
		UnmatchedAssignees: []string{}, Skipped: []skipped{}}
	var out []plannedIssue
	for _, in := range issues {
		title := strings.TrimSpace(in.title)
		if msg := checkTitle(title); msg != "" {
			s.Skipped = append(s.Skipped, skipped{in.row, msg})
			continue
		}
		p := plannedIssue{title: title, priority: mapPriority(in.priority), due: parseDue(in.due)}
		var msg string
		if d := doc.FromText(strings.TrimSpace(in.body + "\n\n" + in.footer)); d != nil {
			p.description, msg = checkDescription(d)
		}
		for _, c := range in.comments {
			if msg != "" {
				break
			}
			if d := commentDoc(c); d != nil {
				var body json.RawMessage
				body, msg = checkComment(string(d))
				p.comments = append(p.comments, string(body))
			}
		}
		if msg != "" {
			s.Skipped = append(s.Skipped, skipped{in.row, msg})
			continue
		}

		st := mapStatus(in.status, in.category, refs.statuses)
		p.statusID = st.ID
		if i := slices.IndexFunc(s.Statuses, func(m statusMap) bool { return m.From == in.status }); i >= 0 {
			s.Statuses[i].Count++
		} else {
			s.Statuses = append(s.Statuses, statusMap{From: in.status, To: Person{st.ID, st.Name}, Count: 1})
		}

		if a := strings.TrimSpace(in.assignee); a != "" {
			p.assigneeID = matchMember(a, refs.members)
			if p.assigneeID == nil && !slices.Contains(s.UnmatchedAssignees, a) {
				s.UnmatchedAssignees = append(s.UnmatchedAssignees, a)
			}
		}

		for _, l := range in.labels {
			l = strings.TrimSpace(l)
			if r := []rune(l); len(r) > 30 {
				l = strings.TrimSpace(string(r[:30])) // a label name's limit
			}
			if l == "" || slices.ContainsFunc(p.labels, func(have string) bool { return strings.EqualFold(have, l) }) {
				continue
			}
			p.labels = append(p.labels, l)
			if _, ok := refs.labels[strings.ToLower(l)]; !ok &&
				!slices.ContainsFunc(s.NewLabels, func(n string) bool { return strings.EqualFold(n, l) }) {
				s.NewLabels = append(s.NewLabels, l)
			}
		}
		s.Comments += len(p.comments) + in.commentCount
		out = append(out, p)
	}
	return s, out
}

// mapStatus returns the status named like name (case-insensitive), else the first of the category its keywords
// suggest, else the default (the first). A set category ("default" for the default) skips the name.
func mapStatus(name, category string, statuses []Status) Status {
	if category == "" {
		if i := slices.IndexFunc(statuses, func(s Status) bool { return name != "" && strings.EqualFold(s.Name, strings.TrimSpace(name)) }); i >= 0 {
			return statuses[i]
		}
		category = statusCategory(name)
	}
	if i := slices.IndexFunc(statuses, func(s Status) bool { return s.Category == category }); i >= 0 {
		return statuses[i]
	}
	return statuses[0]
}

// statusCategory is the category a status name's keywords suggest; "default" when none do.
func statusCategory(name string) string {
	n := strings.ToLower(name)
	has := func(words ...string) bool {
		return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(n, w) })
	}
	switch {
	case has("done", "closed", "resolved", "complete"):
		return "completed"
	case has("cancel", "won't", "won’t", "wont", "duplicate", "invalid"):
		return "canceled"
	case has("progress", "review", "doing"):
		return "started"
	case has("backlog", "icebox", "triage"):
		return "backlog"
	}
	return "default"
}

// mapPriority turns a priority name or a digit 0–4 into a priority; 0 (none) when it's neither.
func mapPriority(s string) int {
	switch s = strings.ToLower(strings.TrimSpace(s)); s {
	case "urgent", "highest", "blocker", "critical":
		return 1
	case "high":
		return 2
	case "medium", "normal":
		return 3
	case "low", "lowest", "minor", "trivial":
		return 4
	}
	if n, ok := parsePriority(s); ok {
		return n
	}
	return 0
}

// parseDue returns YYYY-MM-DD for a date in that form, RFC 3339 or Jira's format; nil for anything else.
func parseDue(s string) *string {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.DateOnly, time.RFC3339, jiraTime} {
		if t, err := time.Parse(layout, s); err == nil {
			d := t.Format(time.DateOnly)
			return &d
		}
	}
	return nil
}

// matchMember returns the id of the member with the email, else the one with the name; nil when there's neither.
func matchMember(v string, members []importMember) *string {
	v = strings.ToLower(v)
	for _, match := range []func(importMember) bool{
		func(m importMember) bool { return m.email == v }, func(m importMember) bool { return m.name == v }} {
		if i := slices.IndexFunc(members, match); i >= 0 {
			return &members[i].id
		}
	}
	return nil
}

// commentDoc is an imported comment as a document: "**author** · date" on the first line, then the text; nil when
// it has no text.
func commentDoc(c sourceComment) json.RawMessage {
	body := doc.FromText(c.body)
	if body == nil || c.author == "" && c.date == "" {
		return body
	}
	var head []map[string]any
	if c.author != "" {
		head = append(head, map[string]any{"type": "text", "text": c.author, "marks": []map[string]string{{"type": "bold"}}})
	}
	if c.date != "" {
		sep := " · "
		if c.author == "" {
			sep = ""
		}
		head = append(head, map[string]any{"type": "text", "text": sep + c.date})
	}
	var d struct {
		Type    string            `json:"type"`
		Content []json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(body, &d) // FromText's own output
	p, _ := json.Marshal(map[string]any{"type": "paragraph", "content": head})
	d.Content = append([]json.RawMessage{p}, d.Content...)
	out, _ := json.Marshal(d)
	return out
}
