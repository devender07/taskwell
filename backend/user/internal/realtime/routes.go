// Package realtime keeps one WebSocket per signed-in tab. It carries events naming the REST paths a change touched
// (published with Publish inside the change's transaction, fanned out by every instance's LISTEN), live editing of
// issue descriptions (handed to the issue package through a JoinFunc) and who's viewing an issue.
//
// routes.go mounts the socket, socket.go serves it, hub.go indexes the connections, listen.go fans notifications out,
// publish.go and event.go build events, presence.go tracks viewers and model.go owns every SQL query.
package realtime

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Session is the sign-in a socket runs under. The zero Session means not signed in.
type Session struct {
	UserID  string
	ID      string    // the sign-in (refresh token family)
	Expires time.Time // when the access token it was opened with expires; the socket closes then
}

type Handler struct {
	model        *model
	authenticate func(*http.Request) (Session, error)
	alive        func(context.Context, Session) (bool, error)
	member       func(ctx context.Context, userID, slug string) (string, error)
	joinFn       JoinFunc
	hub          *hub
}

// New takes the auth handler's Session and Alive (the sign-in wasn't revoked and its user isn't suspended), member (the id of the workspace with the slug when the user is in it,
// else "") and the issue handler's live-editing join.
func New(db *pgxpool.Pool, authenticate func(*http.Request) (Session, error), alive func(context.Context, Session) (bool, error),
	member func(ctx context.Context, userID, slug string) (string, error), join JoinFunc) *Handler {
	return &Handler{model: &model{db: db}, authenticate: authenticate, alive: alive, member: member, joinFn: join, hub: newHub()}
}

// Register mounts the socket. It isn't body-capped: the connection is hijacked.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/realtime", h.serve)
}
