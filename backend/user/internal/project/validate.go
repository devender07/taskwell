package project

import (
	"regexp"
	"strings"
	"unicode/utf16"
)

// Same rules and messages as frontend/shared/src/validate/validate.ts.
// Lengths count UTF-16 units to match JS String.length. Callers pass the name trimmed and the key normalized.

var keyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,4}$`)

func jsLen(s string) int { return len(utf16.Encode([]rune(s))) }

func checkName(s string) string {
	if s == "" {
		return "Project name is required."
	}
	if jsLen(s) > 60 {
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
	if jsLen(s) > 10000 {
		return "Description must be 10,000 characters or fewer."
	}
	return ""
}

func normKey(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
