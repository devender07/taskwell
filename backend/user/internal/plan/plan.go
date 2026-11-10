// Package plan is the billing plans and their limits (docs/billing-plan.md). A plan belongs to an account; a
// workspace has the limits of its creator's plan. It's a leaf so the workspace, project and file packages can check
// limits without importing billing, which imports workspace.
package plan

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Plan is one of the plans; 0 for a limit is unlimited. Projects, Members and StorageBytes are per workspace.
type Plan struct {
	ID           string
	Name         string
	Workspaces   int
	Projects     int
	Members      int
	StorageBytes int64
}

// Free is the plan of an account with no subscription in force.
const Free = "free"

// Plans is every plan in display order. The paid ones match the taskwell_plan in the custom_data of their Paddle prices.
var Plans = []Plan{
	{ID: Free, Name: "Free", Workspaces: 1, Projects: 1, Members: 1, StorageBytes: 2 << 30},
	{ID: "starter", Name: "Starter", Workspaces: 1, Projects: 5, Members: 10, StorageBytes: 20 << 30},
	{ID: "pro", Name: "Pro", Workspaces: 3, Members: 25, StorageBytes: 100 << 30},
	{ID: "advanced", Name: "Advanced"},
}

// Get returns the plan with the id.
func Get(id string) (Plan, bool) {
	for _, p := range Plans {
		if p.ID == id {
			return p, true
		}
	}
	return Plan{}, false
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
