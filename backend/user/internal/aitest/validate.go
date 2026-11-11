package aitest

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/devender07/taskwell/backend/user/internal/doc"
	"github.com/devender07/taskwell/backend/user/internal/netguard"
)

// Lengths count UTF-16 units to match JS String.length.

const (
	maxTitleLen    = 200
	maxSteps       = 30
	maxStepLen     = 1000
	maxExpectedLen = 2000
	maxNoteLen     = 2000

	// Local runs.
	maxClientLen  = 40
	maxActionLen  = 300
	maxReproLines = 30
	maxReproLen   = 300
	maxLocalSteps = 60 // a case
	maxVideo      = 10 << 20
)

// cleanCase trims the fields and drops blank steps.
func cleanCase(f caseFields) caseFields {
	steps := []string{}
	for _, s := range f.Steps {
		if s = strings.TrimSpace(s); s != "" {
			steps = append(steps, s)
		}
	}
	return caseFields{Title: strings.TrimSpace(f.Title), Steps: steps, Expected: strings.TrimSpace(f.Expected)}
}

// checkCase returns the field errors for a cleaned case (empty messages for valid fields).
func checkCase(f caseFields) map[string]string {
	errs := map[string]string{}
	switch {
	case f.Title == "":
		errs["title"] = "Enter a title."
	case doc.Len(f.Title) > maxTitleLen:
		errs["title"] = "Title must be 200 characters or fewer."
	}
	switch {
	case len(f.Steps) == 0:
		errs["steps"] = "Add at least one step."
	case len(f.Steps) > maxSteps:
		errs["steps"] = "Use 30 steps or fewer."
	case slices.ContainsFunc(f.Steps, func(s string) bool { return doc.Len(s) > maxStepLen }):
		errs["steps"] = "Each step must be 1000 characters or fewer."
	}
	switch {
	case f.Expected == "":
		errs["expected"] = "Describe the expected result."
	case doc.Len(f.Expected) > maxExpectedLen:
		errs["expected"] = "Expected result must be 2000 characters or fewer."
	}
	return errs
}

func isFinished(status string) bool { return slices.Contains(finished, status) }

// checkBaseURL returns the site's normalized base URL (scheme and host, plus a path if given) and its lowercased
// host, or a message for the base_url field. http is accepted only when allowHTTP (outside production), and a
// private or local host only when allowPrivate (AI_TEST_ALLOW_PRIVATE, dev only).
func checkBaseURL(raw string, allowHTTP, allowPrivate bool) (base, host, msg string) {
	const msgScheme = "Enter a URL starting with https://."
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.Opaque != "" || (u.Scheme != "https" && !(allowHTTP && u.Scheme == "http")) {
		return "", "", msgScheme
	}
	host = strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || len(raw) > 2000 {
		return "", "", msgScheme
	}
	if !allowPrivate && netguard.PrivateHost(host) {
		return "", "", "Enter a public address, not a private or local one."
	}
	u.Host, u.RawQuery, u.Fragment, u.Scheme = strings.ToLower(u.Host), "", "", strings.ToLower(u.Scheme)
	return strings.TrimSuffix(u.String(), "/"), host, ""
}

func checkAccount(label, username, password string, passwordRequired bool) map[string]string {
	errs := map[string]string{}
	switch {
	case label == "":
		errs["label"] = "Enter a label."
	case doc.Len(label) > 100:
		errs["label"] = "Label must be 100 characters or fewer."
	}
	switch {
	case username == "":
		errs["username"] = "Enter a username."
	case doc.Len(username) > 200:
		errs["username"] = "Username must be 200 characters or fewer."
	}
	switch {
	case password == "" && passwordRequired:
		errs["password"] = "Enter a password."
	case len(password) > 500:
		errs["password"] = "Password must be 500 characters or fewer."
	}
	return errs
}

const (
	maxScriptSteps = 60
	maxCheckLen    = 200
)

// checkScript keeps only each step's own fields and returns the cleaned script, or a message for the script field.
// Navigations must stay on the workspace's site hosts and fill_password must name one of its account labels.
func checkScript(sc script, hosts, labels []string) (script, string) {
	sc.Check = strings.TrimSpace(sc.Check)
	switch {
	case len(sc.Steps) == 0:
		return sc, "Add at least one step."
	case len(sc.Steps) > maxScriptSteps:
		return sc, "Use 60 steps or fewer."
	case sc.Steps[0].Tool != "navigate":
		return sc, "Step 1: start with a navigate step."
	case sc.Check == "":
		return sc, "Add a check: text that's on the page only when the expected result is met."
	case doc.Len(sc.Check) > maxCheckLen:
		return sc, "Check must be 200 characters or fewer."
	}
	out := script{Steps: make([]scriptStep, len(sc.Steps)), Check: sc.Check}
	for i, s := range sc.Steps {
		c, msg := checkStep(s, hosts, labels)
		if msg != "" {
			return sc, fmt.Sprintf("Step %d: %s", i+1, msg)
		}
		out.Steps[i] = c
	}
	return out, ""
}

func checkStep(s scriptStep, hosts, labels []string) (scriptStep, string) {
	c := scriptStep{Tool: s.Tool}
	if s.Tool == "click" || s.Tool == "type" || s.Tool == "select" || s.Tool == "fill_password" {
		t := s.Target
		switch {
		case t == nil || t.Tag == "" || (t.CSS == "" && t.Name == ""):
			return c, "give the target's tag and its css or name."
		case len(t.CSS) > 500:
			return c, "css must be 500 characters or fewer."
		case doc.Len(t.Name) > 200:
			return c, "name must be 200 characters or fewer."
		}
		c.Target = &locator{CSS: t.CSS, Tag: strings.ToLower(t.Tag), Name: t.Name}
	}
	switch s.Tool {
	case "navigate":
		u, err := url.Parse(s.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !allowedHost(u.Hostname(), hosts) {
			return c, s.URL + " isn't a registered test site. Add it in Settings → AI testing."
		}
		c.URL = s.URL
	case "click":
	case "type":
		if doc.Len(s.Text) > 1000 {
			return c, "text must be 1000 characters or fewer."
		}
		c.Text = s.Text
	case "select":
		if doc.Len(s.Value) > 200 {
			return c, "value must be 200 characters or fewer."
		}
		c.Value = s.Value
	case "press":
		if s.Key == "" || len(s.Key) > 30 {
			return c, "key must be 1–30 characters."
		}
		c.Key = s.Key
	case "wait":
		if s.Seconds < 1 || s.Seconds > 5 {
			return c, "wait 1 to 5 seconds."
		}
		c.Seconds = s.Seconds
	case "assert_text", "assert_no_text":
		c.Text = strings.TrimSpace(s.Text)
		if c.Text == "" || doc.Len(c.Text) > 200 {
			return c, "text must be 1–200 characters."
		}
	case "fill_password":
		if !slices.Contains(labels, s.Account) {
			return c, fmt.Sprintf("no test account is labelled %q. Add it in Settings → AI testing.", s.Account)
		}
		c.Account = s.Account
	default:
		return c, fmt.Sprintf("unknown step %q.", s.Tool)
	}
	return c, ""
}

// literalFill is a .fill( call given a string literal.
var literalFill = regexp.MustCompile("\\.fill\\(\\s*['\"`]")

// checkPlaywright returns the message for a case's Playwright code: at most 20000 characters, and passwords only
// through process.env (a line mentioning a password may not fill a literal).
func checkPlaywright(code string) string {
	if utf8.RuneCountInString(code) > 20000 {
		return "Use 20000 characters or fewer."
	}
	for l := range strings.Lines(code) {
		if strings.Contains(strings.ToLower(l), "password") && literalFill.MatchString(l) {
			return "Use process.env for passwords, never a literal."
		}
	}
	return ""
}
