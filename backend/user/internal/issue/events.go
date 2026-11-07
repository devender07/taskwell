package issue

import (
	"bytes"
	"encoding/json"
	"slices"
)

// statusSnap and eventRef are the names an event keeps, so the log reads right after renames and deletes.
type statusSnap struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Icon     *string `json:"icon,omitempty"` // the type's; left out for built-ins, as in events from before types
	Color    *string `json:"color,omitempty"`
}

type eventRef struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

// snapshot is an issue's logged fields, read before and after a change.
type snapshot struct {
	title       string
	description json.RawMessage
	status      statusSnap
	assignee    *Person
	priority    int
	dueDate     *string
	estimate    *int
	labels      []Label
	parent      *eventRef
	projectID   string
	cycleID     string     // "" when none
	cycle       *cycleSnap // nil when none
}

type cycleSnap struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}

// event is one activity entry about to be written; data marshals to the kind's JSON.
type event struct {
	kind string
	data any
}

type change[T any] struct {
	From T `json:"from"`
	To   T `json:"to"`
}

// diff returns an event for each logged field that differs between a and b.
func diff(a, b snapshot) []event {
	var es []event
	if a.title != b.title {
		es = append(es, event{"title", change[string]{a.title, b.title}})
	}
	if !bytes.Equal(a.description, b.description) {
		es = append(es, event{"description", struct{}{}})
	}
	if a.status.ID != b.status.ID {
		es = append(es, event{"status", change[statusSnap]{a.status, b.status}})
	}
	if personID(a.assignee) != personID(b.assignee) {
		es = append(es, event{"assignee", change[*Person]{a.assignee, b.assignee}})
	}
	if a.priority != b.priority {
		es = append(es, event{"priority", change[int]{a.priority, b.priority}})
	}
	if !intEq(a.estimate, b.estimate) {
		es = append(es, event{"estimate", change[*int]{a.estimate, b.estimate}})
	}
	if deref(a.dueDate) != deref(b.dueDate) {
		es = append(es, event{"due_date", change[*string]{a.dueDate, b.dueDate}})
	}
	added, removed := labelsMissing(b.labels, a.labels), labelsMissing(a.labels, b.labels)
	if len(added) > 0 || len(removed) > 0 {
		es = append(es, event{"labels", map[string][]Label{"added": added, "removed": removed}})
	}
	if refID(a.parent) != refID(b.parent) {
		es = append(es, event{"parent", change[*eventRef]{a.parent, b.parent}})
	}
	if a.cycleID != b.cycleID {
		es = append(es, event{"cycle", change[*cycleSnap]{a.cycle, b.cycle}})
	}
	return es
}

// labelsMissing returns the labels in ls that aren't in from, never nil.
func labelsMissing(ls, from []Label) []Label {
	out := []Label{}
	for _, l := range ls {
		if !slices.ContainsFunc(from, func(f Label) bool { return f.ID == l.ID }) {
			out = append(out, l)
		}
	}
	return out
}

func personID(p *Person) string {
	if p == nil {
		return ""
	}
	return p.ID
}

func refID(r *eventRef) string {
	if r == nil {
		return ""
	}
	return r.ID
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func intEq(a, b *int) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
