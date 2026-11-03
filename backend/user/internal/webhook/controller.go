package webhook

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/netguard"
	"github.com/devender07/tether/backend/user/internal/realtime"
	"github.com/devender07/tether/backend/user/internal/secret"
	"github.com/devender07/tether/backend/user/internal/web"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

const (
	errNoWorkspace = "Workspace not found."
	errForbidden   = "You don't have permission to do that."
	errNoWebhook   = "Webhook not found."
	errNoDelivery  = "Delivery not found."
	errTooMany     = "A workspace can have up to 10 webhooks."
)

// Options: Key seals webhook secrets (gitlink.Key's); AllowHTTP permits http:// URLs (outside production) and
// AllowPrivate URLs on private or local addresses (AI_TEST_ALLOW_PRIVATE, dev only).
type Options struct {
	Key          []byte
	AllowHTTP    bool
	AllowPrivate bool
}

type Handler struct {
	model        *model
	opts         Options
	resolve      netguard.Resolver
	client       *http.Client
	wake         chan struct{}
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
}

// New takes the auth handler's Authenticate and the workspace handler's Member.
func New(db *pgxpool.Pool, opts Options, authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, opts: opts, resolve: netguard.Lookup, client: newClient(opts.AllowPrivate),
		wake: make(chan struct{}, 1), authenticate: authenticate, member: member}
}

func sealAD(wsID, id string) []byte { return []byte(wsID + ":webhook:" + id) }

func newSecret() string { return "whsec_" + web.RandomToken() }

func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID)
	if !ok || err != nil {
		return err
	}
	whs, err := h.model.list(r.Context(), ws.ID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, whs)
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	url, urlMsg := checkURL(r.Context(), h.resolve, r.PostForm.Get("url"), h.opts.AllowHTTP, h.opts.AllowPrivate)
	events, eventsMsg := checkEvents(r.PostForm["events"])
	if !web.Check(w, map[string]string{"url": urlMsg, "events": eventsMsg}) {
		return nil
	}
	id, err := h.model.newID(r.Context())
	if err != nil {
		return err
	}
	plain := newSecret()
	sealed, err := secret.Seal(h.opts.Key, sealAD(ws.ID, id), plain)
	if err != nil {
		return err
	}
	created, err := h.model.create(r.Context(), changed(ws, userID), ws.ID, id, url, events, sealed, userID)
	if err != nil {
		return err
	}
	if !created {
		web.Err(w, http.StatusConflict, errTooMany)
		return nil
	}
	wh, err := h.model.get(r.Context(), ws.ID, id)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, map[string]any{"webhook": wh, "secret": plain})
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, id, ok, err := h.webhook(w, r, userID)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	msgs := map[string]string{}
	var url *string
	if vs, set := r.PostForm["url"]; set {
		u, msg := checkURL(r.Context(), h.resolve, vs[0], h.opts.AllowHTTP, h.opts.AllowPrivate)
		url, msgs["url"] = &u, msg
	}
	var events []string
	if vs, set := r.PostForm["events"]; set {
		events, msgs["events"] = checkEvents(vs)
	}
	var active *bool
	if vs, set := r.PostForm["active"]; set {
		v := vs[0] == "true"
		active = &v
	}
	if !web.Check(w, msgs) {
		return nil
	}
	found, err := h.model.update(r.Context(), changed(ws, userID), ws.ID, id, url, events, active)
	if err != nil {
		return err
	}
	if !found {
		web.Err(w, http.StatusNotFound, errNoWebhook)
		return nil
	}
	wh, err := h.model.get(r.Context(), ws.ID, id)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, wh)
	return nil
}

func (h *Handler) rollSecret(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, id, ok, err := h.webhook(w, r, userID)
	if !ok || err != nil {
		return err
	}
	plain := newSecret()
	sealed, err := secret.Seal(h.opts.Key, sealAD(ws.ID, id), plain)
	if err != nil {
		return err
	}
	found, err := h.model.setSecret(r.Context(), changed(ws, userID), ws.ID, id, sealed)
	if err != nil || !h.found(w, found, errNoWebhook) {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]string{"secret": plain})
	return nil
}

func (h *Handler) test(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, id, ok, err := h.webhook(w, r, userID)
	if !ok || err != nil {
		return err
	}
	found, err := h.model.ping(r.Context(), changed(ws, userID, deliveriesPath(ws.Slug, id)), ws.ID, id, userID)
	if err != nil || !h.found(w, found, errNoWebhook) {
		return err
	}
	h.Wake()
	web.JSON(w, http.StatusAccepted, struct{}{})
	return nil
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, id, ok, err := h.webhook(w, r, userID)
	if !ok || err != nil {
		return err
	}
	found, err := h.model.delete(r.Context(), changed(ws, userID), ws.ID, id)
	if err != nil || !h.found(w, found, errNoWebhook) {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) deliveries(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, id, ok, err := h.webhook(w, r, userID)
	if !ok || err != nil {
		return err
	}
	ds, found, err := h.model.deliveries(r.Context(), ws.ID, id)
	if err != nil || !h.found(w, found, errNoWebhook) {
		return err
	}
	if ds == nil {
		ds = []Delivery{}
	}
	web.JSON(w, http.StatusOK, ds)
	return nil
}

func (h *Handler) redeliver(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, id, ok, err := h.webhook(w, r, userID)
	if !ok || err != nil {
		return err
	}
	deliveryID := r.PathValue("deliveryId")
	if !web.IsUUID(deliveryID) {
		web.Err(w, http.StatusNotFound, errNoDelivery)
		return nil
	}
	found, err := h.model.redeliver(r.Context(), changed(ws, userID, deliveriesPath(ws.Slug, id)), ws.ID, id, deliveryID)
	if err != nil || !h.found(w, found, errNoDelivery) {
		return err
	}
	h.Wake()
	web.JSON(w, http.StatusAccepted, struct{}{})
	return nil
}

// found writes msg as a 404 unless found.
func (h *Handler) found(w http.ResponseWriter, found bool, msg string) bool {
	if !found {
		web.Err(w, http.StatusNotFound, msg)
	}
	return found
}

// access returns the path's workspace. It writes a 404 when the caller isn't a member or a 403 without
// integrations.manage, and returns ok=false.
func (h *Handler) access(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, bool, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return nil, false, err
	case ws == nil:
		web.Err(w, http.StatusNotFound, errNoWorkspace)
		return nil, false, nil
	case !workspace.Has(ws.Permissions, workspace.PermIntegrationsManage):
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil, false, nil
	}
	return ws, true, nil
}

// webhook is access plus the path's webhook id, answering 404 when it isn't a UUID.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, string, bool, error) {
	ws, ok, err := h.access(w, r, userID)
	if !ok || err != nil {
		return nil, "", false, err
	}
	id := r.PathValue("id")
	if !web.IsUUID(id) {
		web.Err(w, http.StatusNotFound, errNoWebhook)
		return nil, "", false, nil
	}
	return ws, id, true, nil
}

// changed is the event telling the workspace's tabs the webhooks list (and paths) changed.
func changed(ws *workspace.Workspace, userID string, paths ...string) realtime.Event {
	return realtime.Event{Kind: "webhooks.changed", Workspace: ws.Slug, Actor: userID, Audience: realtime.ToWorkspace(ws.ID),
		Paths: append(paths, webhooksPath(ws.Slug))}
}
