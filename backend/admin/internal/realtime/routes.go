// Package realtime keeps one WebSocket per admin tab and sends it the admin-audience events every service publishes
// (with Publish, inside the change's transaction) on the Postgres channel realtime. Text events only.
//
// routes.go mounts and serves the socket, listen.go fans notifications out and publish.go builds events.
package realtime

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/admin/internal/web"
)

const (
	readLimit   = 4 << 10 // clients only send pings
	eventBuffer = 64
	writeWait   = 10 * time.Second
	pingEvery   = 30 * time.Second
)

type Handler struct {
	db    *pgxpool.Pool
	auth  web.Authenticator
	mu    sync.Mutex
	conns map[*conn]struct{}
}

// conn is one admin tab. The hub queues events on events; a writer goroutine drains it.
type conn struct {
	events chan []byte
	cancel context.CancelFunc
}

// New takes the auth handler's Authenticate, which checks the account is an unsuspended platform admin.
func New(db *pgxpool.Pool, auth web.Authenticator) *Handler {
	return &Handler{db: db, auth: auth, conns: map[*conn]struct{}{}}
}

// Register mounts the socket. It isn't body-capped: the connection is hijacked.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/realtime", h.serve)
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	userID, err := h.auth(r)
	if err != nil {
		slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		web.WriteErr(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	if userID == "" {
		web.WriteErr(w, http.StatusUnauthorized, "Not signed in.")
		return
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has written the error response
	}
	defer ws.CloseNow()
	ws.SetReadLimit(readLimit)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c := &conn{events: make(chan []byte, eventBuffer), cancel: cancel}
	c.send(text(map[string]string{"type": "hello", "client": rand.Text()}))
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.conns, c)
		h.mu.Unlock()
	}()
	go c.write(ctx, ws)
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var m struct{ Type string }
		if typ == websocket.MessageText && json.Unmarshal(data, &m) == nil && m.Type == "ping" {
			c.send(text(map[string]string{"type": "pong"}))
		}
	}
}

// send queues a message, ending a connection that can't keep up.
func (c *conn) send(msg []byte) {
	select {
	case c.events <- msg:
	default:
		c.cancel()
	}
}

// Sockets returns the number of admin tabs connected here.
func (h *Handler) Sockets() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns)
}

// toAll queues msg for every admin tab here.
func (h *Handler) toAll(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		c.send(msg)
	}
}

// write sends queued messages and pings until ctx ends or a write fails, then ends the connection.
func (c *conn) write(ctx context.Context, ws *websocket.Conn) {
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

func text(v any) []byte {
	b, _ := json.Marshal(v) // maps of strings only
	return b
}
