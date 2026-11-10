// Package feature holds the per-plan feature flags (docs/feature-flags-plan.md): system_config's feature_<name> rows,
// each the comma-separated plan ids it's on for. A workspace has the features of its owner's plan. An off feature's
// endpoints answer 404 through Middleware, and its background work only takes workspaces on Plans.
//
// feature.go has the names, Enabled and the route table with the middleware; routes.go serves GET /api/features.
package feature

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/plan"
	"github.com/devender07/taskwell/backend/user/internal/sysconfig"
	"github.com/devender07/taskwell/backend/user/internal/web"
)

const (
	Chat         = "chat"
	Notes        = "notes"
	Files        = "files"
	Roadmap      = "roadmap"
	Feedback     = "feedback"
	Team         = "team"
	Cycles       = "cycles"
	AITesting    = "ai_testing"
	Integrations = "integrations"
	Webhooks     = "webhooks"
	Templates    = "templates"
	API          = "api"
	Imports      = "imports"
)

var names = []string{Chat, Notes, Files, Roadmap, Feedback, Team, Cycles, AITesting, Integrations, Webhooks, Templates, API, Imports}

// Off is the answer to a request for an off feature.
const Off = "This feature is turned off."

const ws = "/api/workspaces/{slug}"

// routes maps path patterns (http.ServeMux syntax, any method; a trailing / covers the subtree) to their feature.
var routes = map[string]string{
	ws + "/chats":     Chat,
	ws + "/chats/":    Chat,
	ws + "/messages/": Chat,

	ws + "/notes":  Notes,
	ws + "/notes/": Notes,

	// The files endpoints also carry issue attachments (core): only the project file list (no ?issue=) and folders are
	// the feature; uploads and deletes stay open, the UI that makes project files is hidden.
	"GET " + ws + "/projects/{key}/files": Files,
	ws + "/projects/{key}/folders":        Files,
	ws + "/projects/{key}/folders/":       Files,

	ws + "/roadmap":                    Roadmap,
	ws + "/projects/{key}/milestones":  Roadmap,
	ws + "/projects/{key}/milestones/": Roadmap,

	ws + "/feedback":                     Feedback,
	ws + "/feedback/":                    Feedback,
	ws + "/feedback-form":                Feedback,
	ws + "/issues/{identifier}/feedback": Feedback,
	"/api/feedback-forms/":               Feedback,

	ws + "/analytics/":             Team,
	ws + "/members/{id}/analytics": Team,

	ws + "/projects/{key}/cycles":  Cycles,
	ws + "/projects/{key}/cycles/": Cycles,

	ws + "/issues/{identifier}/test-cases":       AITesting,
	ws + "/issues/{identifier}/test-cases/":      AITesting,
	ws + "/issues/{identifier}/test-runs":        AITesting,
	ws + "/issues/{identifier}/playwright":       AITesting,
	ws + "/issues/{identifier}/browser-sessions": AITesting,
	ws + "/test-cases/":                          AITesting,
	ws + "/test-runs/":                           AITesting,
	ws + "/browser-sessions/":                    AITesting,
	ws + "/test-sites":                           AITesting,
	ws + "/test-sites/":                          AITesting,
	ws + "/test-accounts":                        AITesting,
	ws + "/test-accounts/":                       AITesting,
	ws + "/ai-usage":                             AITesting,

	ws + "/integrations":  Integrations,
	ws + "/integrations/": Integrations,
	"/api/hooks/":         Integrations,

	ws + "/webhooks":  Webhooks,
	ws + "/webhooks/": Webhooks,

	ws + "/templates":  Templates,
	ws + "/templates/": Templates,

	"/api/me/tokens":  API,
	"/api/me/tokens/": API,
	"/api/oauth/":     API,
	"/.well-known/oauth-authorization-server": API,

	ws + "/import/": Imports,
}

type Flags struct {
	cfg   *sysconfig.Store
	db    *pgxpool.Pool
	match *http.ServeMux // routes, matched with the API mux's own rules
}

func New(cfg *sysconfig.Store, db *pgxpool.Pool) *Flags {
	m := http.NewServeMux()
	for p := range routes {
		m.Handle(p, http.NotFoundHandler())
	}
	return &Flags{cfg: cfg, db: db, match: m}
}

// Plans returns the ids of the plans the feature is on for, never nil: none while feature_<name> is "false" (off for
// everyone), else those listed in feature_<name>_plans (a missing or unreadable row is every plan). The two are
// separate so turning a feature off and on again keeps its plans.
func (f *Flags) Plans(ctx context.Context, name string) []string {
	if on, _, err := f.cfg.Lookup(ctx, "feature_"+name); err == nil && on == "false" {
		return []string{}
	}
	v, found, err := f.cfg.Lookup(ctx, "feature_"+name+"_plans")
	if err != nil {
		slog.WarnContext(ctx, "feature flag unreadable, treating as on", "feature", name, "err", err)
	}
	if err != nil || !found {
		ids := make([]string, len(plan.Plans))
		for i, p := range plan.Plans {
			ids[i] = p.ID
		}
		return ids
	}
	ids := []string{}
	for _, id := range strings.Split(v, ",") {
		if _, ok := plan.Get(strings.TrimSpace(id)); ok {
			ids = append(ids, strings.TrimSpace(id))
		}
	}
	return ids
}

// Enabled reports whether the feature is on for the plan.
func (f *Flags) Enabled(ctx context.Context, name, planID string) bool {
	return slices.Contains(f.Plans(ctx, name), planID)
}

// AnyPlan reports whether the feature is on for at least one plan.
func (f *Flags) AnyPlan(ctx context.Context, name string) bool { return len(f.Plans(ctx, name)) > 0 }

// planOf returns the plan id of the owner of the workspace the where clause (on workspaces w, $1 = arg) finds, Free
// when the owner has none; false when it finds none or can't tell.
func (f *Flags) planOf(ctx context.Context, where, arg string) (string, bool) {
	var id string
	err := f.db.QueryRow(ctx, `select coalesce(u.plan, 'free') from workspaces w left join users u on u.id = w.created_by
		where `+where, arg).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.WarnContext(ctx, "workspace plan unreadable", "err", err)
	}
	return id, err == nil
}

// on reports whether the feature is on for the plan, or with no plan (ok false), for any plan.
func (f *Flags) on(ctx context.Context, name, planID string, ok bool) bool {
	if !ok {
		return f.AnyPlan(ctx, name)
	}
	return f.Enabled(ctx, name, planID)
}

// InWorkspace reports whether the feature is on for the workspace with the slug.
func (f *Flags) InWorkspace(ctx context.Context, name, slug string) bool {
	id, ok := f.planOf(ctx, `w.slug = lower($1)`, slug)
	return f.on(ctx, name, id, ok)
}

// InChat reports whether the feature is on for the workspace of the chat with the id (a UUID).
func (f *Flags) InChat(ctx context.Context, name, chatID string) bool {
	id, ok := f.planOf(ctx, `w.id = (select workspace_id from chats where id = $1::uuid)`, chatID)
	return f.on(ctx, name, id, ok)
}

// Middleware answers 404 for an off feature's endpoints, and for any Bearer (API token) request while the API is off,
// for the workspace the request is about (its path's slug, feedback form token or git hook token), or for every plan
// when it's about none.
func (f *Flags) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, p := f.match.Handler(r)
		name := routes[p]
		if name == Files && r.URL.Query().Has("issue") {
			name = ""
		}
		tok := bearer(r)
		if name == "" && !tok {
			next.ServeHTTP(w, r)
			return
		}
		ctx := r.Context()
		id, ok := f.requestPlan(ctx, r.URL.Path)
		if name != "" && !f.on(ctx, name, id, ok) || tok && !f.on(ctx, API, id, ok) {
			web.Err(w, http.StatusNotFound, Off)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestPlan returns the plan of the workspace the path is about: /api/workspaces/{slug}/,
// /api/feedback-forms/{token} or /api/hooks/{provider}/{token}; false for any other path.
func (f *Flags) requestPlan(ctx context.Context, path string) (string, bool) {
	seg := strings.Split(path, "/") // "", "api", kind, ...
	if len(seg) < 4 || seg[1] != "api" || seg[3] == "" {
		return "", false
	}
	switch {
	case seg[2] == "workspaces":
		return f.planOf(ctx, `w.slug = lower($1)`, seg[3])
	case seg[2] == "feedback-forms":
		return f.planOf(ctx, `w.feedback_token = $1`, seg[3])
	case seg[2] == "hooks" && len(seg) > 4:
		return f.planOf(ctx, `w.id = (select workspace_id from git_hooks where token = $1)`, seg[4])
	}
	return "", false
}

func bearer(r *http.Request) bool {
	scheme, _, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	return strings.EqualFold(scheme, "Bearer")
}
