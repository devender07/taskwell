// Package chat implements the workspace's chat: the workspace chat everyone is in, each member's notes, direct
// messages and private groups, with threaded replies, reactions and read markers. Mentions and direct messages notify
// through notify.RecordChat; every change is published to the chat's audience, which also receives typing (Audience).
//
// routes.go mounts the endpoints, controller.go handles HTTP (parse, validate, respond), validate.go checks input and
// model.go owns every SQL query.
package chat

import (
	"net/http"

	"github.com/devender07/tether/backend/user/internal/web"
)

// maxBody fits a 10,000-character message as doc JSON.
const maxBody = 512 << 10

// Register mounts the API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	const ws = "/api/workspaces/{slug}"
	mux.HandleFunc("GET "+ws+"/chats", h.wrap(h.list))
	mux.HandleFunc("GET "+ws+"/chats/unread-count", h.wrap(h.unreadCount))
	mux.HandleFunc("POST "+ws+"/chats", h.wrap(h.createGroup))
	mux.HandleFunc("POST "+ws+"/chats/direct", h.wrap(h.direct))
	mux.HandleFunc("GET "+ws+"/chats/{id}", h.wrap(h.get))
	mux.HandleFunc("POST "+ws+"/chats/{id}", h.wrap(h.rename))
	mux.HandleFunc("DELETE "+ws+"/chats/{id}", h.wrap(h.deleteChat))
	mux.HandleFunc("POST "+ws+"/chats/{id}/members", h.wrap(h.addMembers))
	mux.HandleFunc("DELETE "+ws+"/chats/{id}/members/{userId}", h.wrap(h.removeMember))
	mux.HandleFunc("GET "+ws+"/chats/{id}/messages", h.wrap(h.messages))
	mux.HandleFunc("POST "+ws+"/chats/{id}/messages", h.wrap(h.createMessage))
	mux.HandleFunc("POST "+ws+"/chats/{id}/read", h.wrap(h.read))
	mux.HandleFunc("GET "+ws+"/messages/{id}/replies", h.wrap(h.replies))
	mux.HandleFunc("POST "+ws+"/messages/{id}", h.wrap(h.editMessage))
	mux.HandleFunc("DELETE "+ws+"/messages/{id}", h.wrap(h.deleteMessage))
	mux.HandleFunc("POST "+ws+"/messages/{id}/pin", h.wrap(h.pin))
	mux.HandleFunc("DELETE "+ws+"/messages/{id}/pin", h.wrap(h.unpin))
	mux.HandleFunc("POST "+ws+"/messages/{id}/reactions", h.wrap(h.react))
	mux.HandleFunc("DELETE "+ws+"/messages/{id}/reactions", h.wrap(h.unreact))
}

// wrap caps the body size, answers 401 when signed out, and turns a returned error into a logged 500.
func (h *Handler) wrap(fn web.Handler) http.HandlerFunc { return web.Wrap(h.authenticate, maxBody, fn) }
