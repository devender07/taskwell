package realtime

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestPayload(t *testing.T) {
	many := make([]string, 300)
	for i := range many {
		many[i] = Issue("acme", fmt.Sprintf("ENG-%030d", i))
	}
	tests := []struct {
		name  string
		event Event
		want  []string
	}{
		{"issue paths", Event{Kind: "issue.updated", Workspace: "acme", Audience: ToWorkspace("w1"),
			Paths: append(IssuePaths("acme", "ENG-12", "ENG"), Issue("acme", "ENG-12"))},
			[]string{"/workspaces/acme/activity", "/workspaces/acme/issues/ENG-12", "/workspaces/acme/issues/ENG-12/timeline",
				"/workspaces/acme/my-issues", "/workspaces/acme/project-stats", "/workspaces/acme/projects/ENG/issues",
				"/workspaces/acme/roadmap", "/workspaces/acme/watching"}},
		{"too long collapses to the workspace prefix", Event{Kind: "issue.updated", Workspace: "acme", Audience: ToWorkspace("w1"),
			Paths: append(many, Project("acme", "ENG"))}, []string{"/workspaces/acme/"}},
		{"user event", Event{Kind: "profile.changed", Audience: ToUser("u1"), Paths: []string{"/me"}}, []string{"/me"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := payload(tt.event)
			if err != nil {
				t.Fatal(err)
			}
			if len(p) > maxPayload {
				t.Fatalf("payload is %d bytes", len(p))
			}
			var env envelope
			var e Event
			if err := json.Unmarshal(p, &env); err != nil || json.Unmarshal(env.Event, &e) != nil {
				t.Fatalf("bad payload %s", p)
			}
			if got := slices.Sorted(slices.Values(e.Paths)); !slices.Equal(got, tt.want) {
				t.Errorf("paths = %v, want %v", got, tt.want)
			}
			if env.Aud != tt.event.Audience || strings.Contains(string(env.Event), `"Audience"`) {
				t.Errorf("audience: %s", p)
			}
		})
	}
}

func TestSessionRevokedCloses(t *testing.T) {
	p, err := payload(SessionRevoked("u1"))
	if err != nil {
		t.Fatal(err)
	}
	var e envelope
	if err := json.Unmarshal(p, &e); err != nil || e.Aud != "user:u1" || !e.Close {
		t.Fatalf("got %s, %v", p, err)
	}
}
