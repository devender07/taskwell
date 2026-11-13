// Package plan is the billing plans and their limits (docs/billing-plan.md). A plan belongs to an account; a
// workspace has the limits of its creator's plan. It's a leaf so the workspace, project and file packages can check
// limits without importing billing, which imports workspace.
package plan

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

// Plan is one of the plans; 0 for a limit is unlimited. Projects, Members and StorageBytes are per workspace.
// AIUSD is the AI testing spend a month across every workspace of the account, and 0 is none: AI calls cost money,
// so no plan has them unlimited.
type Plan struct {
	ID           string
	Name         string
	Workspaces   int
	Projects     int
	Members      int
	StorageBytes int64
	AIUSD        float64
}

// Free is the plan of an account with no subscription in force.
const Free = "free"

// Plans is every plan in display order with its default limits. The paid ones match the taskwell_plan in the
// custom_data of their Paddle prices. The limits in force are system_config's (see Load), read with Get.
var Plans = []Plan{
	{ID: Free, Name: "Free", Workspaces: 1, Projects: 1, Members: 1, StorageBytes: 2 << 30},
	{ID: "starter", Name: "Starter", Workspaces: 1, Projects: 5, Members: 10, StorageBytes: 20 << 30, AIUSD: 1},
	{ID: "pro", Name: "Pro", Workspaces: 3, Members: 25, StorageBytes: 100 << 30, AIUSD: 3},
	{ID: "advanced", Name: "Advanced", StorageBytes: 500 << 30, AIUSD: 15},
}

// loaded is Plans with the limits an admin set; nil until Load.
var loaded atomic.Pointer[[]Plan]

// beta is system_config's beta_mode as of the last Load.
var beta atomic.Bool

// Beta reports whether beta mode is on (docs/beta-mode-plan.md): limit messages don't mention upgrading.
func Beta() bool { return beta.Load() }

// Msg is paid, or betaMsg while beta mode is on.
func Msg(paid, betaMsg string) string {
	if Beta() {
		return betaMsg
	}
	return paid
}

// All returns every plan in display order with the limits in force.
func All() []Plan {
	if p := loaded.Load(); p != nil {
		return *p
	}
	return Plans
}

// Get returns the plan with the id.
func Get(id string) (Plan, bool) {
	for _, p := range All() {
		if p.ID == id {
			return p, true
		}
	}
	return Plan{}, false
}

// Rows is a pool: Load's query.
type Rows interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Load reads the limits from system_config, so the admin app's Config page can change them: plan_<id>_workspaces,
// _projects, _members, _storage_gb and _ai_usd, each a number from 0 (0 is unlimited; for _ai_usd, none). A missing or
// broken value keeps the plan's default. It also reads beta_mode for Beta; a missing row is off.
func Load(ctx context.Context, db Rows) error {
	rows, err := db.Query(ctx, `select key, value from system_config where key like 'plan\_%' or key = 'beta_mode'`)
	if err != nil {
		return fmt.Errorf("load plan limits: %w", err)
	}
	vals, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) ([2]string, error) {
		var kv [2]string
		return kv, r.Scan(&kv[0], &kv[1])
	})
	if err != nil {
		return fmt.Errorf("load plan limits: %w", err)
	}
	next := slices.Clone(Plans)
	betaOn := false
	for _, kv := range vals {
		if kv[0] == "beta_mode" {
			betaOn = kv[1] == "true"
			continue
		}
		n, err := strconv.ParseFloat(kv[1], 64)
		if err != nil || n < 0 {
			slog.WarnContext(ctx, "plan limit isn't a number from 0, keeping the default", "key", kv[0], "value", kv[1])
			continue
		}
		for i := range next {
			limit, ok := strings.CutPrefix(kv[0], "plan_"+next[i].ID+"_")
			if !ok {
				continue
			}
			switch limit {
			case "workspaces":
				next[i].Workspaces = int(n)
			case "projects":
				next[i].Projects = int(n)
			case "members":
				next[i].Members = int(n)
			case "storage_gb":
				next[i].StorageBytes = int64(n) << 30
			case "ai_usd":
				next[i].AIUSD = n
			}
		}
	}
	loaded.Store(&next)
	beta.Store(betaOn)
	return nil
}

// Watch loads the limits now and again every interval until ctx is done, so an admin's change applies without a
// restart. A failed load keeps the limits it had.
func Watch(ctx context.Context, db Rows, every time.Duration) {
	for {
		if err := Load(ctx, db); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "load plan limits", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// Querier is a pool or a transaction.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func get(id string, err error) (Plan, error) {
	if err != nil {
		return Plan{}, fmt.Errorf("read plan: %w", err)
	}
	p, ok := Get(id)
	if !ok {
		return Plan{}, fmt.Errorf("read plan: unknown plan %q", id)
	}
	return p, nil
}

// ForUser returns the account's plan in force.
func ForUser(ctx context.Context, q Querier, userID string) (Plan, error) {
	var id string
	err := q.QueryRow(ctx, `select plan from users where id = $1`, userID).Scan(&id)
	return get(id, err)
}

// ForWorkspace returns the plan of the workspace's owner (its creator), Free when it has none.
func ForWorkspace(ctx context.Context, q Querier, workspaceID string) (Plan, error) {
	var id string
	err := q.QueryRow(ctx, `select coalesce(u.plan, 'free') from workspaces w left join users u on u.id = w.created_by
		where w.id = $1`, workspaceID).Scan(&id)
	return get(id, err)
}

// SQL is ForWorkspace as an SQL expression on the workspace id column col, for queries that pick workspaces by plan.
func SQL(col string) string {
	return `coalesce((select pu.plan from workspaces pw join users pu on pu.id = pw.created_by where pw.id = ` + col + `), 'free')`
}

// GB formats a storage limit for messages: "2 GB".
func GB(bytes int64) string { return fmt.Sprintf("%d GB", bytes>>30) }

// Count is "1 project" or "2 projects".
func Count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// Exceeded is a limit the plan doesn't allow going past; Error is the user-facing message (a 402).
type Exceeded string

func (e Exceeded) Error() string { return string(e) }
