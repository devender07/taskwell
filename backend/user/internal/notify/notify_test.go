package notify

import (
	"fmt"
	"strings"
	"testing"
)

func TestNotes(t *testing.T) {
	move := map[string]string{"from": "Todo", "to": "Done"}
	cases := []struct {
		name     string
		c        Change
		watchers []string
		want     string
	}{
		{"assigned someone else", Change{ActorID: "a", Assignee: "b"}, []string{"c"}, "b:assigned"},
		{"assigned yourself", Change{ActorID: "a", Assignee: "a"}, nil, ""},
		{"status to watchers but the actor", Change{ActorID: "a", Status: move}, []string{"a", "b", "c"}, "b:status c:status"},
		{"new comment to watchers", Change{ActorID: "a", CommentID: "x", NewComment: true}, []string{"a", "b"}, "b:comment"},
		{"mentioned in a new comment get the mention only", Change{ActorID: "a", CommentID: "x", NewComment: true,
			Mentioned: []string{"b", "d"}}, []string{"b", "c"}, "c:comment b:mention d:mention"},
		{"edited comment mentions only", Change{ActorID: "a", CommentID: "x", Mentioned: []string{"b"}}, []string{"c"}, "b:mention"},
		{"description mention, not yourself", Change{ActorID: "a", Mentioned: []string{"a", "b"}}, []string{"c"}, "b:mention"},
		{"assign and move together", Change{ActorID: "a", Assignee: "b", Status: move}, []string{"b"}, "b:assigned b:status"},
		{"nothing changed", Change{ActorID: "a"}, []string{"b"}, ""},
	}
	for _, c := range cases {
		var got []string
		for _, n := range notes(c.c, c.watchers) {
			got = append(got, n.userID+":"+n.kind)
		}
		if s := strings.Join(got, " "); s != c.want {
			t.Errorf("%s: got %q, want %q", c.name, s, c.want)
		}
	}
	if n := notes(Change{ActorID: "a", CommentID: "x", Excerpt: "hi", Mentioned: []string{"b"}}, nil); fmt.Sprint(n[0].data) != "map[comment_id:x excerpt:hi]" {
		t.Errorf("comment mention data: %v", n[0].data)
	}
}

func TestEmails(t *testing.T) {
	m := message{kind: "status", actor: "Demo", identifier: "ENG-12", title: "Fix login", workspace: "Acme", slug: "acme",
		from: "Todo", to: "In progress"}
	subject, body := instantEmail(m, "http://app/")
	if subject != "ENG-12 moved to In progress" ||
		body != "Demo moved ENG-12: Fix login from Todo to In progress\n\nhttp://app/acme/issue/ENG-12\n\nChange email settings: http://app/acme/settings/notifications\n" {
		t.Errorf("status email: %q %q", subject, body)
	}
	m.kind, m.excerpt = "comment", "line one\nline two"
	if _, body := instantEmail(m, "http://app"); !strings.Contains(body, "\n\n> line one\n> line two\n\n") {
		t.Errorf("comment email: %q", body)
	}
	m.kind, m.onComment = "mention", true
	if subject, body := instantEmail(m, "http://app"); subject != "Demo mentioned you in ENG-12" ||
		!strings.HasPrefix(body, "Demo mentioned you in a comment on ENG-12: Fix login") {
		t.Errorf("mention email: %q %q", subject, body)
	}

	ms := make([]message, 52)
	for i := range ms {
		ms[i] = message{kind: "assigned", actor: "Demo", identifier: fmt.Sprintf("ENG-%d", i/10), workspace: "Acme", slug: "acme"}
	}
	subject, body = digestEmail(ms, "http://app")
	if subject != "Your Tether digest: 52 updates" || strings.Count(body, "- Demo assigned you") != 50 ||
		!strings.Contains(body, "and 2 more") || strings.Count(body, "Acme\n") != 1 || strings.Count(body, "/issue/ENG-") != 5 {
		t.Errorf("digest: %q\n%s", subject, body)
	}
	if subject, _ := digestEmail(ms[:1], "http://app"); subject != "Your Tether digest: 1 update" {
		t.Errorf("digest of one: %q", subject)
	}
}

func TestExcerpt(t *testing.T) {
	if got := Excerpt("  " + strings.Repeat("é", 301)); got != strings.Repeat("é", 300)+"…" {
		t.Errorf("long: %q", got)
	}
	if got := Excerpt(" short "); got != "short" {
		t.Errorf("short: %q", got)
	}
}

func TestChatNotes(t *testing.T) {
	cases := []struct {
		name string
		c    ChatChange
		want string
	}{
		{"mentions, not the actor, once", ChatChange{ActorID: "a", Mentioned: []string{"a", "b", "b", "c"}}, "b:chat_mention c:chat_mention"},
		{"direct message", ChatChange{ActorID: "a", Direct: "b"}, "b:message"},
		{"mentioned in a direct message gets the mention only", ChatChange{ActorID: "a", Direct: "b", Mentioned: []string{"b"}}, "b:chat_mention"},
		{"nothing", ChatChange{ActorID: "a"}, ""},
	}
	for _, c := range cases {
		var got []string
		for _, n := range chatNotes(c.c) {
			got = append(got, n.userID+":"+n.kind)
		}
		if s := strings.Join(got, " "); s != c.want {
			t.Errorf("%s: got %q, want %q", c.name, s, c.want)
		}
	}
}

func TestChatEmails(t *testing.T) {
	m := message{kind: "chat_mention", actor: "Maya", workspace: "Acme", slug: "acme", excerpt: "hi", chatID: "c1",
		chatKind: "group", chatName: "Design team"}
	subject, body := instantEmail(m, "http://app/")
	if subject != "Maya mentioned you in Design team" ||
		body != "Maya mentioned you in Design team\n\n> hi\n\nhttp://app/acme/inbox/c/c1\n\nChange email settings: http://app/acme/settings/notifications\n" {
		t.Errorf("chat mention email: %q %q", subject, body)
	}
	m.kind = "message"
	if subject, body := instantEmail(m, "http://app"); subject != "Maya sent you a message" || !strings.Contains(body, "/acme/inbox/c/c1") {
		t.Errorf("message email: %q %q", subject, body)
	}
	_, body = digestEmail([]message{m, {kind: "assigned", actor: "Demo", identifier: "ENG-1", title: "T", workspace: "Acme", slug: "acme"}}, "http://app")
	if !strings.Contains(body, "Acme\n- Maya: hi\n\nENG-1: T\n") {
		t.Errorf("digest: %q", body)
	}
}
