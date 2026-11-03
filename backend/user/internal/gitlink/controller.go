package gitlink

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/realtime"
	"github.com/devender07/tether/backend/user/internal/secret"
	"github.com/devender07/tether/backend/user/internal/web"
	"github.com/devender07/tether/backend/user/internal/workspace"
)

const (
	github = "github"
	gitlab = "gitlab"

	errNotFound       = "Workspace not found."
	errForbidden      = "You don't have permission to do that."
	errUnknown        = "Unknown provider."
	errStatusNotFound = "Status not found."
)

var providerNames = map[string]string{github: "GitHub", gitlab: "GitLab"}

type Handler struct {
	model        *model
	key          []byte
	baseURL      func(ctx context.Context) (string, error)
	authenticate func(*http.Request) (string, error)
	member       func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)
}

// New takes the key webhook secrets are sealed with (see Key), app_base_url's getter, the auth handler's Authenticate
// and the workspace handler's Member.
func New(db *pgxpool.Pool, key []byte, baseURL func(ctx context.Context) (string, error),
	authenticate func(*http.Request) (string, error),
	member func(ctx context.Context, userID, slug string) (*workspace.Workspace, error)) *Handler {
	return &Handler{model: &model{db: db}, key: key, baseURL: baseURL, authenticate: authenticate, member: member}
}

// Key derives the sealing key from INTEGRATION_SECRET_KEY, which production requires (32+ bytes); in dev, when it's
// unset, from the JWT secret.
func Key(envKey, jwtSecret string, prod bool) ([]byte, error) {
	switch {
	case envKey != "" && (len(envKey) >= 32 || !prod):
		return secret.Key("tether integrations", envKey), nil
	case prod:
		return nil, errors.New("APP_ENV=production requires INTEGRATION_SECRET_KEY of at least 32 bytes (e.g. openssl rand -base64 32)")
	case jwtSecret == "":
		return nil, errors.New("INTEGRATION_SECRET_KEY is unset and there's no jwt_secret to derive one from")
	}
	slog.Warn("INTEGRATION_SECRET_KEY is unset: webhook secrets are encrypted with a key derived from jwt_secret (dev only)")
	return secret.Key("tether integrations", "jwt_secret:"+jwtSecret), nil
}

func sealAD(workspaceID, provider string) []byte { return []byte(workspaceID + ":" + provider) }

func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID)
	if !ok || err != nil {
		return err
	}
	out := make([]*Integration, 0, 2)
	for _, p := range []string{github, gitlab} {
		in, err := h.integration(r.Context(), ws.ID, p)
		if err != nil {
			return err
		}
		if in == nil {
			in = &Integration{Provider: p}
		}
		out = append(out, in)
	}
	web.JSON(w, http.StatusOK, out)
	return nil
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, provider, ok, err := h.provider(w, r, userID)
	if !ok || err != nil {
		return err
	}
	token, plain := web.RandomToken(), web.RandomToken()
	sealed, err := secret.Seal(h.key, sealAD(ws.ID, provider), plain)
	if err != nil {
		return err
	}
	if err := h.model.connect(r.Context(), ws.ID, provider, token, sealed, userID, changed(ws, userID)); err != nil {
		return err
	}
	in, err := h.integration(r.Context(), ws.ID, provider)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"integration": in, "secret": plain})
	return nil
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, provider, ok, err := h.provider(w, r, userID)
	if !ok || err != nil {
		return err
	}
	_ = r.ParseMultipartForm(maxBody)
	errs := map[string]string{}
	read := func(field string) (setting, error) {
		vs, set := r.PostForm[field]
		if !set {
			return setting{}, nil
		}
		id := strings.TrimSpace(vs[0])
		if id == "" {
			return setting{set: true}, nil
		}
		found := false
		if uuidRe.MatchString(id) {
			var err error
			if found, err = h.model.statusExists(r.Context(), ws.ID, id); err != nil {
				return setting{}, err
			}
		}
		if !found {
			errs[field] = errStatusNotFound
		}
		return setting{true, id}, nil
	}
	open, err := read("open_status")
	if err != nil {
		return err
	}
	merge, err := read("merge_status")
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		web.Fields(w, http.StatusBadRequest, errs)
		return nil
	}
	found, err := h.model.settings(r.Context(), ws.ID, provider, open, merge, changed(ws, userID))
	if err != nil {
		return err
	}
	if !found {
		web.Err(w, http.StatusNotFound, notConnected(provider))
		return nil
	}
	in, err := h.integration(r.Context(), ws.ID, provider)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, in)
	return nil
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, provider, ok, err := h.provider(w, r, userID)
	if !ok || err != nil {
		return err
	}
	found, err := h.model.disconnect(r.Context(), ws.ID, provider, changed(ws, userID))
	if err != nil {
		return err
	}
	if !found {
		web.Err(w, http.StatusNotFound, notConnected(provider))
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// hook handles the provider's webhook deliveries.
func (h *Handler) hook(provider string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDelivery))
		if errors.As(err, new(*http.MaxBytesError)) {
			web.Err(w, http.StatusRequestEntityTooLarge, "The payload is over 1 MB.")
			return
		}
		if err != nil {
			web.Err(w, http.StatusBadRequest, "The payload couldn't be read.")
			return
		}
		hk, err := h.model.hookByToken(ctx, provider, r.PathValue("token"))
		if err != nil {
			h.fail(w, r, err)
			return
		}
		if hk == nil {
			web.Err(w, http.StatusNotFound, "Unknown webhook.")
			return
		}
		sec, err := secret.Open(h.key, sealAD(hk.workspaceID, provider), hk.secret)
		if err != nil {
			h.fail(w, r, errors.New("webhook secret "+err.Error()))
			return
		}
		var p pr
		var handled bool
		event := ""
		if provider == github {
			event = r.Header.Get("X-GitHub-Event")
			if !validSignature(sec, r.Header.Get("X-Hub-Signature-256"), body) {
				web.Err(w, http.StatusUnauthorized, "Invalid signature.")
				return
			}
			p, handled, err = parseGitHub(event, body)
		} else {
			event = r.Header.Get("X-Gitlab-Event")
			if subtle.ConstantTimeCompare([]byte(sec), []byte(r.Header.Get("X-Gitlab-Token"))) != 1 {
				web.Err(w, http.StatusUnauthorized, "Invalid token.")
				return
			}
			p, handled, err = parseGitLab(event, body)
		}
		log := slog.With("provider", provider, "workspace_id", hk.workspaceID, "event", event, "action", p.Action)
		if err != nil {
			log.WarnContext(ctx, "webhook", "outcome", "bad payload")
			web.Err(w, http.StatusBadRequest, "The payload isn't valid JSON.")
			return
		}
		if !handled {
			err = h.model.touch(ctx, hk.id)
		} else {
			err = h.model.deliver(ctx, hk.id, provider, p)
		}
		switch {
		case errors.Is(err, errNoHook):
			web.Err(w, http.StatusNotFound, "Unknown webhook.")
		case err != nil:
			h.fail(w, r, err)
		case !handled:
			log.InfoContext(ctx, "webhook", "outcome", "ignored")
			web.JSON(w, http.StatusOK, map[string]bool{"ignored": true})
		default:
			log.InfoContext(ctx, "webhook", "outcome", "ok", "state", p.State)
			web.JSON(w, http.StatusOK, map[string]bool{"ok": true})
		}
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "webhook failed", "err", err)
	web.Err(w, http.StatusInternalServerError, "Something went wrong. Try again.")
}

// validSignature checks GitHub's "sha256=<hex HMAC of the body>" header.
func validSignature(key, header string, body []byte) bool {
	got, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil || !strings.HasPrefix(header, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// integration is the provider's connection with its webhook URL; nil when not connected.
func (h *Handler) integration(ctx context.Context, workspaceID, provider string) (*Integration, error) {
	in, err := h.model.integration(ctx, workspaceID, provider)
	if in == nil || err != nil {
		return nil, err
	}
	base, err := h.baseURL(ctx)
	if err != nil {
		return nil, err
	}
	u := strings.TrimRight(base, "/") + "/api/hooks/" + provider + "/" + in.token
	in.WebhookURL = &u
	return in, nil
}

// provider is access plus the path's provider, or writes a 404 for an unknown one.
func (h *Handler) provider(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, string, bool, error) {
	ws, ok, err := h.access(w, r, userID)
	if !ok || err != nil {
		return nil, "", false, err
	}
	p := r.PathValue("provider")
	if providerNames[p] == "" {
		web.Err(w, http.StatusNotFound, errUnknown)
		return nil, "", false, nil
	}
	return ws, p, true, nil
}

// access returns the path's workspace. It writes a 404 when the caller isn't a member or a 403 without
// integrations.manage, and returns ok=false.
func (h *Handler) access(w http.ResponseWriter, r *http.Request, userID string) (*workspace.Workspace, bool, error) {
	ws, err := h.member(r.Context(), userID, r.PathValue("slug"))
	switch {
	case err != nil:
		return nil, false, err
	case ws == nil:
		web.Err(w, http.StatusNotFound, errNotFound)
		return nil, false, nil
	case !workspace.Has(ws.Permissions, workspace.PermIntegrationsManage):
		web.Err(w, http.StatusForbidden, errForbidden)
		return nil, false, nil
	}
	return ws, true, nil
}

func notConnected(provider string) string { return providerNames[provider] + " isn't connected." }

// changed is a settings change by the user: the integrations page refetches.
func changed(ws *workspace.Workspace, userID string) realtime.Event {
	return realtime.Event{Kind: "integrations.changed", Workspace: ws.Slug, Actor: userID, Audience: realtime.ToWorkspace(ws.ID),
		Paths: []string{integrationsPath(ws.Slug)}}
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
