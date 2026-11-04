package issue

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/devender07/taskwell/backend/user/internal/doc"
)

// Same rules and messages as frontend/shared/src/validate/validate.ts.
// Lengths count UTF-16 units to match JS String.length. Callers pass names and titles trimmed.

var (
	colorRe      = regexp.MustCompile(`^#[0-9a-f]{6}([0-9a-f]{2})?$`) // #rrggbb, or #rrggbbaa with opacity
	dateRe       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	identifierRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{1,4})-([1-9][0-9]*)$`)
	categories   = []string{"backlog", "unstarted", "started", "completed", "canceled"}
	// typeIcons are the status type icon keys, as @taskwell/shared/icons draws them.
	typeIcons = []string{"circle", "circle-dashed", "circle-half", "circle-check", "circle-x", "eye", "refresh", "pause",
		"clock", "flag", "star", "bug", "rocket", "alert", "lock", "message"}
)

const (
	maxDescriptionLen = 50000
	maxCommentLen     = 20000
)

func checkDescription(raw []byte) (json.RawMessage, string) {
	return doc.Check(raw, "Description", maxDescriptionLen)
}

// checkComment is doc.Check for a comment, which can't be empty.
func checkComment(raw string) (json.RawMessage, string) {
	if raw == "" {
		return nil, "Write a comment."
	}
	d, msg := doc.Check([]byte(raw), "Comment", maxCommentLen)
	if d == nil && msg == "" {
		msg = "Write a comment."
	}
	return d, msg
}

func checkTitle(s string) string {
	if s == "" {
		return "Title is required."
	}
	if doc.Len(s) > 200 {
		return "Title must be 200 characters or fewer."
	}
	return ""
}

// parsePriority returns the priority 0–4, or ok=false.
func parsePriority(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 0 && n <= 4
}

func checkPriority(s string) string {
	if _, ok := parsePriority(s); !ok {
		return "Choose a priority."
	}
	return ""
}

// checkDueDate accepts YYYY-MM-DD for a real date, or empty.
func checkDueDate(s string) string {
	if s == "" {
		return ""
	}
	if _, err := time.Parse(time.DateOnly, s); err != nil || !dateRe.MatchString(s) {
		return "Enter a valid date."
	}
	return ""
}

func checkStatusName(s string) string {
	if s == "" {
		return "Status name is required."
	}
	if doc.Len(s) > 30 {
		return "Status name must be 30 characters or fewer."
	}
	return ""
}

func checkTypeName(s string) string {
	if s == "" {
		return "Type name is required."
	}
	if doc.Len(s) > 30 {
		return "Type name must be 30 characters or fewer."
	}
	return ""
}

func checkTypeIcon(s string) string {
	if !slices.Contains(typeIcons, s) {
		return "Choose an icon."
	}
	return ""
}

func checkCategory(s string) string {
	if !slices.Contains(categories, s) {
		return "Choose a category."
	}
	return ""
}

func checkLabelName(s string) string {
	if s == "" {
		return "Label name is required."
	}
	if doc.Len(s) > 30 {
		return "Label name must be 30 characters or fewer."
	}
	return ""
}

// checkColor takes the color normalized by normColor.
func checkColor(s string) string {
	if !colorRe.MatchString(s) {
		return "Choose a color."
	}
	return ""
}

func normColor(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// parseIdentifier splits "eng-142" into "ENG" and 142; ok=false when it isn't an identifier.
func ParseIdentifier(s string) (key string, number int, ok bool) {
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
