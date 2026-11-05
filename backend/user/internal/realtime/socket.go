package realtime

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/devender07/taskwell/backend/user/internal/web"
)

const (
	readLimit    = 1 << 20 // live-editing frames
	controlLimit = 4 << 10 // text frames: clients only send small control messages
	eventBuffer  = 64
	frameBuffer  = 256
	writeWait    = 10 * time.Second
	pingEvery    = 30 * time.Second

	// statusReauth closes a socket whose access token expired or whose sign-in may have been revoked: the client
	// refreshes its token before reconnecting.
	statusReauth websocket.StatusCode = 4001
)

// Collab is one connection's live editing of one issue description or note body (a collab.Session).
type Collab interface {
	ID() string          // the document's uuid
	WorkspaceID() string // its workspace's id
	Start(ctx context.Context) error
	Frame(ctx context.Context, data []byte) error // a binary frame from the client, tag included
	Leave(ctx context.Context)
}

// JoinFunc checks that the user may open the workspace's issue or note and returns its session, not yet started; nil
// and the message to show when they can't.
type JoinFunc func(ctx context.Context, c *Conn, userID, slug, identifier string) (Collab, string, error)

// Conn is one tab's socket. The hub queues events on events and live-editing frames on frames (kept in order);
// a writer goroutine drains both.
type Conn struct {
	id, userID string
	session    Session
	ws         *websocket.Conn
	events     chan []byte
	frames     chan frame
	cancel     context.CancelFunc
	space      string // the subscribed workspace's id; guarded by hub.mu

	mu     sync.Mutex // held while handling a live-editing message
	joined map[string]Collab

	chats map[string]*chatAccess // typing: the chats checked lately; only the read goroutine uses it
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

// reauth ends the connection telling the tab to refresh its access token before reconnecting.
func (c *Conn) reauth() { go c.ws.Close(statusReauth, "Sign-in ended.") }

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
	h.conns.Add(1) // before the hijack, while the server's Shutdown still waits for this request
	defer h.conns.Done()
	s, err := h.authenticate(r)
	if err != nil {
		slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		web.Err(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	userID := s.UserID
	if userID == "" {
		web.Err(w, http.StatusUnauthorized, "Not signed in.")
		return
	}
	connID := rand.Text()
	if ok, err := h.model.claim(r.Context(), connID, userID, instance, MaxConnsPerUser); err != nil {
		slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		web.Err(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	} else if !ok {
		web.Err(w, http.StatusTooManyRequests, "Too many open tabs. Close some and try again.")
		return
	}
	defer h.release(r.Context(), connID)
	var opts *websocket.AcceptOptions
	if h.AppOrigin != nil {
		if u, err := url.Parse(h.AppOrigin(r)); err == nil && u.Host != "" {
			opts = &websocket.AcceptOptions{OriginPatterns: []string{u.Host}}
		}
	}
	ws, err := websocket.Accept(w, r, opts)
	if err != nil {
		return // Accept has written the error response
	}
	defer ws.CloseNow()
	ws.SetReadLimit(readLimit)

	c := &Conn{id: connID, userID: userID, session: s, ws: ws, events: make(chan []byte, eventBuffer), frames: make(chan frame, frameBuffer),
		joined: map[string]Collab{}, chats: map[string]*chatAccess{}}
	// The tab's own live-editing saves carry its id as origin, like its REST calls.
	ctx, cancel := context.WithCancel(context.WithValue(r.Context(), originKey{}, c.id))
	defer cancel()
	c.cancel = cancel
	expire := time.AfterFunc(time.Until(s.Expires), c.reauth)
	defer expire.Stop()
	c.event(text(map[string]string{"type": "hello", "client": c.id}))
	h.hub.add(c)
	defer h.disconnect(r.Context(), c)
	go c.write(ctx, ws)
	if err := h.read(ctx, ws, c); err != nil && ctx.Err() == nil {
		slog.ErrorContext(ctx, "realtime connection failed", "user_id", userID, "err", err)
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
		var m struct {
			Type, Workspace, Issue, Note, Chat string
			Thread                             *string
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "ping":
			c.event(text(map[string]string{"type": "pong"}))
		case "subscribe":
			err = h.subscribe(ctx, c, m.Workspace)
		case "collab.join":
			if m.Note != "" && h.JoinNote != nil {
				err = h.join(ctx, c, h.JoinNote, "note", m.Workspace, m.Note)
			} else {
				err = h.join(ctx, c, h.joinFn, "issue", m.Workspace, m.Issue)
			}
		case "collab.leave":
			err = h.leave(ctx, c, m.Issue)
		case "typing":
			err = h.typing(ctx, c, m.Chat, m.Thread)
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

// join opens live editing of the issue or note (kind, the key its replies carry): collab.joined, the viewers (issues
// only), then the stored document and ready. Joining a document again restarts it.
func (h *Handler) join(ctx context.Context, c *Conn, joinFn JoinFunc, kind, slug, identifier string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Suspension or a revoked sign-in also closes the socket through SessionRevoked; this covers a missed event.
	if ok, err := h.alive(ctx, c.session); err != nil {
		return err
	} else if !ok {
		c.reauth()
		return nil
	}
	s, msg, err := joinFn(ctx, c, c.userID, slug, identifier)
	if err != nil {
		return err
	}
	if s == nil {
		return c.push(ctx, frame{websocket.MessageText, text(map[string]string{"type": "collab.error", kind: identifier, "error": msg})})
	}
	id := s.ID()
	if old := c.joined[id]; old != nil {
		old.Leave(ctx)
	}
	c.joined[id] = s
	if err := c.push(ctx, frame{websocket.MessageText, text(map[string]string{"type": "collab.joined", kind: identifier, "id": id})}); err != nil {
		return err
	}
	if kind != "issue" {
		return s.Start(ctx)
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

// release frees the connection's place in the user's cap.
func (h *Handler) release(ctx context.Context, connID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := h.model.release(ctx, connID); err != nil {
		slog.Error("realtime release failed", "err", err)
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
