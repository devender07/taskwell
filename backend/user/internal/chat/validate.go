package chat

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/devender07/tether/backend/user/internal/doc"
)

const (
	maxNameLen    = 80
	maxMessageLen = 10000
	maxMembers    = 50 // a group's people, its creator included
	maxEmojiLen   = 16 // code points, as the reactions table checks
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// isUUID reports whether s is a uuid as Postgres prints it, so comparing it to a uuid column can't fail.
func isUUID(s string) bool { return uuidRe.MatchString(s) }

// checkName takes a group name trimmed.
func checkName(s string) string {
	if s == "" {
		return "Name the group."
	}
	if doc.Len(s) > maxNameLen {
		return "Keep the name under 80 characters."
	}
	return ""
}

// checkBody validates a message's doc JSON, which can't be empty, and returns it normalized.
func checkBody(raw string) (json.RawMessage, string) {
	if raw == "" {
		return nil, "Write a message."
	}
	d, msg := doc.Check([]byte(raw), "Message", maxMessageLen)
	if d == nil && msg == "" {
		msg = "Write a message."
	}
	return d, msg
}

// checkEmoji takes an emoji trimmed: 1–16 characters, no spaces or control characters.
func checkEmoji(s string) string {
	if s == "" || utf8.RuneCountInString(s) > maxEmojiLen || !utf8.ValidString(s) ||
		strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "Choose an emoji."
	}
	return ""
}

// people returns the distinct ids picked, without the user's own, and whether they all look like user ids.
func people(ids []string, self string) ([]string, bool) {
	ok := true
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !isUUID(id) {
			ok = false
			continue
		}
		if id != self && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, ok
}
