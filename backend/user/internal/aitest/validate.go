package aitest

import (
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/devender07/tether/backend/user/internal/doc"
)

// Lengths count UTF-16 units to match JS String.length.

var (
	identifierRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{1,4})-([1-9][0-9]*)$`)
	uuidRe       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

const (
	maxTitleLen    = 200
	maxSteps       = 30
	maxStepLen     = 1000
	maxExpectedLen = 2000
	maxNoteLen     = 2000
)

// parseIdentifier splits "eng-142" into "ENG" and 142; ok=false when it isn't an identifier.
func parseIdentifier(s string) (key string, number int, ok bool) {
	m := identifierRe.FindStringSubmatch(s)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.ParseInt(m[2], 10, 32)
	if err != nil {
		return "", 0, false
	}
	return strings.ToUpper(m[1]), int(n), true
}

func isUUID(s string) bool { return uuidRe.MatchString(s) }

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
	if !allowPrivate && privateHost(host) {
		return "", "", "Enter a public address, not a private or local one."
	}
	u.Host, u.RawQuery, u.Fragment, u.Scheme = strings.ToLower(u.Host), "", "", strings.ToLower(u.Scheme)
	return strings.TrimSuffix(u.String(), "/"), host, ""
}

// privateHost reports whether host is localhost or an IP literal the proxy would refuse.
func privateHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	a, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && blockedAddr(a)
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
