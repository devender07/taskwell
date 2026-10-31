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

	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	model        *model
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (string, error)
	joinFn       JoinFunc
	hub          *hub
}

// New takes the auth handler's Authenticate, member (the id of the workspace with the slug when the user is in it,
// else "") and the issue handler's live-editing join.
func New(db *pgxpool.Pool, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (string, error), join JoinFunc) *Handler {
	return &Handler{model: &model{db: db}, authenticate: authenticate, member: member, joinFn: join, hub: newHub()}
}

// Register mounts the socket. It isn't body-capped: the connection is hijacked.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/realtime", h.serve)
}
