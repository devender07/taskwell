package workspace

import (
	"context"
	"testing"

	"github.com/devender07/tether/backend/user/internal/dbtest"
)

// TestRemoveMemberLeavesProjects: someone removed from the workspace stops being a member of its private projects,
// so rejoining doesn't bring them back.
func TestRemoveMemberLeavesProjects(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia")
	m := &model{db: pool}
	ws, err := m.create(ctx, ann, "Private", dbtest.Name("private-"))
	if err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, ws.ID, mia, "member")
	if _, err := pool.Exec(ctx, `with p as (insert into projects (workspace_id, name, key, private) values ($1, 'Lead', 'LEAD', true) returning id)
		insert into project_members (project_id, user_id) select id, $2 from p`, ws.ID, mia); err != nil {
		t.Fatal(err)
	}
	if removed, err := m.removeMember(ctx, &ws, ann, mia); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from project_members where user_id = $1`, mia).Scan(&n); err != nil || n != 0 {
		t.Errorf("project memberships left: %d %v", n, err)
	}
}
