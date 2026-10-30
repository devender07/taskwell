package issue

import (
	"context"
	"encoding/binary"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/devender07/tether/backend/user/internal/workspace"
)

// Live-editing frame types: the first byte of every binary frame.
const (
	msgReady byte = iota
	msgUpdate
	msgAwareness
	msgCompact
	msgSave
	msgSeed
)

const (
	maxFrame   = 1 << 20
	sendBuffer = 256
	writeWait  = 10 * time.Second
	pingEvery  = 30 * time.Second
)

// hub holds a room per issue being edited live.
// ponytail: single API instance only; with several instances, forward updates through Postgres LISTEN/NOTIFY.
type hub struct {
	mu    sync.Mutex
	rooms map[string]*room
}

type room struct {
	clients map[*client]struct{}
	seeder  *client // the editor told to seed the document from the saved description, until its seed is stored
	seeded  bool    // the seed update is stored
}

// client is one connection. The hub queues frames on send; a writer goroutine drains it.
type client struct {
	send    chan []byte
	cancel  context.CancelFunc
	canEdit bool
}

func (hb *hub) join(issueID string, c *client) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	r := hb.rooms[issueID]
	if r == nil {
		r = &room{clients: map[*client]struct{}{}}
		hb.rooms[issueID] = r
	}
	r.clients[c] = struct{}{}
}

func (hb *hub) leave(issueID string, c *client) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	if r := hb.rooms[issueID]; r != nil {
		delete(r.clients, c)
		if len(r.clients) == 0 {
			delete(hb.rooms, issueID)
		} else if r.seeder == c {
			r.passSeed()
		}
	}
}

// passSeed hands an unfinished seed to another editor in the room, or leaves it for the next one to join.
// Called with hb.mu held.
// ponytail: the new seeder skips seeding if its user already typed into the empty document; their text then
// replaces the saved description. Rare: needs the first editor to leave within a second of opening the issue.
func (r *room) passSeed() {
	r.seeder = nil
	for c := range r.clients {
		if !c.canEdit {
			continue
		}
		select {
		case c.send <- []byte{msgSeed}:
			r.seeder = c
			return
		default:
		}
	}
}

// claimSeed returns true for the first editor since the room opened, or since the seeder left without seeding.
func (hb *hub) claimSeed(issueID string, c *client) bool {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	r := hb.rooms[issueID]
	if r == nil || r.seeded || r.seeder != nil {
		return false
	}
	r.seeder = c
	return true
}

// stored records that c's update is in the database; the seeder's first one is the seed.
func (hb *hub) stored(issueID string, c *client) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	if r := hb.rooms[issueID]; r != nil && r.seeder == c {
		r.seeder, r.seeded = nil, true
	}
}

// broadcast queues msg for every connection on the issue but from, dropping any whose buffer is full.
func (hb *hub) broadcast(issueID string, from *client, msg []byte) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	r := hb.rooms[issueID]
	if r == nil {
		return
	}
	for c := range r.clients {
		if c == from {
			continue
		}
		select {
		case c.send <- msg:
		default:
			delete(r.clients, c)
			c.cancel()
			if r.seeder == c {
				r.passSeed()
			}
		}
	}
}

// collab serves the live-editing WebSocket for an issue's description.
func (h *Handler) collab(w http.ResponseWriter, r *http.Request) {
	var ws *workspace.Workspace
	var i *Issue
	ok := false
	userID, err := h.authenticate(r)
	if err == nil && userID == "" {
		writeErr(w, http.StatusUnauthorized, "Not signed in.")
		return
	}
	if err == nil {
		ws, i, ok, err = h.issue(w, r, userID)
	}
	if err != nil {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeErr(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	if !ok {
		return
	}
	allow(i, userID, ws.Permissions)

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has written the error response
	}
	defer conn.CloseNow()
	conn.SetReadLimit(maxFrame)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c := &client{send: make(chan []byte, sendBuffer), cancel: cancel, canEdit: i.CanEdit}
	// Join before loading: an update stored after the load is broadcast to us, one stored before is in the load
	// (and maybe broadcast too; Yjs ignores duplicates).
	h.hub.join(i.ID, c)
	defer h.hub.leave(i.ID, c)
	if err := h.sendStored(ctx, conn, i, c); err != nil {
		slog.Info("collab load failed", "issue", i.ID, "err", err)
		return
	}
	go c.write(ctx, conn)
	h.readFrames(ctx, conn, c, i.ID, userID, i.CanEdit)
}

// sendStored sends the issue's stored updates, then ready.
func (h *Handler) sendStored(ctx context.Context, conn *websocket.Conn, i *Issue, c *client) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	us, err := h.model.docUpdates(ctx, i.ID)
	if err != nil {
		return err
	}
	var last int64
	for _, u := range us {
		if err := conn.Write(ctx, websocket.MessageBinary, append([]byte{msgUpdate}, u.data...)); err != nil {
			return err
		}
		last = u.id
	}
	var flags byte
	if i.CanEdit {
		flags |= 1
	}
	// Only an editor seeds: a read-only client's seed update would be ignored, leaving the room unseeded for good.
	if i.CanEdit && len(us) == 0 && i.Description != nil && h.hub.claimSeed(i.ID, c) {
		flags |= 2
	}
	return conn.Write(ctx, websocket.MessageBinary, binary.BigEndian.AppendUint64([]byte{msgReady, flags}, uint64(last)))
}

// readFrames handles the client's frames until it disconnects or a store fails.
func (h *Handler) readFrames(ctx context.Context, conn *websocket.Conn, c *client, issueID, userID string, canEdit bool) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary || len(data) < 2 {
			continue
		}
		body := data[1:]
		switch {
		case data[0] == msgAwareness:
			h.hub.broadcast(issueID, c, data)
		case !canEdit:
		case data[0] == msgUpdate:
			if err := h.model.addDocUpdate(ctx, issueID, body); err != nil {
				slog.Error("collab update failed", "issue", issueID, "err", err)
				return
			}
			h.hub.stored(issueID, c)
			h.hub.broadcast(issueID, c, data)
		case data[0] == msgCompact && len(body) > 8:
			upTo := binary.BigEndian.Uint64(body)
			if upTo > math.MaxInt64 {
				continue
			}
			if err := h.model.compactDoc(ctx, issueID, int64(upTo), body[8:]); err != nil {
				slog.Error("collab compact failed", "issue", issueID, "err", err)
				return
			}
		case data[0] == msgSave:
			doc, msg := checkDoc(body)
			if msg != "" {
				slog.Warn("collab save rejected", "issue", issueID, "reason", msg)
				continue
			}
			old, err := h.model.saveDescription(ctx, issueID, doc)
			if err != nil {
				slog.Error("collab save failed", "issue", issueID, "err", err)
				return
			}
			h.notifyMentions(ctx, issueID, userID, old, doc)
		}
	}
}

// write sends queued frames and pings until ctx ends or a write fails, then ends the connection.
func (c *client) write(ctx context.Context, conn *websocket.Conn) {
	defer c.cancel()
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		var err error
		wctx, cancel := context.WithTimeout(ctx, writeWait)
		select {
		case <-ctx.Done():
			cancel()
			return
		case msg := <-c.send:
			err = conn.Write(wctx, websocket.MessageBinary, msg)
		case <-ping.C:
			err = conn.Ping(wctx)
		}
		cancel()
		if err != nil {
			return
		}
	}
}
