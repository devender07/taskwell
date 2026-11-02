package realtime

import (
	"encoding/json"
	"slices"
	"strconv"
	"time"
)

// Event tells tabs which REST paths (without /api) changed, so they refetch them. It never carries data.
type Event struct {
	Type      string    `json:"type"` // "event", set by Publish
	Kind      string    `json:"kind"`
	Workspace string    `json:"workspace"` // the workspace's slug; "" for user-wide and admin events
	Actor     string    `json:"actor"`
	Origin    string    `json:"origin"` // the X-Tether-Client of the request that made the change, set by Publish
	Paths     []string  `json:"paths"`
	NewSlug   string    `json:"new_slug,omitempty"`
	At        time.Time `json:"at"`

	// Audience is who gets it: ToWorkspace, ToUser or ToAdmin. Never sent.
	Audience string `json:"-"`
	// WorkspaceID, on a workspace.removed event, is the workspace whose subscriptions and live editing end.
	WorkspaceID string `json:"-"`
	// Close ends the audience's connections after the event: they re-authenticate, reconnect and re-join.
	Close bool `json:"-"`
	// Project is the id of the project a ToWorkspace event is about, if any. When it's private, Publish sends the
	// event to each person who can see it (ToUser) instead.
	Project string `json:"-"`
}

const ToAdmin = "admin"

// ToWorkspace is every connection subscribed to the workspace with the id.
func ToWorkspace(id string) string { return "ws:" + id }

// ToUser is every connection of the user with the id.
func ToUser(id string) string { return "user:" + id }

func toIssue(id string) string { return "issue:" + id }

// maxPayload keeps a NOTIFY payload under Postgres's 8000-byte limit.
const maxPayload = 7900

// envelope is the NOTIFY payload: the event plus its audience.
type envelope struct {
	Aud   string          `json:"aud"`
	WS    string          `json:"ws,omitempty"` // Event.WorkspaceID
	Close bool            `json:"close,omitempty"`
	Skip  string          `json:"skip,omitempty"` // a user whose connections don't get it: a typing frame's sender
	Event json.RawMessage `json:"event,omitempty"`
}

// payload returns e's NOTIFY payload, its paths de-duplicated. Paths too long for a notification collapse to the
// workspace prefix.
func payload(e Event) ([]byte, error) {
	e.Paths = slices.Compact(slices.Sorted(slices.Values(e.Paths)))
	for {
		ev, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		p, err := json.Marshal(envelope{Aud: e.Audience, WS: e.WorkspaceID, Close: e.Close, Event: ev})
		if err != nil || len(p) <= maxPayload || e.Workspace == "" || len(e.Paths) == 1 {
			return p, err
		}
		e.Paths = []string{All(e.Workspace)}
	}
}

// --- paths ---

// Workspace is the workspace's own path.
func Workspace(slug string) string { return "/workspaces/" + slug }

// All is the prefix of every path in the workspace: everything there may have changed.
func All(slug string) string { return Workspace(slug) + "/" }

func Issue(slug, identifier string) string { return Workspace(slug) + "/issues/" + identifier }

func Timeline(slug, identifier string) string { return Issue(slug, identifier) + "/timeline" }

func Project(slug, key string) string { return Workspace(slug) + "/projects/" + key }

func ProjectIssues(slug, key string) string { return Project(slug, key) + "/issues" }

// Cycles is the project's cycle list.
func Cycles(slug, key string) string { return Project(slug, key) + "/cycles" }

func Cycle(slug, key string, number int) string {
	return Cycles(slug, key) + "/" + strconv.Itoa(number)
}

func MyIssues(slug string) string { return Workspace(slug) + "/my-issues" }

// Activity is the workspace's activity feed on Home.
func Activity(slug string) string { return Workspace(slug) + "/activity" }

// ProjectStats is the per-project issue counts on Home.
func ProjectStats(slug string) string { return Workspace(slug) + "/project-stats" }

// IssuePaths are what an issue change touches: its page and timeline, its project's list and board, my issues, and
// Home's activity and project counts.
func IssuePaths(slug, identifier, key string) []string {
	return []string{Issue(slug, identifier), Timeline(slug, identifier), ProjectIssues(slug, key), MyIssues(slug),
		Activity(slug), ProjectStats(slug)}
}

// SessionRevoked closes the user's sockets after a sign-out, password change or reset (the admin service publishes it
// on suspension too). The tabs whose sign-in is still live reconnect; the others can't.
func SessionRevoked(userID string) Event {
	return Event{Kind: "session.revoked", Actor: userID, Audience: ToUser(userID), Paths: []string{"/me"}, Close: true}
}

// --- admin events (paths are the admin API's) ---

// AdminUsers tells the admin app a user signed up.
func AdminUsers(actor string) Event {
	return Event{Kind: "admin.users", Actor: actor, Audience: ToAdmin, Paths: []string{"/users", "/stats"}}
}

// AdminWorkspaces tells the admin app a workspace was created, renamed or deleted, or its members changed.
func AdminWorkspaces(actor string) Event {
	return Event{Kind: "admin.workspaces", Actor: actor, Audience: ToAdmin, Paths: []string{"/workspaces", "/stats"}}
}

// AdminStats tells the admin app the project, issue or comment counts changed.
func AdminStats(actor string) Event {
	return Event{Kind: "admin.stats", Actor: actor, Audience: ToAdmin, Paths: []string{"/stats", "/workspaces"}}
}
