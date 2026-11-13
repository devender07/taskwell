package workspace

import (
	"regexp"
	"slices"

	"github.com/devender07/taskwell/backend/shared/doc"
)

// Same rules and messages as frontend/shared/src/validate/validate.ts.
// Lengths count UTF-16 units to match JS String.length. Callers pass the name trimmed and the slug lowercased.

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// reserved are top-level app paths and likely future ones.
var reserved = []string{"api", "admin", "app", "signin", "signup", "signout", "forgot-password", "reset-password",
	"new-workspace", "settings", "account", "invite", "invites", "join", "help", "support", "docs", "status", "about",
	"pricing", "billing", "welcome", "blog", "www", "mail", "static", "assets"}

func checkName(s string) string {
	if s == "" {
		return "Workspace name is required."
	}
	if doc.Len(s) > 50 {
		return "Workspace name must be 50 characters or fewer."
	}
	return ""
}

func checkRoleName(s string) string {
	if s == "" {
		return "Role name is required."
	}
	if doc.Len(s) > 40 {
		return "Role name must be 40 characters or fewer."
	}
	return ""
}

func checkSlug(s string) string {
	switch {
	case s == "":
		return "Workspace URL is required."
	case doc.Len(s) < 3 || doc.Len(s) > 32:
		return "Workspace URL must be 3 to 32 characters."
	case !slugRe.MatchString(s):
		return "Use lowercase letters, numbers and single hyphens."
	case slices.Contains(reserved, s):
		return "That URL is reserved."
	}
	return ""
}

// checkEmail and normEmail are copies of auth's.
