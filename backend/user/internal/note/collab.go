package note

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/collab"
	"github.com/devender07/taskwell/backend/user/internal/doc"
	"github.com/devender07/taskwell/backend/user/internal/web"
	"github.com/devender07/taskwell/backend/user/internal/workspace"
)

// docs is where note bodies keep their live state.
var docs = collab.Store{Updates: "note_doc_updates", Key: "note_id", Docs: "notes", Channel: "note_collab"}

// Join checks that the user has the note (a view member joins read-only) and returns its live-editing session, not
// yet started; nil and the message to show when they can't.
func (h *Handler) Join(ctx context.Context, peer collab.Peer, userID, slug, noteID string) (*collab.Session, string, error) {
	ws, err := h.member(ctx, userID, slug)
	if err != nil || ws == nil {
		return nil, errWSNotFound, err
	}
	if ws.Guest {
		return nil, errGuest, nil
	}
	if !web.IsUUID(noteID) {
		return nil, errNoteNotFound, nil
	}
	n, err := h.model.get(ctx, userID, ws.ID, noteID)
	if err != nil || n == nil {
		return nil, errNoteNotFound, err
	}
	d := collab.Doc{ID: n.ID, CanEdit: n.CanEdit, Seedable: n.Body != nil, Generation: n.bodyGen}
	return h.collab.Session(peer, d, ws.ID, userID), "", nil
}

// Listen relays other instances' live-editing frames until ctx ends.
func (h *Handler) Listen(ctx context.Context) { h.collab.Listen(ctx) }

// Listening reports whether the live-editing LISTEN is connected.
func (h *Handler) Listening() bool { return h.collab.Listening() }

// saveFrame stores a live session's save frame as the note's body and tells every member; one that fails the check
// is dropped.
func (m *model) saveFrame(ctx context.Context, noteID, actorID string, body []byte) error {
	b, msg := doc.Check(body, "Note", maxBodyChars)
	if msg != "" {
		slog.Warn("collab save rejected", "note", noteID, "reason", msg)
		return nil
	}
	var stored any // an empty document is stored as null, like a REST write
	if b != nil {
		stored = []byte(b)
	}
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		var slug string
		if err := tx.QueryRow(ctx,
			`update notes n set body = $2, updated_at = now(), updated_by = $3 from workspaces w
			 where n.id = $1 and w.id = n.workspace_id and n.body is distinct from $2::jsonb
			 returning w.slug`, noteID, stored, actorID).Scan(&slug); err != nil {
			return err
		}
		ids, err := memberIDs(ctx, tx, noteID)
		if err != nil {
			return err
		}
		return publish(ctx, tx, &workspace.Workspace{Slug: slug}, actorID, ids)
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) { // unchanged, or the note is gone
		return fmt.Errorf("save note body: %w", err)
	}
	return nil
}
