package issue

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"time"
)

// Live-editing frame types: the first byte of every binary frame. Bytes 1-16 are the issue's uuid (see tag).
const (
	msgReady byte = iota
	msgUpdate
	msgAwareness
	msgCompact
	msgSave
	msgSeed
)

// hub holds a room per issue being edited live on this API instance; listen.go relays other instances' frames.
type hub struct {
	mu    sync.Mutex
	rooms map[string]*room
}

type room struct {
	clients map[*client]struct{}
	seeder  *client // the editor here told to seed the document from the saved description, until its seed is stored
}

// client is one session in a room. The hub queues tagged frames on its peer.
type client struct {
	peer    Peer
	canEdit bool
	loaded  []int64   // ids of the stored updates sent on start: the only ones its compaction may delete
	tokens  float64   // frame budget, see allow
	last    time.Time // when tokens was last topped up
}

// Live-editing limits. A connection past them is closed; its tab reconnects and reloads.
const (
	maxUpdate  = 256 << 10 // one update; a 50,000-character paste is well under
	maxCompact = 1 << 20   // a compacted document
	maxStored  = 8 << 20   // all of an issue's stored updates
	frameRate  = 60        // frames a second per session, sustained
	frameBurst = 200
)

// allow spends one of the client's frame tokens, which refill at frameRate up to frameBurst.
func (c *client) allow(now time.Time) bool {
	c.tokens = min(frameBurst, c.tokens+now.Sub(c.last).Seconds()*frameRate)
	c.last = now
	if c.tokens < 1 {
		return false
	}
	c.tokens--
	return true
}

// compactable returns the ids c loaded up to and including upTo, once: the client compacts only right after ready.
func (c *client) compactable(upTo int64) []int64 {
	ids := slices.DeleteFunc(c.loaded, func(id int64) bool { return id > upTo })
	c.loaded = nil
	return ids
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

// leave removes c. When c was seeding and its seed isn't stored, it hands the seed to another editor in the room;
// the caller then refreshes the database claim (passed) or clears it for any instance's next editor.
func (hb *hub) leave(issueID string, c *client) (seeding, passed bool) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	r := hb.rooms[issueID]
	if r == nil {
		return false, false
	}
	delete(r.clients, c)
	if len(r.clients) == 0 {
		delete(hb.rooms, issueID)
	}
	if r.seeder != c {
		return false, false
	}
	return true, r.passSeed(issueID)
}

// passSeed hands an unfinished seed to another editor in the room. Called with hb.mu held.
// ponytail: the new seeder skips seeding if its user already typed into the empty document; their text then
// replaces the saved description. Rare: needs the first editor to leave within a second of opening the issue.
func (r *room) passSeed(issueID string) bool {
	r.seeder = nil
	for c := range r.clients {
		if c.canEdit && c.peer.Send(tag(msgSeed, issueID, nil)) {
			r.seeder = c
			return true
		}
	}
	return false
}

// seeding records that c won the database seed claim.
func (hb *hub) seeding(issueID string, c *client) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	if r := hb.rooms[issueID]; r != nil {
		r.seeder = c
	}
}

// stored records that c's update is in the database; the seeder's first one is the seed.
func (hb *hub) stored(issueID string, c *client) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	if r := hb.rooms[issueID]; r != nil && r.seeder == c {
		r.seeder = nil
	}
}

// has reports whether any connection here is on the issue.
func (hb *hub) has(issueID string) bool {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	return hb.rooms[issueID] != nil
}

// closeAll ends every connection; their editors reconnect and reload from the database.
func (hb *hub) closeAll() {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	for _, r := range hb.rooms {
		for c := range r.clients {
			c.peer.Close()
		}
	}
}

// closeRoom ends every connection on the issue here; their editors reconnect and reload from the database.
func (hb *hub) closeRoom(issueID string) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	if r := hb.rooms[issueID]; r != nil {
		for c := range r.clients {
			c.peer.Close()
		}
	}
}

// broadcast queues msg, a tagged frame, for every session on the issue but from, ending any connection whose buffer
// is full (it then leaves the room).
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
		if !c.peer.Send(msg) {
			c.peer.Close()
		}
	}
}

// Peer is a realtime connection as live editing needs it.
type Peer interface {
	Send(frame []byte) bool                        // queue without waiting; false when the buffer is full
	Write(ctx context.Context, frame []byte) error // queue, waiting for room
	Close()                                        // end the connection; its tab reconnects and reloads
}

// Session is one connection's live editing of one issue, joined over the realtime socket.
type Session struct {
	h      *Handler
	c      *client
	issue  *Issue
	wsID   string
	userID string
}

// Join checks that the user may open the issue (a member; can_edit decides whether their edits count) and returns
// its session, not yet started; nil and the message to show when they can't.
func (h *Handler) Join(ctx context.Context, peer Peer, userID, slug, identifier string) (*Session, string, error) {
	ws, err := h.member(ctx, userID, slug)
	if err != nil || ws == nil {
		return nil, errNotFound, err
	}
	i, err := h.find(ctx, ws.ID, identifier)
	if err != nil || i == nil {
		return nil, errIssueNotFound, err
	}
	allow(i, userID, ws.Permissions)
	return &Session{h: h, c: &client{peer: peer, canEdit: i.CanEdit}, issue: i, wsID: ws.ID, userID: userID}, "", nil
}

func (s *Session) ID() string { return s.issue.ID }

func (s *Session) WorkspaceID() string { return s.wsID }

// Start enters the issue's room and sends the stored updates, then ready.
func (s *Session) Start(ctx context.Context) error {
	// Join before loading: an update stored after the load is broadcast to us, one stored before is in the load
	// (and maybe broadcast too; Yjs ignores duplicates).
	s.h.hub.join(s.issue.ID, s.c)
	if err := s.h.sendStored(ctx, s.issue, s.c); err != nil {
		return fmt.Errorf("collab load: %w", err)
	}
	return nil
}

// Leave takes the session out of its room and updates the seed claim when it was seeding.
func (s *Session) Leave(ctx context.Context) { s.h.leave(ctx, s.issue.ID, s.c) }

// sendStored sends the issue's stored updates, then ready.
func (h *Handler) sendStored(ctx context.Context, i *Issue, c *client) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	us, err := h.model.docUpdates(ctx, i.ID)
	if err != nil {
		return err
	}
	var last int64
	c.loaded = c.loaded[:0]
	for _, u := range us {
		if err := c.peer.Write(ctx, tag(msgUpdate, i.ID, u.data)); err != nil {
			return err
		}
		last = u.id
		c.loaded = append(c.loaded, u.id)
	}
	var flags byte
	if i.CanEdit {
		flags |= 1
	}
	// Only an editor seeds: a read-only client's seed update would be ignored, leaving the room unseeded for good.
	if i.CanEdit && len(us) == 0 && i.Description != nil {
		ok, err := h.model.claimSeed(ctx, i.ID)
		if err != nil {
			return err
		}
		if ok {
			h.hub.seeding(i.ID, c)
			flags |= 2
		}
	}
	return c.peer.Write(ctx, tag(msgReady, i.ID, binary.BigEndian.AppendUint64([]byte{flags}, uint64(last))))
}

// leave takes c out of its room and updates the seed claim when c was seeding.
func (h *Handler) leave(ctx context.Context, issueID string, c *client) {
	seeding, passed := h.hub.leave(issueID, c)
	if !seeding {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := h.model.setSeedClaim(ctx, issueID, passed); err != nil {
		slog.Error("collab seed claim failed", "issue", issueID, "err", err)
	}
}

// Frame handles one binary frame from the client: type, the issue's uuid, payload. An error ends the connection.
func (s *Session) Frame(ctx context.Context, data []byte) error {
	if len(data) < tagLen+1 {
		return nil
	}
	h, c, issueID := s.h, s.c, s.issue.ID
	body := data[tagLen:]
	if !c.allow(time.Now()) {
		s.reject("rate")
		return nil
	}
	switch {
	case data[0] == msgAwareness:
		h.hub.broadcast(issueID, c, data)
		if p := inlineNote(issueID, data); p != "" {
			if err := h.model.notify(ctx, p); err != nil {
				slog.Warn("collab awareness notify failed", "issue", issueID, "err", err)
			}
		}
	case !c.canEdit:
	case data[0] == msgUpdate:
		if len(body) > maxUpdate {
			s.reject("update size")
			return nil
		}
		ok, err := h.model.addDocUpdate(ctx, issueID, body, inlineNote(issueID, data))
		if err != nil {
			return err
		}
		if !ok {
			s.reject("stored size")
			return nil
		}
		h.hub.stored(issueID, c)
		h.hub.broadcast(issueID, c, data)
	case data[0] == msgCompact && len(body) > 8:
		if len(body)-8 > maxCompact {
			s.reject("compact size")
			return nil
		}
		upTo := binary.BigEndian.Uint64(body)
		if upTo > math.MaxInt64 {
			return nil
		}
		if ids := c.compactable(int64(upTo)); len(ids) > 0 {
			return h.model.compactDoc(ctx, issueID, ids, body[8:])
		}
	case data[0] == msgSave:
		doc, msg := checkDescription(body)
		if msg != "" {
			slog.Warn("collab save rejected", "issue", issueID, "reason", msg)
			return nil
		}
		return h.model.saveDescription(ctx, issueID, s.userID, doc)
	}
	return nil
}

// reject closes the session's connection for breaking a live-editing limit.
func (s *Session) reject(reason string) {
	slog.Warn("collab frame rejected", "issue", s.issue.ID, "user_id", s.userID, "reason", reason)
	s.c.peer.Close()
}

// tagLen is a frame's type byte and issue uuid.
const tagLen = 17

// tag builds a frame for the issue: the type, the issue's uuid as 16 raw bytes, the payload.
func tag(typ byte, issueID string, payload []byte) []byte {
	b := make([]byte, tagLen, tagLen+len(payload))
	b[0] = typ
	_, _ = hex.Decode(b[1:], []byte(strings.ReplaceAll(issueID, "-", ""))) // a uuid from the database
	return append(b, payload...)
}
