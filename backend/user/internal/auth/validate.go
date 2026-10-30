package auth

import (
	"regexp"
	"strings"
	"unicode/utf16"
)

// Same rules and messages as frontend/shared/src/validate/validate.ts.
// Lengths count UTF-16 units to match JS String.length.

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func jsLen(s string) int { return len(utf16.Encode([]rune(s))) }

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

func checkName(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return "Name is required."
	}
	if jsLen(s) > 100 {
		return "Name must be 100 characters or fewer."
	}
	return ""
}

// checkPassword: length only (NIST 800-63B); 72 bytes is bcrypt's input limit.
func checkPassword(v string) string {
	if v == "" {
		return "Password is required."
	}
	if jsLen(v) < 8 {
		return "Password must be at least 8 characters."
	}
	if len(v) > 72 {
		return "Password is too long."
	}
	return ""
}

var codeRe = regexp.MustCompile(`^\d{6}$`)

func checkCode(v string) string {
	if !codeRe.MatchString(strings.TrimSpace(v)) {
		return "Enter the 6-digit code."
	}
	return ""
}

// fieldErrors collects non-empty messages keyed by field; nil when all valid.
func fieldErrors(kv ...string) map[string]string {
	var errs map[string]string
	for i := 0; i < len(kv); i += 2 {
		if kv[i+1] != "" {
			if errs == nil {
				errs = map[string]string{}
			}
			errs[kv[i]] = kv[i+1]
		}
	}
	return errs
}
