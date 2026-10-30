package workspace

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf16"
)

// Same rules and messages as frontend/shared/src/validate/validate.ts.
// Lengths count UTF-16 units to match JS String.length. Callers pass the name trimmed and the slug lowercased.

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// reserved are top-level app paths and likely future ones.
var reserved = []string{"api", "admin", "app", "signin", "signup", "signout", "forgot-password", "reset-password",
	"new-workspace", "settings", "account", "invite", "invites", "join", "help", "support", "docs", "status", "about",
	"pricing", "blog", "www", "mail", "static", "assets"}

func jsLen(s string) int { return len(utf16.Encode([]rune(s))) }

func checkName(s string) string {
	if s == "" {
		return "Workspace name is required."
	}
	if jsLen(s) > 50 {
		return "Workspace name must be 50 characters or fewer."
	}
	return ""
}

func checkRoleName(s string) string {
	if s == "" {
		return "Role name is required."
	}
	if jsLen(s) > 40 {
		return "Role name must be 40 characters or fewer."
	}
	return ""
}

func checkSlug(s string) string {
	switch {
	case s == "":
		return "Workspace URL is required."
	case jsLen(s) < 3 || jsLen(s) > 32:
		return "Workspace URL must be 3 to 32 characters."
	case !slugRe.MatchString(s):
		return "Use lowercase letters, numbers and single hyphens."
	case slices.Contains(reserved, s):
		return "That URL is reserved."
	}
	return ""
}

// checkEmail and normEmail are copies of auth's.
var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func checkEmail(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return "Email is required."
	}
	if jsLen(s) > 254 || !emailRe.MatchString(s) {
		return "Enter a valid email address."
	}
	return ""
}

func normEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
