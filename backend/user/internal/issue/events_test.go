package issue

import (
	"encoding/json"
	"testing"
)

func TestDiff(t *testing.T) {
	todo, doing := statusSnap{ID: "s1", Name: "Todo", Category: "unstarted"}, statusSnap{ID: "s2", Name: "In progress", Category: "started"}
	maya, demo := &Person{"u1", "Maya"}, &Person{"u2", "Demo"}
	bug, docs := Label{"l1", "Bug", "#ff0000"}, Label{"l2", "Docs", "#00ff00"}
	date := func(s string) *string { return &s }
	base := snapshot{title: "A", description: json.RawMessage(`{"type":"doc"}`), status: todo, assignee: maya, priority: 2,
		dueDate: date("2026-11-03"), labels: []Label{bug}, parent: &eventRef{"i1", "ENG-1", "Parent"}}
	with := func(f func(*snapshot)) snapshot {
		s := base
		f(&s)
		return s
	}
	cases := []struct {
		name string
		to   snapshot
		want string
	}{
		{"same values", with(func(s *snapshot) { s.status.Name = "Renamed"; s.labels = []Label{bug} }), `[]`},
		{"title", with(func(s *snapshot) { s.title = "B" }), `[{"title":{"from":"A","to":"B"}}]`},
		{"description", with(func(s *snapshot) { s.description = nil }), `[{"description":{}}]`},
		{"status", with(func(s *snapshot) { s.status = doing }),
			`[{"status":{"from":{"id":"s1","name":"Todo","category":"unstarted"},"to":{"id":"s2","name":"In progress","category":"started"}}}]`},
		{"reassign", with(func(s *snapshot) { s.assignee = demo }),
			`[{"assignee":{"from":{"id":"u1","name":"Maya"},"to":{"id":"u2","name":"Demo"}}}]`},
		{"unassign", with(func(s *snapshot) { s.assignee = nil }), `[{"assignee":{"from":{"id":"u1","name":"Maya"},"to":null}}]`},
		{"priority", with(func(s *snapshot) { s.priority = 0 }), `[{"priority":{"from":2,"to":0}}]`},
		{"due date", with(func(s *snapshot) { s.dueDate = nil }), `[{"due_date":{"from":"2026-11-03","to":null}}]`},
		{"labels", with(func(s *snapshot) { s.labels = []Label{docs} }),
			`[{"labels":{"added":[{"id":"l2","name":"Docs","color":"#00ff00"}],"removed":[{"id":"l1","name":"Bug","color":"#ff0000"}]}}]`},
		{"label added", with(func(s *snapshot) { s.labels = []Label{bug, docs} }),
			`[{"labels":{"added":[{"id":"l2","name":"Docs","color":"#00ff00"}],"removed":[]}}]`},
		{"parent", with(func(s *snapshot) { s.parent = nil }), `[{"parent":{"from":{"id":"i1","identifier":"ENG-1","title":"Parent"},"to":null}}]`},
		{"two fields", with(func(s *snapshot) { s.title = "B"; s.priority = 4 }),
			`[{"title":{"from":"A","to":"B"}},{"priority":{"from":2,"to":4}}]`},
	}
	for _, c := range cases {
		got := []map[string]any{}
		for _, e := range diff(base, c.to) {
			got = append(got, map[string]any{e.kind: e.data})
		}
		if b, _ := json.Marshal(got); string(b) != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, b, c.want)
		}
	}
}
