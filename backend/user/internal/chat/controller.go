package chat

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/web"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

const (
	errNoWorkspace = "Workspace not found."
	errNoChat      = "Chat not found."
	errNoMessage   = "Message not found."
	errPeople      = "Someone you picked isn't in this workspace."
	errNoPeople    = "Add at least one person."
	errTooManyPpl  = "Groups can have up to 50 people."
)

type Handler struct {
	model        *model
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
}

// New takes the auth handler's Authenticate and the workspace handler's Member.
func New(db *pgxpool.Pool, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, authenticate: authenticate, member: member}
}

// Audience returns who sees the chat as realtime audiences, or nil when the user can't open it; for typing frames.
func (h *Handler) Audience(ctx context.Context, userID, chatID string) ([]string, error) {
	if !web.IsUUID(chatID) {
		return nil, nil
	}
	return h.model.chatAudience(ctx, userID, chatID)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, ok, err := h.scope(w, r, userID)
	if !ok || err != nil {
		return err
	}
	cs, err := h.model.list(r.Context(), userID, sc.wsID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, cs)
	return nil
}

func (h *Handler) unreadCount(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, ok, err := h.scope(w, r, userID)
	if !ok || err != nil {
		return err
	}
	n, err := h.model.unreadCount(r.Context(), userID, sc.wsID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]int{"count": n})
	return nil
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, c, err := h.chat(w, r, userID)
	if c == nil || err != nil {
		return err
	}
	return h.respondChat(w, r, sc, c.id, http.StatusOK)
}

func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, ok, err := h.scope(w, r, userID)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	name := strings.TrimSpace(r.PostForm.Get("name"))
	ids, valid := people(r.PostForm["member_ids"], userID)
	errs := map[string]string{}
	if msg := checkName(name); msg != "" {
		errs["name"] = msg
	}
	switch {
	case !valid:
		errs["member_ids"] = errPeople
	case len(ids) == 0:
		errs["member_ids"] = errNoPeople
	case len(ids)+1 > maxMembers:
		errs["member_ids"] = errTooManyPpl
	}
	if len(errs) > 0 {
		web.Fields(w, http.StatusBadRequest, errs)
		return nil
	}
	id, err := h.model.createGroup(r.Context(), sc, name, ids)
	if errors.Is(err, errNotMember) {
		web.Fields(w, http.StatusBadRequest, map[string]string{"member_ids": errPeople})
		return nil
	}
	if err != nil {
		return err
	}
	return h.respondChat(w, r, sc, id, http.StatusCreated)
}

func (h *Handler) direct(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, ok, err := h.scope(w, r, userID)
	if !ok || err != nil {
		return err
	}
	other := strings.TrimSpace(r.FormValue("user_id"))
	bad := func() error {
		web.Fields(w, http.StatusBadRequest, map[string]string{"user_id": "Choose someone in this workspace."})
		return nil
	}
	if !web.IsUUID(other) || other == userID {
		return bad()
	}
	id, err := h.model.direct(r.Context(), sc, other)
	if errors.Is(err, errNotMember) {
		return bad()
	}
	if err != nil {
		return err
	}
	return h.respondChat(w, r, sc, id, http.StatusOK)
}

func (h *Handler) rename(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, c, err := h.chat(w, r, userID)
	if c == nil || err != nil {
		return err
	}
	if c.kind != "group" {
		web.Err(w, http.StatusForbidden, "Only groups can be renamed.")
		return nil
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if msg := checkName(name); msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"name": msg})
		return nil
	}
	if err := h.model.rename(r.Context(), sc, c.id, name); err != nil {
		return err
	}
	return h.respondChat(w, r, sc, c.id, http.StatusOK)
}

func (h *Handler) addMembers(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, c, err := h.chat(w, r, userID)
	if c == nil || err != nil {
		return err
	}
	if c.kind != "group" {
		web.Err(w, http.StatusForbidden, "Only groups can have people added.")
		return nil
	}
	_ = r.ParseMultipartForm(maxBody)
	ids, valid := people(r.PostForm["user_ids"], userID)
	fail := func(msg string) error {
		web.Fields(w, http.StatusBadRequest, map[string]string{"user_ids": msg})
		return nil
	}
	switch {
	case !valid:
		return fail(errPeople)
	case len(ids) == 0:
		return fail(errNoPeople)
	}
	switch err := h.model.addMembers(r.Context(), sc, c.id, ids); {
	case errors.Is(err, errNotMember):
		return fail(errPeople)
	case errors.Is(err, errTooMany):
		return fail(errTooManyPpl)
	case err != nil:
		return err
	}
	return h.respondChat(w, r, sc, c.id, http.StatusOK)
}

// removeMember takes someone out of a group: yourself (leaving), or anyone when you created it.
func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, c, err := h.chat(w, r, userID)
	if c == nil || err != nil {
		return err
	}
	target := r.PathValue("userId")
	switch {
	case c.kind != "group":
		web.Err(w, http.StatusForbidden, "Only groups can have people removed.")
		return nil
	case target != userID && c.createdBy != userID:
		web.Err(w, http.StatusForbidden, "Only the group's creator can remove people.")
		return nil
	}
	if web.IsUUID(target) {
		if err := h.model.removeMember(r.Context(), sc, c.id, target); err != nil {
			return err
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// deleteChat deletes the chat and all its messages for everyone in it: a direct chat by either person, Notes by its
// owner, a group by its creator. The workspace chat can't be deleted.
func (h *Handler) deleteChat(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, c, err := h.chat(w, r, userID)
	if c == nil || err != nil {
		return err
	}
	switch {
	case c.kind == "workspace":
		web.Err(w, http.StatusForbidden, "The workspace chat can't be deleted.")
		return nil
	case c.kind == "group" && c.createdBy != userID:
		web.Err(w, http.StatusForbidden, "Only the group's creator can delete it.")
		return nil
	}
	if err := h.model.deleteChat(r.Context(), sc, c.id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) messages(w http.ResponseWriter, r *http.Request, userID string) error {
	_, c, err := h.chat(w, r, userID)
	if c == nil || err != nil {
		return err
	}
	before, ok := cursor(w, r)
	if !ok {
		return nil
	}
	ms, more, err := h.model.messages(r.Context(), userID, c.id, before)
	if err != nil {
		return err
	}
	pinned, err := h.model.pinned(r.Context(), userID, c.id)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"messages": ms, "has_more": more, "pinned": pinned})
	return nil
}

func (h *Handler) createMessage(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, c, err := h.chat(w, r, userID)
	if c == nil || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	body, msg := checkBody(r.PostForm.Get("body"))
	parent := strings.TrimSpace(r.PostForm.Get("parent_id"))
	errs := map[string]string{}
	if msg != "" {
		errs["body"] = msg
	}
	const errParent = "You can only reply to a message in this chat."
	if parent != "" && !web.IsUUID(parent) {
		errs["parent_id"] = errParent
	}
	if len(errs) > 0 {
		web.Fields(w, http.StatusBadRequest, errs)
		return nil
	}
	id, err := h.model.createMessage(r.Context(), sc, *c, parent, body)
	if errors.Is(err, errBadParent) {
		web.Fields(w, http.StatusBadRequest, map[string]string{"parent_id": errParent})
		return nil
	}
	if err != nil {
		return err
	}
	return h.respondMessage(w, r, userID, id, http.StatusCreated)
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, c, err := h.chat(w, r, userID)
	if c == nil || err != nil {
		return err
	}
	id := strings.TrimSpace(r.FormValue("message_id"))
	if id == "" || web.IsUUID(id) {
		if err := h.model.read(r.Context(), sc, c.id, id); err != nil {
			return err
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) replies(w http.ResponseWriter, r *http.Request, userID string) error {
	_, m, err := h.message(w, r, userID)
	if m == nil || err != nil {
		return err
	}
	if m.parentID != "" {
		web.Err(w, http.StatusNotFound, errNoMessage)
		return nil
	}
	before, ok := cursor(w, r)
	if !ok {
		return nil
	}
	root, err := h.model.message(r.Context(), userID, m.id)
	if err != nil {
		return err
	}
	rs, more, err := h.model.replies(r.Context(), userID, m.id, before)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"root": root, "replies": rs, "has_more": more})
	return nil
}

func (h *Handler) editMessage(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, m, err := h.own(w, r, userID, "You can only edit your own messages.")
	if m == nil || err != nil {
		return err
	}
	body, msg := checkBody(r.FormValue("body"))
	if msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"body": msg})
		return nil
	}
	if err := h.model.editMessage(r.Context(), sc, *m, body); err != nil {
		return err
	}
	return h.respondMessage(w, r, userID, m.id, http.StatusOK)
}

func (h *Handler) deleteMessage(w http.ResponseWriter, r *http.Request, userID string) error {
	sc, m, err := h.own(w, r, userID, "You can only delete your own messages.")
	if m == nil || err != nil {
		return err
	}
	if err := h.model.deleteMessage(r.Context(), sc, *m); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) pin(w http.ResponseWriter, r *http.Request, userID string) error {
	days, msg := checkPinDays(r.FormValue("days"))
	if msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"days": msg})
		return nil
	}
	return h.setPin(w, r, userID, days)
}

func (h *Handler) unpin(w http.ResponseWriter, r *http.Request, userID string) error {
	return h.setPin(w, r, userID, 0)
}

// setPin pins (days > 0) or unpins a top-level message that isn't deleted; a pin in place is only its pinner's to
// change.
func (h *Handler) setPin(w http.ResponseWriter, r *http.Request, userID string, days int) error {
	sc, m, err := h.message(w, r, userID)
	if m == nil || err != nil {
		return err
	}
	if m.deleted || m.parentID != "" {
		web.Err(w, http.StatusNotFound, errNoMessage)
		return nil
	}
	if m.pinned && m.pinnedBy != "" && m.pinnedBy != userID {
		web.Err(w, http.StatusForbidden, "Only the person who pinned this message can change its pin.")
		return nil
	}
	if err := h.model.pin(r.Context(), sc, *m, days); err != nil {
		return err
	}
	return h.respondMessage(w, r, userID, m.id, http.StatusOK)
}

func (h *Handler) react(w http.ResponseWriter, r *http.Request, userID string) error {
	return h.reaction(w, r, userID, strings.TrimSpace(r.FormValue("emoji")), true)
}

func (h *Handler) unreact(w http.ResponseWriter, r *http.Request, userID string) error {
	return h.reaction(w, r, userID, strings.TrimSpace(r.URL.Query().Get("emoji")), false)
}

// reaction adds or removes the user's emoji on a message that isn't deleted.
func (h *Handler) reaction(w http.ResponseWriter, r *http.Request, userID, emoji string, add bool) error {
	sc, m, err := h.message(w, r, userID)
	if m == nil || err != nil {
		return err
	}
	if m.deleted {
		web.Err(w, http.StatusNotFound, errNoMessage)
		return nil
	}
	msg := checkEmoji(emoji)
	if msg != "" && add {
		web.Fields(w, http.StatusBadRequest, map[string]string{"emoji": msg})
		return nil
	}
	if msg == "" {
		if err := h.model.react(r.Context(), sc, *m, emoji, add); err != nil {
			return err
		}
	}
	return h.respondMessage(w, r, userID, m.id, http.StatusOK)
}

// --- access ---

// scope returns who's asking in the path's workspace, or writes a 404 and returns ok=false when they aren't a member.
func (h *Handler) scope(w http.ResponseWriter, r *http.Request, userID string) (scope, bool, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	if err != nil {
		return scope{}, false, err
	}
	if ws == nil {
		web.Err(w, http.StatusNotFound, errNoWorkspace)
		return scope{}, false, nil
	}
	return scope{userID: userID, wsID: ws.ID, slug: ws.Slug}, true, nil
}

// chat returns the path's chat, or writes a 404 and returns nil when the user can't open it.
func (h *Handler) chat(w http.ResponseWriter, r *http.Request, userID string) (scope, *ref, error) {
	sc, ok, err := h.scope(w, r, userID)
	if !ok || err != nil {
		return sc, nil, err
	}
	var c *ref
	if id := r.PathValue("id"); web.IsUUID(id) {
		if c, err = h.model.chat(r.Context(), userID, sc.wsID, id); err != nil {
			return sc, nil, err
		}
	}
	if c == nil {
		web.Err(w, http.StatusNotFound, errNoChat)
	}
	return sc, c, nil
}

// message returns the path's message, or writes a 404 and returns nil when the user can't see it.
func (h *Handler) message(w http.ResponseWriter, r *http.Request, userID string) (scope, *msgRef, error) {
	sc, ok, err := h.scope(w, r, userID)
	if !ok || err != nil {
		return sc, nil, err
	}
	var m *msgRef
	if id := r.PathValue("id"); web.IsUUID(id) {
		if m, err = h.model.messageRef(r.Context(), userID, sc.wsID, id); err != nil {
			return sc, nil, err
		}
	}
	if m == nil {
		web.Err(w, http.StatusNotFound, errNoMessage)
	}
	return sc, m, nil
}

// own is message for one the user wrote and hasn't deleted: someone else's is a 403 with forbidden, a deleted one 404.
func (h *Handler) own(w http.ResponseWriter, r *http.Request, userID, forbidden string) (scope, *msgRef, error) {
	sc, m, err := h.message(w, r, userID)
	switch {
	case m == nil || err != nil:
		return sc, nil, err
	case !m.mine:
		web.Err(w, http.StatusForbidden, forbidden)
		return sc, nil, nil
	case m.deleted:
		web.Err(w, http.StatusNotFound, errNoMessage)
		return sc, nil, nil
	}
	return sc, m, nil
}

// cursor reads ?before=, or writes a 400 and returns ok=false when it isn't a message id.
func cursor(w http.ResponseWriter, r *http.Request) (string, bool) {
	before := r.URL.Query().Get("before")
	if before != "" && !web.IsUUID(before) {
		web.Err(w, http.StatusBadRequest, "Invalid cursor.")
		return "", false
	}
	return before, true
}

// --- responses ---

// respondChat writes the chat as the user sees it, or a 404 when they no longer can.
func (h *Handler) respondChat(w http.ResponseWriter, r *http.Request, sc scope, id string, status int) error {
	c, err := h.model.get(r.Context(), sc.userID, sc.wsID, id)
	if err != nil {
		return err
	}
	if c == nil {
		web.Err(w, http.StatusNotFound, errNoChat)
		return nil
	}
	web.JSON(w, status, c)
	return nil
}

func (h *Handler) respondMessage(w http.ResponseWriter, r *http.Request, userID, id string, status int) error {
	m, err := h.model.message(r.Context(), userID, id)
	if err != nil {
		return err
	}
	web.JSON(w, status, m)
	return nil
}
