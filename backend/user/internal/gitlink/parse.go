package gitlink

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Ref is an issue identifier found in a pull request; Closes is whether merging it should complete the issue.
type Ref struct {
	Key    string
	Number int
	Closes bool
}

// maxRefs caps the issues one pull request links.
const maxRefs = 20

var (
	idRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9]{1,9})-(\d{1,7})\b`)
	// closeRe is a closing word right before an identifier; group 1 is where the identifier starts.
	closeRe = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?):?\s+([a-z][a-z0-9]{1,9}-\d{1,7})\b`)
)

// Parse returns the identifiers in the branch, title and body, first seen first, at most maxRefs. Those in the branch
// or title close the issue, and so do those in the body after a closing word ("Fixes ENG-1").
func Parse(branch, title, body string) []Ref {
	var refs []Ref
	add := func(text string, closes func(start int) bool) {
		for _, m := range idRe.FindAllStringSubmatchIndex(text, -1) {
			n, _ := strconv.Atoi(text[m[4]:m[5]])
			r := Ref{strings.ToUpper(text[m[2]:m[3]]), n, closes(m[0])}
			if i := slices.IndexFunc(refs, func(o Ref) bool { return o.Key == r.Key && o.Number == r.Number }); i >= 0 {
				refs[i].Closes = refs[i].Closes || r.Closes
			} else if len(refs) < maxRefs {
				refs = append(refs, r)
			}
		}
	}
	always := func(int) bool { return true }
	add(branch, always)
	add(title, always)
	closing := map[int]bool{}
	for _, m := range closeRe.FindAllStringSubmatchIndex(body, -1) {
		closing[m[2]] = true
	}
	add(body, func(start int) bool { return closing[start] })
	return refs
}

// pr is a pull request (GitHub) or merge request (GitLab) as a delivery describes it.
type pr struct {
	Repo, URL, Title, Branch, Author, Body, State string
	Number                                        int
	Action                                        string // the provider's, for the log
}

var githubActions = []string{"opened", "edited", "reopened", "closed", "ready_for_review", "converted_to_draft", "synchronize"}

// parseGitHub reads a pull_request delivery; ok=false for other events and actions.
func parseGitHub(event string, body []byte) (p pr, ok bool, err error) {
	if event != "pull_request" {
		return p, false, nil
	}
	var d struct {
		Action      string
		PullRequest struct {
			Number  int
			HTMLURL string `json:"html_url"`
			Title   string
			Body    *string
			State   string
			Merged  bool
			Draft   bool
			Head    struct{ Ref string }
			User    struct{ Login string }
		} `json:"pull_request"`
		Repository struct {
			FullName string `json:"full_name"`
		}
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return p, false, err
	}
	if !slices.Contains(githubActions, d.Action) {
		return p, false, nil
	}
	g := d.PullRequest
	p = pr{Repo: d.Repository.FullName, URL: g.HTMLURL, Title: g.Title, Branch: g.Head.Ref, Author: g.User.Login,
		Number: g.Number, Action: d.Action, State: "open"}
	if g.Body != nil {
		p.Body = *g.Body
	}
	switch {
	case g.Merged:
		p.State = "merged"
	case g.State == "closed":
		p.State = "closed"
	case g.Draft:
		p.State = "draft"
	}
	return p, valid(p), nil
}

var gitlabActions = []string{"open", "reopen", "update", "close", "merge"}

// parseGitLab reads a Merge Request Hook delivery; ok=false for other events and actions.
func parseGitLab(event string, body []byte) (p pr, ok bool, err error) {
	if event != "Merge Request Hook" {
		return p, false, nil
	}
	var d struct {
		ObjectKind string `json:"object_kind"`
		Attrs      struct {
			IID            int
			URL            string
			Title          string
			Description    *string
			SourceBranch   string `json:"source_branch"`
			State          string
			Action         string
			Draft          bool
			WorkInProgress bool `json:"work_in_progress"`
		} `json:"object_attributes"`
		Project struct {
			Path string `json:"path_with_namespace"`
		}
		User struct{ Username string }
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return p, false, err
	}
	a := d.Attrs
	if d.ObjectKind != "merge_request" || !slices.Contains(gitlabActions, a.Action) {
		return p, false, nil
	}
	p = pr{Repo: d.Project.Path, URL: a.URL, Title: a.Title, Branch: a.SourceBranch, Author: d.User.Username,
		Number: a.IID, Action: a.Action}
	if a.Description != nil {
		p.Body = *a.Description
	}
	switch a.State {
	case "opened":
		p.State = "open"
		if a.Draft || a.WorkInProgress {
			p.State = "draft"
		}
	case "merged", "closed":
		p.State = a.State
	default: // "locked" and anything newer
		return p, false, nil
	}
	return p, valid(p), nil
}

// valid reports whether the delivery names a pull request we can store.
func valid(p pr) bool { return p.Repo != "" && p.Number > 0 && p.URL != "" }
