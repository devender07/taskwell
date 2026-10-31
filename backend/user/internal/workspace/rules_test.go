package workspace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/devender07/tether/backend/user/internal/dbtest"
)

// TestRoleAndMemberRules runs the roles and members endpoints against the scratch database (see dbtest).
func TestRoleAndMemberRules(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()
	ann, bob, cat := dbtest.User(t, pool, "Ann"), dbtest.User(t, pool, "Bob"), dbtest.User(t, pool, "Cat")
	ws, err := (&model{db: pool}).create(ctx, ann, "Rules", dbtest.Name("rules-"))
	if err != nil {
		t.Fatal(err)
	}
	dbtest.Join(t, pool, ws.ID, bob, "member")
	dbtest.Join(t, pool, ws.ID, cat, "member")
	mux := http.NewServeMux()
	New(pool, dbtest.Auth, nil, nil).Register(mux)
	base := "/api/workspaces/" + ws.Slug
	do := func(user, method, path string, form url.Values) (int, string) {
		t.Helper()
		return dbtest.Do(t, mux, user, method, base+path, form)
	}
	role := func(user, name string, perms ...string) string {
		t.Helper()
		code, body := do(user, "POST", "/roles", url.Values{"name": {name}, "permissions": perms})
		if code != http.StatusCreated {
			t.Fatalf("create role %s: %d %s", name, code, body)
		}
		var r Role
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatal(err)
		}
		return r.ID
	}
	var adminRole, memberRole string
	if err := pool.QueryRow(ctx, `select (select id::text from workspace_roles where workspace_id = $1 and builtin = 'admin'),
		(select id::text from workspace_roles where workspace_id = $1 and builtin = 'member')`, ws.ID).Scan(&adminRole, &memberRole); err != nil {
		t.Fatal(err)
	}
	lead := role(ann, "Lead", append([]string{permRolesManage, permMembersManage, PermIssuesEditAny}, memberDefaults...)...)
	manager := role(ann, "Manager", append([]string{permRolesManage, permMembersManage}, memberDefaults...)...)
	if code, _ := do(ann, "POST", "/members/"+cat, url.Values{"role_id": {manager}}); code != http.StatusOK {
		t.Fatalf("make cat a manager: %d", code)
	}

	steps := []struct {
		name         string
		user, method string
		path         string
		form         url.Values
		want         int
	}{
		{"a member can't change roles", bob, "POST", "/members/" + cat, url.Values{"role_id": {memberRole}}, http.StatusForbidden},
		{"the last admin can't step down", ann, "POST", "/members/" + ann, url.Values{"role_id": {memberRole}}, http.StatusConflict},
		{"the last admin can't leave", ann, "DELETE", "/members/" + ann, nil, http.StatusConflict},
		{"a manager can't change the Admin role", cat, "POST", "/roles/" + adminRole, url.Values{"name": {"Admin"}}, http.StatusForbidden},
		{"a manager can't weaken a role above theirs", cat, "POST", "/roles/" + lead, url.Values{"name": {"Lead"}}, http.StatusForbidden},
		{"a manager can't delete a role above theirs", cat, "DELETE", "/roles/" + lead, nil, http.StatusForbidden},
		{"a manager can't grant what they lack", cat, "POST", "/roles", url.Values{"name": {"Big"}, "permissions": {PermIssuesEditAny}}, http.StatusForbidden},
		{"a manager can't make someone admin", cat, "POST", "/members/" + bob, url.Values{"role_id": {adminRole}}, http.StatusForbidden},
		{"a manager can't remove an admin", cat, "DELETE", "/members/" + ann, nil, http.StatusForbidden},
		{"a manager can promote a member", cat, "POST", "/members/" + bob, url.Values{"role_id": {manager}}, http.StatusOK},
		{"built-in roles can't be deleted", ann, "DELETE", "/roles/" + memberRole, nil, http.StatusForbidden},
		{"a role name is unique", ann, "POST", "/roles", url.Values{"name": {"lead"}}, http.StatusConflict},
		{"an admin deletes a custom role", ann, "DELETE", "/roles/" + manager, nil, http.StatusNoContent},
		{"its holders became members", cat, "POST", "/members/" + bob, url.Values{"role_id": {memberRole}}, http.StatusForbidden},
		{"a member can leave", bob, "DELETE", "/members/" + bob, nil, http.StatusNoContent},
		{"a non-member gets 404", bob, "GET", "/members", nil, http.StatusNotFound},
	}
	for _, s := range steps {
		if code, body := do(s.user, s.method, s.path, s.form); code != s.want {
			t.Errorf("%s: got %d %s, want %d", s.name, code, body, s.want)
		}
	}
	var role2 string
	if err := pool.QueryRow(ctx, `select role_id::text from workspace_members where workspace_id = $1 and user_id = $2`, ws.ID, cat).
		Scan(&role2); err != nil || role2 != memberRole {
		t.Errorf("cat's role after deleting Manager: %q %v, want Member", role2, err)
	}
}
