package notify

import (
	"fmt"
	"slices"
	"strings"
)

// Change is one change to an issue that may notify people, recorded with Record in the change's transaction.
type Change struct {
	IssueID, ActorID string
	Watch            []string // users who start watching: the creator, a commenter
	Assignee         string   // the user just assigned; "" when the assignee didn't change to someone
	Status           any      // the status event's {from, to}; nil when the status didn't change
	CommentID        string   // the comment created or edited; "" for a description change
	Excerpt          string   // the comment's opening text
	NewComment       bool     // CommentID was just created
	Mentioned        []string // user ids newly mentioned in the description or comment
}

type note struct {
	userID, kind string
	data         any
}

// notes returns who gets what for the change, given the issue's watchers before it. Never the actor; people
// mentioned in a new comment get the mention instead of the comment.
func notes(c Change, watchers []string) []note {
	var ns []note
	add := func(userID, kind string, data any) {
		if userID != c.ActorID {
			ns = append(ns, note{userID, kind, data})
		}
	}
	about := map[string]string{}
	if c.CommentID != "" {
		about = map[string]string{"comment_id": c.CommentID, "excerpt": c.Excerpt}
	}
	if c.Assignee != "" {
		add(c.Assignee, "assigned", map[string]string{})
	}
	for _, w := range watchers {
		if c.Status != nil {
			add(w, "status", c.Status)
		}
		if c.NewComment && !slices.Contains(c.Mentioned, w) {
			add(w, "comment", about)
		}
	}
	for _, m := range c.Mentioned {
		add(m, "mention", about)
	}
	return ns
}

// Excerpt returns text's first 300 characters, trimmed.
func Excerpt(text string) string {
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > 300 {
		text = strings.TrimSpace(string(r[:300])) + "…"
	}
	return text
}

// message is one notification as an email needs it.
type message struct {
	kind, actor, identifier, title string
	workspace, slug                string
	from, to                       string // status names
	excerpt                        string
	onComment                      bool
}

// instantEmail returns the subject and body of the email for one notification; base is app_base_url.
func instantEmail(m message, base string) (subject, body string) {
	what := m.identifier + ": " + m.title
	switch m.kind {
	case "assigned":
		subject = m.actor + " assigned you " + m.identifier
		body = m.actor + " assigned you " + what
	case "status":
		subject = m.identifier + " moved to " + m.to
		body = fmt.Sprintf("%s moved %s from %s to %s", m.actor, what, m.from, m.to)
	case "comment":
		subject = m.actor + " commented on " + m.identifier
		body = m.actor + " commented on " + what + "\n\n" + quote(m.excerpt)
	default:
		subject = m.actor + " mentioned you in " + m.identifier
		where := ""
		if m.onComment {
			where = "a comment on "
		}
		body = m.actor + " mentioned you in " + where + what
	}
	return subject, body + "\n\n" + issueURL(base, m) + footer(base, m.slug)
}

// digestLines caps a digest's notification lines.
const digestLines = 50

// digestEmail returns the subject and body of one user's digest; ms is ordered by workspace, then issue.
func digestEmail(ms []message, base string) (subject, body string) {
	subject = fmt.Sprintf("Your Tether digest: %d updates", len(ms))
	if len(ms) == 1 {
		subject = "Your Tether digest: 1 update"
	}
	var b strings.Builder
	last := message{}
	for i, m := range ms {
		if i == digestLines {
			fmt.Fprintf(&b, "\nand %d more\n", len(ms)-digestLines)
			break
		}
		if m.slug != last.slug {
			fmt.Fprintf(&b, "\n%s\n", m.workspace)
		}
		if m.slug != last.slug || m.identifier != last.identifier {
			fmt.Fprintf(&b, "\n%s: %s\n%s\n", m.identifier, m.title, issueURL(base, m))
		}
		last = m
		var line string
		switch m.kind {
		case "assigned":
			line = m.actor + " assigned you"
		case "status":
			line = fmt.Sprintf("%s moved it from %s to %s", m.actor, m.from, m.to)
		case "comment":
			line = m.actor + " commented"
		default:
			line = m.actor + " mentioned you"
		}
		b.WriteString("- " + line + "\n")
	}
	return subject, strings.Trim(b.String(), "\n") + footer(base, ms[0].slug)
}

func issueURL(base string, m message) string {
	return strings.TrimRight(base, "/") + "/" + m.slug + "/issue/" + m.identifier
}

func footer(base, slug string) string {
	return "\n\nChange email settings: " + strings.TrimRight(base, "/") + "/" + slug + "/settings/notifications\n"
}

// quote prefixes each line of s with "> ".
func quote(s string) string {
	return "> " + strings.ReplaceAll(s, "\n", "\n> ")
}
