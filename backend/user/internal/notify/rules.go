package notify

import (
	"fmt"
	"slices"
	"strings"
	"time"
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

// chatNotes returns who gets what for a chat message: the mentioned a chat_mention, the other person in a direct chat
// a message unless mentioned. Never the actor.
func chatNotes(c ChatChange) []note {
	var ns []note
	for _, u := range c.Mentioned {
		if u != c.ActorID && !slices.ContainsFunc(ns, func(n note) bool { return n.userID == u }) {
			ns = append(ns, note{u, "chat_mention", nil})
		}
	}
	if c.Direct != "" && c.Direct != c.ActorID && !slices.Contains(c.Mentioned, c.Direct) {
		ns = append(ns, note{c.Direct, "message", nil})
	}
	return ns
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
	chatID, chatKind, chatName     string // chat kinds only
	projectKey, projectName        string // project_added only
	dueDate                        string // "2026-11-12"; due_soon and overdue only
}

// day formats the message's due date with layout ("Mon, Jan 2"), or returns it as it is when it isn't a date.
func (m message) day(layout string) string {
	d, err := time.Parse(time.DateOnly, m.dueDate)
	if err != nil {
		return m.dueDate
	}
	return d.Format(layout)
}

// instantEmail returns the subject and body of the email for one notification; base is app_base_url.
func instantEmail(m message, base string) (subject, body string) {
	switch m.kind {
	case "chat_mention":
		where := m.chatName
		if m.chatKind == "direct" {
			where = "a message"
		}
		return m.actor + " mentioned you in " + where, m.actor + " mentioned you in " + where + "\n\n" + quote(m.excerpt) +
			"\n\n" + chatURL(base, m) + footer(base, m.slug)
	case "message":
		return m.actor + " sent you a message", m.actor + " sent you a message\n\n" + quote(m.excerpt) + "\n\n" +
			chatURL(base, m) + footer(base, m.slug)
	case "project_added":
		return m.actor + " added you to " + m.projectName, m.actor + " added you to the project " + m.projectName + " in " +
			m.workspace + "\n\n" + projectURL(base, m) + footer(base, m.slug)
	}
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
	case "due_soon":
		subject = m.identifier + " is due tomorrow"
		body = what + " is due tomorrow (" + m.day("Mon, Jan 2") + ")."
	case "overdue":
		subject = m.identifier + " is overdue"
		body = what + " was due " + m.day("Mon, Jan 2") + " and is still open."
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
	subject = fmt.Sprintf("Your Taskwell digest: %d updates", len(ms))
	if len(ms) == 1 {
		subject = "Your Taskwell digest: 1 update"
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
		if m.chatID != "" {
			last = m
			b.WriteString("- " + m.actor + ": " + m.excerpt + "\n")
			continue
		}
		if m.projectKey != "" {
			last = m
			b.WriteString("- " + m.actor + " added you to " + m.projectName + ": " + projectURL(base, m) + "\n")
			continue
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
		case "due_soon":
			line = "due tomorrow"
		case "overdue":
			line = "overdue since " + m.day("Jan 2")
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

func projectURL(base string, m message) string {
	return strings.TrimRight(base, "/") + "/" + m.slug + "/projects/" + m.projectKey
}

func chatURL(base string, m message) string {
	return strings.TrimRight(base, "/") + "/" + m.slug + "/inbox/c/" + m.chatID
}

func footer(base, slug string) string {
	return "\n\nChange email settings: " + strings.TrimRight(base, "/") + "/" + slug + "/settings/notifications\n"
}

// quote prefixes each line of s with "> ".
func quote(s string) string {
	return "> " + strings.ReplaceAll(s, "\n", "\n> ")
}
