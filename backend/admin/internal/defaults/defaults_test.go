package defaults

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDefaultsAPI runs the handlers in a transaction that rolls back (the defaults are shared by every test): the
// built-in role and last status refusals, the guest permission rule, name clashes, reordering and a template whose
// label is deleted. Needs TEST_DATABASE_URL migrated by the user service's tests.
func TestDefaultsAPI(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // nothing commits
	if _, err := tx.Exec(ctx, `delete from default_templates; delete from default_labels`); err != nil {
		t.Fatal(err)
	}
	h := &Handler{model: &model{db: tx}}
	h.Register(http.NewServeMux()) // panics on clashing patterns

	do := func(fn func(http.ResponseWriter, *http.Request, string) error, id string, form url.Values, out any) (int, string) {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/defaults", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		if err := fn(w, r, ""); err != nil {
			t.Fatal(err)
		}
		if out != nil && w.Code < 300 {
			if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
		return w.Code, strings.TrimSpace(w.Body.String())
	}
	want := func(what string, code int, body string, wantCode int, wantBody string) {
		t.Helper()
		if code != wantCode || (wantBody != "" && body != wantBody) {
			t.Errorf("%s: %d %s, want %d %s", what, code, body, wantCode, wantBody)
		}
	}

	var d Defaults
	do(h.list, "", nil, &d)
	if len(d.Roles) < 2 || *d.Roles[0].Builtin != "member" || *d.Roles[1].Builtin != "guest" || len(d.Statuses) == 0 ||
		len(d.Permissions) == 0 {
		t.Fatalf("defaults: %+v", d)
	}
	member, guest := d.Roles[0].ID, d.Roles[1].ID

	code, body := do(h.deleteRole, member, nil, nil)
	want("delete Member", code, body, 409, `{"error":"Built-in roles can't be deleted."}`)
	code, body = do(h.saveRole, guest, url.Values{"permission": {"issues.create", "members.invite"}}, nil)
	want("guest inviting", code, body, 400, `{"errors":{"permission":"Guests can't have workspace, member or analytics permissions."}}`)
	code, body = do(h.saveRole, member, url.Values{"permission": {"nope"}}, nil)
	want("unknown permission", code, body, 400, `{"errors":{"permission":"Choose permissions from the list."}}`)
	var role Role
	code, body = do(h.saveRole, member, url.Values{"name": {"Renamed"}, "permission": {"members.invite", "issues.create"}}, &role)
	if want("Member permissions", code, body, 200, ""); role.Name != "Member" || strings.Join(role.Permissions, ",") != "members.invite,issues.create" {
		t.Errorf("Member = %+v", role)
	}
	code, body = do(h.saveRole, "", url.Values{"name": {"admin"}}, nil)
	want("custom Admin", code, body, 400, `{"errors":{"name":"A role with this name already exists."}}`)
	code, body = do(h.saveRole, "", url.Values{"name": {" QA "}, "permission": {"issues.status_any"}}, &role)
	want("create QA", code, body, 201, "")
	code, body = do(h.saveRole, "", url.Values{"name": {"qa"}}, nil)
	want("second QA", code, body, 400, `{"errors":{"name":"A role with this name already exists."}}`)
	code, body = do(h.deleteRole, role.ID, nil, nil)
	want("delete QA", code, body, 204, "")
	code, body = do(h.deleteRole, "not-a-uuid", nil, nil)
	want("delete missing", code, body, 404, `{"error":"Role not found."}`)

	var typ StatusType
	code, body = do(h.saveType, "", url.Values{"name": {"In review"}, "category": {"started"}, "icon": {"eye"}, "color": {"#5E6AD2"}}, &typ)
	want("create type", code, body, 201, "")
	var st Status
	code, body = do(h.saveStatus, "", url.Values{"name": {"Review"}, "category": {"backlog"}, "type_id": {typ.ID}}, &st)
	if want("create typed status", code, body, 201, ""); st.Category != "started" || st.TypeID == nil || st.Position != len(d.Statuses) {
		t.Errorf("status = %+v", st)
	}
	code, body = do(h.saveStatus, "", url.Values{"name": {"Other"}, "type_id": {"nope"}}, nil)
	want("unknown type", code, body, 400, `{"errors":{"type_id":"Choose a type from the list."}}`)
	code, body = do(h.saveType, typ.ID, url.Values{"name": {"In review"}, "category": {"completed"}, "icon": {"eye"}}, nil)
	want("update type", code, body, 200, "")

	var label Label
	code, body = do(h.saveLabel, "", url.Values{"name": {"Bug"}, "color": {"#eb5757"}}, &label)
	want("create label", code, body, 201, "")
	code, body = do(h.saveLabel, "", url.Values{"name": {"bug"}, "color": {"#eb5757"}}, nil)
	want("second Bug", code, body, 400, `{"errors":{"name":"A label with this name already exists."}}`)
	code, body = do(h.saveTemplate, "", url.Values{"name": {"Bug report"}, "label_id": {"00000000-0000-0000-0000-000000000000"}}, nil)
	want("unknown label", code, body, 400, `{"errors":{"label_id":"Choose labels from the list."}}`)
	var tpl Template
	code, body = do(h.saveTemplate, "", url.Values{"name": {"Bug report"}, "priority": {"2"}, "status_id": {st.ID},
		"label_id": {label.ID, label.ID}, "description": {`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Steps"}]}]}`}}, &tpl)
	if want("create template", code, body, 201, ""); tpl.StatusID == nil || *tpl.StatusID != st.ID || len(tpl.LabelIDs) != 1 || !strings.HasPrefix(string(tpl.Description), `{"type":"doc"`) {
		t.Errorf("template = %s", body)
	}
	code, body = do(h.saveTemplate, tpl.ID, url.Values{"name": {"Bug report"}}, &tpl)
	if want("update template", code, body, 200, ""); string(tpl.Description) != "null" || tpl.StatusID != nil || len(tpl.LabelIDs) != 0 {
		t.Errorf("cleared template = %s", body)
	}
	do(h.saveTemplate, tpl.ID, url.Values{"name": {"Bug report"}, "label_id": {label.ID}}, nil)
	do(h.deleteLabel, label.ID, nil, nil)

	do(h.list, "", nil, &d)
	if got := d.Templates[0].LabelIDs; len(got) != 0 {
		t.Errorf("template labels after the label's deletion = %v", got)
	}
	if got := d.Statuses[len(d.Statuses)-1]; got.ID != st.ID || got.Category != "completed" {
		t.Errorf("typed status after the type's change = %+v", got)
	}
	ids := make([]string, len(d.Statuses))
	for i, s := range d.Statuses {
		ids[len(ids)-1-i] = s.ID
	}
	code, body = do(h.reorderStatuses, "", url.Values{"id": ids[1:]}, nil)
	want("reorder missing one", code, body, 400, `{"errors":{"id":"Send every status once."}}`)
	var order []Status
	code, body = do(h.reorderStatuses, "", url.Values{"id": ids}, &order)
	if want("reorder", code, body, 200, ""); len(order) != len(ids) || order[0].ID != ids[0] || order[0].Position != 0 {
		t.Errorf("order = %s", body)
	}

	for _, id := range ids[:len(ids)-1] {
		code, body = do(h.deleteStatus, id, nil, nil)
		want("delete status", code, body, 204, "")
	}
	code, body = do(h.deleteStatus, ids[len(ids)-1], nil, nil)
	want("delete the last status", code, body, 409, `{"error":"Keep at least one status."}`)
}
