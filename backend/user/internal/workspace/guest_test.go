package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/devender07/taskwell/backend/user/internal/dbtest"
	"github.com/devender07/taskwell/backend/user/internal/secret"
)

// TestGuests: a new workspace has a Guest role, which can't be given workspace permissions; a guest invite needs
// projects and accepting it adds the guest to them; a guest sees admins and who shares a project with them, and is
// flagged on the workspace and member JSON.
func TestGuests(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, mia, otto, gus := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Mia"), dbtest.User(t, pool, "Otto"), dbtest.User(t, pool, "Gus")
	m := &model{db: pool}
	ws, err := m.create(ctx, ann, "Guests", dbtest.Name("guests-"))
	if err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, ws.ID, mia, "member")
	dbtest.Join(t, pool, ws.ID, otto, "member")
	var pub, priv string
	if err := pool.QueryRow(ctx, `with p as (insert into projects (workspace_id, name, key, private)
		values ($1, 'Web', 'WEB', false), ($1, 'Lead', 'LEAD', true) returning id, key)
		select (select id::text from p where key = 'WEB'), (select id::text from p where key = 'LEAD')`, ws.ID).Scan(&pub, &priv); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into project_members (project_id, user_id) values ($1, $2)`, priv, otto); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, nil, nil).Register(mux)
	base := "/api/workspaces/" + ws.Slug
	do := func(user, method, path string, form url.Values, want int) string {
		t.Helper()
		code, body := dbtest.Do(t, mux, user, method, base+path, form)
		if code != want {
			t.Fatalf("%s %s %v: %d %s, want %d", method, path, form, code, body, want)
		}
		return body
	}

	var roles []Role
	if err := json.Unmarshal([]byte(do(ann, "GET", "/roles", nil, 200)), &roles); err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(roles, func(r Role) bool { return r.Builtin != nil && *r.Builtin == roleGuest })
	if i < 0 || !slices.Equal(roles[i].Permissions, memberDefaults) {
		t.Fatalf("roles %+v", roles)
	}
	guestRole := roles[i].ID
	do(ann, "POST", "/roles/"+guestRole, url.Values{"permissions": {"issues.create", "members.invite"}}, 400)
	do(ann, "POST", "/roles/"+guestRole, url.Values{"permissions": {"issues.create", "issues.edit_any"}}, 200)

	if body := do(ann, "POST", "/invites", url.Values{"email": {"g@dbtest.test"}, "role_id": {guestRole}}, 400); !strings.Contains(body, "Choose at least one project.") {
		t.Fatalf("guest invite without projects: %s", body)
	}
	do(ann, "POST", "/invites", url.Values{"email": {"g@dbtest.test"}, "role_id": {guestRole}, "project_keys": {"NOPE"}}, 400)
	ids, err := m.projectIDs(ctx, ws.ID, ann, []string{"web"})
	if err != nil || !slices.Equal(ids, []string{pub}) {
		t.Fatalf("project ids %v %v", ids, err)
	}
	var email string
	if err := pool.QueryRow(ctx, `select email from users where id = $1`, gus).Scan(&email); err != nil {
		t.Fatal(err)
	}
	tok := secret.NewToken()
	if _, err := m.invite(ctx, &ws, email, ann, guestRole, ids, secret.Hash(tok), 7); err != nil {
		t.Fatal(err)
	}
	if got, _, err := m.accept(ctx, gus, secret.Hash(tok)); err != nil || got == nil || !got.Guest {
		t.Fatalf("accept: %+v %v", got, err)
	}
	var in bool
	if err := pool.QueryRow(ctx, `select exists(select 1 from project_members where project_id = $1 and user_id = $2)`, pub, gus).Scan(&in); err != nil || !in {
		t.Fatalf("guest not in WEB: %v %v", in, err)
	}
	if body := do(gus, "GET", "", nil, 200); !strings.Contains(body, `"guest":true`) || strings.Contains(body, "members.invite") {
		t.Errorf("guest's workspace: %s", body)
	}
	do(gus, "GET", "/roles", nil, 403)

	// Gus sees whoever can see WEB, his project: everyone while it's public; then, private, the admin and himself.
	var page struct{ Members []Member }
	members := func() []string {
		t.Helper()
		if err := json.Unmarshal([]byte(do(gus, "GET", "/members", nil, 200)), &page); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, mb := range page.Members {
			names = append(names, strings.SplitN(mb.Email, "-", 2)[0])
			if mb.ID == gus && !mb.Guest {
				t.Error("guest not flagged on the member list")
			}
		}
		slices.Sort(names)
		return names
	}
	if got := members(); !slices.Equal(got, []string{"ann", "gus", "mia", "otto"}) {
		t.Errorf("members of public WEB: %v", got)
	}
	if _, err := pool.Exec(ctx, `update projects set private = true where id = $1`, pub); err != nil {
		t.Fatal(err)
	}
	if got := members(); !slices.Equal(got, []string{"ann", "gus"}) {
		t.Errorf("members of private WEB: %v", got)
	}
	if code, body := dbtest.Do(t, mux, gus, "GET", "/api/workspaces", nil); code != 200 || !strings.Contains(body, `"guest":true`) {
		t.Errorf("workspace list: %d %s", code, body)
	}
}
