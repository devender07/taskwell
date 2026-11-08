// Package plan is the billing plans and their limits (docs/billing-plan.md). It's a leaf so the workspace and file
// packages can check limits without importing billing, which imports workspace.
package plan

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Plan is one of the three plans; 0 for Members or StorageBytes is unlimited.
type Plan struct {
	ID           string
	Name         string
	Members      int
	StorageBytes int64
}

const Starter = "starter"

// Plans is every plan in display order.
var Plans = []Plan{
	{ID: Starter, Name: "Starter", Members: 5, StorageBytes: 2 << 30},
	{ID: "pro", Name: "Pro", Members: 10, StorageBytes: 100 << 30},
	{ID: "ultimate", Name: "Ultimate"},
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

// Limits returns the plan in force for the workspace.
func Limits(ctx context.Context, q Querier, workspaceID string) (Plan, error) {
	var id string
	if err := q.QueryRow(ctx, `select plan from workspaces where id = $1`, workspaceID).Scan(&id); err != nil {
		return Plan{}, fmt.Errorf("workspace plan: %w", err)
	}
	p, ok := Get(id)
	if !ok {
		return Plan{}, fmt.Errorf("workspace plan: unknown plan %q", id)
	}
	return p, nil
}

// GB formats a storage limit for messages: "2 GB".
func GB(bytes int64) string { return fmt.Sprintf("%d GB", bytes>>30) }
