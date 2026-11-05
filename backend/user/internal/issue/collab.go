package issue

import (
	"context"

	"github.com/devender07/taskwell/backend/user/internal/collab"
)

// Join checks that the user may open the issue (a member; can_edit decides whether their edits count) and returns
// its live-editing session, not yet started; nil and the message to show when they can't.
func (h *Handler) Join(ctx context.Context, peer collab.Peer, userID, slug, identifier string) (*collab.Session, string, error) {
	ws, err := h.member(ctx, userID, slug)
	if err != nil || ws == nil {
		return nil, errNotFound, err
	}
	i, err := h.find(ctx, ws.ID, identifier, userID)
	if err != nil || i == nil {
		return nil, errIssueNotFound, err
	}
	allow(i, userID, ws.Permissions)
	d := collab.Doc{ID: i.ID, CanEdit: i.CanEdit, Seedable: i.Description != nil, Generation: i.DescriptionGeneration}
	return h.collab.Session(peer, d, ws.ID, userID), "", nil
}

// Listen relays other instances' live-editing frames until ctx ends.
func (h *Handler) Listen(ctx context.Context) { h.collab.Listen(ctx) }

// Listening reports whether the live-editing LISTEN is connected.
func (h *Handler) Listening() bool { return h.collab.Listening() }
