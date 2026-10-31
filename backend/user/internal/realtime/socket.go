package realtime

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	readLimit    = 1 << 20 // live-editing frames
	controlLimit = 4 << 10 // text frames: clients only send small control messages
	eventBuffer  = 64
	frameBuffer  = 256
	writeWait    = 10 * time.Second
	pingEvery    = 30 * time.Second
)

// Collab is one connection's live editing of one issue (the issue package's Session).
type Collab interface {
	ID() string          // the issue's uuid
	WorkspaceID() string // its workspace's id
	Start(ctx context.Context) error
	Frame(ctx context.Context, data []byte) error // a binary frame from the client, tag included
	Leave(ctx context.Context)
}

// JoinFunc checks that the user may open the workspace's issue and returns its session, not yet started; nil and
// the message to show when they can't.
type JoinFunc func(ctx context.Context, c *Conn, userID, slug, identifier string) (Collab, string, error)

// Conn is one tab's socket. The hub queues events on events and live-editing frames on frames (kept in order);
// a writer goroutine drains both.
type Conn struct {
	id, userID string
	events     chan []byte
	frames     chan frame
	cancel     context.CancelFunc
	ws         string // the subscribed workspace's id; guarded by hub.mu

	mu     sync.Mutex // held while handling a live-editing message
	joined map[string]Collab
}

type frame struct {
	typ  websocket.MessageType
	data []byte
}

// Send queues a binary frame without waiting; false when the buffer is full.
func (c *Conn) Send(data []byte) bool { return c.offer(frame{websocket.MessageBinary, data}) }

// Write queues a binary frame, waiting for room.
func (c *Conn) Write(ctx context.Context, data []byte) error {
	return c.push(ctx, frame{websocket.MessageBinary, data})
}

// Close ends the connection; the tab reconnects.
func (c *Conn) Close() { c.cancel() }

func (c *Conn) offer(f frame) bool {
	select {
	case c.frames <- f:
		return true
	default:
		return false
	}
}

func (c *Conn) push(ctx context.Context, f frame) error {
	select {
	case c.frames <- f:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// event queues a text message, ending a connection that can't keep up.
func (c *Conn) event(msg []byte) {
	select {
	case c.events <- msg:
	default:
		c.cancel()
	}
}

func text(v any) []byte {
	b, _ := json.Marshal(v) // maps and strings only
	return b
}

// serve runs one tab's socket until it disconnects.
func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	userID, err := h.authenticate(r)
	if err != nil {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeErr(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "Not signed in.")
		return
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has written the error response
	}
	defer ws.CloseNow()
	ws.SetReadLimit(readLimit)

	c := &Conn{id: rand.Text(), userID: userID, events: make(chan []byte, eventBuffer), frames: make(chan frame, frameBuffer),
		joined: map[string]Collab{}}
	// The tab's own live-editing saves carry its id as origin, like its REST calls.
	ctx, cancel := context.WithCancel(context.WithValue(r.Context(), originKey{}, c.id))
	defer cancel()
	c.cancel = cancel
	c.event(text(map[string]string{"type": "hello", "client": c.id}))
	h.hub.add(c)
	defer h.disconnect(r.Context(), c)
	go c.write(ctx, ws)
	if err := h.read(ctx, ws, c); err != nil && ctx.Err() == nil {
		slog.Error("realtime connection failed", "user_id", userID, "err", err)
	}
}

// read handles the tab's messages until it disconnects; an error ends the connection.
func (h *Handler) read(ctx context.Context, ws *websocket.Conn, c *Conn) error {
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			return nil
		}
		if typ == websocket.MessageBinary {
			if err := h.frame(ctx, c, data); err != nil {
				return err
			}
			continue
		}
		if len(data) > controlLimit {
			ws.Close(websocket.StatusMessageTooBig, "Message too big.")
			return nil
		}
		var m struct{ Type, Workspace, Issue string }
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "ping":
			c.event(text(map[string]string{"type": "pong"}))
		case "subscribe":
			err = h.subscribe(ctx, c, m.Workspace)
		case "collab.join":
			err = h.join(ctx, c, m.Workspace, m.Issue)
		case "collab.leave":
			err = h.leave(ctx, c, m.Issue)
		}
		if err != nil {
			return err
		}
	}
}

func (h *Handler) subscribe(ctx context.Context, c *Conn, slug string) error {
	id, err := h.member(ctx, c.userID, slug)
	if err != nil {
		return err
	}
	if id == "" {
		c.event(text(map[string]string{"type": "error", "error": "Workspace not found."}))
		return nil
	}
	h.hub.subscribe(c, id)
	return nil
}

// join opens live editing of the issue: collab.joined, the viewers, then the stored document and ready.
// Joining an issue again restarts it.
func (h *Handler) join(ctx context.Context, c *Conn, slug, identifier string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, msg, err := h.joinFn(ctx, c, c.userID, slug, identifier)
	if err != nil {
		return err
	}
	if s == nil {
		return c.push(ctx, frame{websocket.MessageText, text(map[string]string{"type": "collab.error", "issue": identifier, "error": msg})})
	}
	id := s.ID()
	if old := c.joined[id]; old != nil {
		old.Leave(ctx)
	}
	c.joined[id] = s
	if err := c.push(ctx, frame{websocket.MessageText, text(map[string]string{"type": "collab.joined", "issue": identifier, "id": id})}); err != nil {
		return err
	}
	h.hub.joinIssue(c, id, identifier)
	if err := h.model.view(ctx, c.id, id, c.userID, instance); err != nil {
		return err
	}
	vs, err := h.model.viewers(ctx, id)
	if err != nil {
		return err
	}
	if err := c.push(ctx, frame{websocket.MessageText, presenceMsg(identifier, id, vs)}); err != nil {
		return err
	}
	return s.Start(ctx)
}

// leave ends live editing of the issue with the uuid.
func (h *Handler) leave(ctx context.Context, c *Conn, issueID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return h.leaveAll(ctx, c, func(s Collab) bool { return s.ID() == issueID })
}

// leaveAll ends the connection's live editing of the issues that match. Called with c.mu held.
func (h *Handler) leaveAll(ctx context.Context, c *Conn, match func(Collab) bool) error {
	var ids []string
	for id, s := range c.joined {
		if match(s) {
			s.Leave(ctx)
			delete(c.joined, id)
			h.hub.leaveIssue(c, id)
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return h.model.unview(ctx, c.id, ids)
}

// frame hands a live-editing frame to its issue's session; one for an issue the connection hasn't joined is ignored.
func (h *Handler) frame(ctx context.Context, c *Conn, data []byte) error {
	if len(data) < 17 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.joined[uuidString(data[1:17])]
	if s == nil {
		return nil
	}
	return s.Frame(ctx, data)
}

// dropWorkspace ends the connection's live editing in the workspace, after its user lost access.
func (h *Handler) dropWorkspace(ctx context.Context, c *Conn, workspaceID string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := h.leaveAll(ctx, c, func(s Collab) bool { return s.WorkspaceID() == workspaceID }); err != nil {
		slog.Error("realtime leave workspace failed", "workspace_id", workspaceID, "err", err)
	}
}

// disconnect takes the closed connection out of the hub and every issue it had open.
func (h *Handler) disconnect(ctx context.Context, c *Conn) {
	h.hub.remove(c)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := h.leaveAll(ctx, c, func(Collab) bool { return true }); err != nil {
		slog.Error("realtime disconnect failed", "err", err)
	}
}

// write sends queued messages and pings until ctx ends or a write fails, then ends the connection.
func (c *Conn) write(ctx context.Context, ws *websocket.Conn) {
	defer c.cancel()
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		var send func(context.Context) error
		select {
		case <-ctx.Done():
			return
		case msg := <-c.events:
			send = func(ctx context.Context) error { return ws.Write(ctx, websocket.MessageText, msg) }
		case f := <-c.frames:
			send = func(ctx context.Context) error { return ws.Write(ctx, f.typ, f.data) }
		case <-ping.C:
			send = ws.Ping
		}
		// The deadline starts now, not while waiting for something to send.
		wctx, cancel := context.WithTimeout(ctx, writeWait)
		err := send(wctx)
		cancel()
		if err != nil {
			return
		}
	}
}

// uuidString formats 16 raw bytes as a uuid.
func uuidString(b []byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
