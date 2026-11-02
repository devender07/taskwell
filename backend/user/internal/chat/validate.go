package chat

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/devender07/tether/backend/user/internal/doc"
	"github.com/devender07/tether/backend/user/internal/web"
)

const (
	maxNameLen    = 80
	maxMessageLen = 10000
	maxMembers    = 50 // a group's people, its creator included
	maxEmojiLen   = 16 // code points, as the reactions table checks
)

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
		if !web.IsUUID(id) {
			ok = false
			continue
		}
		if id != self && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, ok
}
