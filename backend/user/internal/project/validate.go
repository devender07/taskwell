package project

import (
	"regexp"
	"strings"
	"time"

	"github.com/devender07/tether/backend/user/internal/doc"
)

// Same rules and messages as frontend/shared/src/validate/validate.ts.
// Lengths count UTF-16 units to match JS String.length. Callers pass the name trimmed and the key normalized.

var keyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,4}$`)

func checkName(s string) string {
	if s == "" {
		return "Project name is required."
	}
	if doc.Len(s) > 60 {
		return "Project name must be 60 characters or fewer."
	}
	return ""
}

func checkKey(s string) string {
	if s == "" {
		return "Project key is required."
	}
	if !keyRe.MatchString(s) {
		return "Use 2 to 5 letters or numbers, starting with a letter."
	}
	return ""
}

func checkDescription(s string) string {
	if doc.Len(s) > 10000 {
		return "Description must be 10,000 characters or fewer."
	}
	return ""
}

func checkMilestoneName(s string) string {
	if s == "" {
		return "Milestone name is required."
	}
	if doc.Len(s) > 60 {
		return "Milestone name must be 60 characters or fewer."
	}
	return ""
}

// checkDate takes a YYYY-MM-DD date.
func checkDate(s string) string {
	if t, err := time.Parse(time.DateOnly, s); err != nil || t.Year() < 1 {
		return errDate
	}
	return ""
}

const (
	errDate      = "Enter a valid date."
	errDateOrder = "Target date must be on or after the start date."
)

func normKey(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
