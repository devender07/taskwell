package realtime

import (
	"maps"
	"sync"
)

// hub indexes this instance's connections by user, by subscribed workspace and by joined issue.
type hub struct {
	mu     sync.Mutex
	users  map[string]map[*Conn]struct{}
	spaces map[string]map[*Conn]struct{}
	issues map[string]map[*Conn]string // the identifier each connection joined the issue by
}

func newHub() *hub {
	return &hub{users: map[string]map[*Conn]struct{}{}, spaces: map[string]map[*Conn]struct{}{}, issues: map[string]map[*Conn]string{}}
}

func addTo[V any](m map[string]map[*Conn]V, key string, c *Conn, v V) {
	if m[key] == nil {
		m[key] = map[*Conn]V{}
	}
	m[key][c] = v
}

func removeFrom[V any](m map[string]map[*Conn]V, key string, c *Conn) {
	delete(m[key], c)
	if len(m[key]) == 0 {
		delete(m, key)
	}
}

func (hb *hub) add(c *Conn) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	addTo(hb.users, c.userID, c, struct{}{})
}

// remove takes c out of the user and workspace indexes; its issues leave one by one.
func (hb *hub) remove(c *Conn) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	removeFrom(hb.users, c.userID, c)
	removeFrom(hb.spaces, c.ws, c)
}

// subscribe replaces c's workspace subscription.
func (hb *hub) subscribe(c *Conn, workspaceID string) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	removeFrom(hb.spaces, c.ws, c)
	c.ws = workspaceID
	addTo(hb.spaces, workspaceID, c, struct{}{})
}

// unsubscribe ends the subscriptions to the workspace, of everyone when userID is "", else of that user's
// connections. It returns the connections concerned: the subscribers, or all of the user's.
func (hb *hub) unsubscribe(workspaceID, userID string) []*Conn {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	from := hb.spaces[workspaceID]
	if userID != "" {
		from = hb.users[userID]
	}
	var cs []*Conn
	for c := range from {
		cs = append(cs, c)
		if c.ws == workspaceID {
			removeFrom(hb.spaces, workspaceID, c)
			c.ws = ""
		}
	}
	return cs
}

func (hb *hub) toWorkspace(id string, msg []byte) { hb.send(hb.spaces, id, msg) }

func (hb *hub) toUser(id string, msg []byte) { hb.send(hb.users, id, msg) }

func (hb *hub) toAll(msg []byte) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	for _, cs := range hb.users {
		for c := range cs {
			c.event(msg)
		}
	}
}

func (hb *hub) send(index map[string]map[*Conn]struct{}, key string, msg []byte) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	for c := range index[key] {
		c.event(msg)
	}
}

func (hb *hub) joinIssue(c *Conn, issueID, identifier string) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	addTo(hb.issues, issueID, c, identifier)
}

func (hb *hub) leaveIssue(c *Conn, issueID string) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	removeFrom(hb.issues, issueID, c)
}

// joined returns a copy of the issue's joined connections here.
func (hb *hub) joined(issueID string) map[*Conn]string {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	return maps.Clone(hb.issues[issueID])
}
