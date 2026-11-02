package workspace

import (
	"slices"
	"strings"
	"testing"

	"github.com/devender07/tether/backend/user/internal/web"
)

func TestValidation(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{checkName(""), "Workspace name is required."},
		{checkName(strings.Repeat("é", 51)), "Workspace name must be 50 characters or fewer."},
		{checkName(strings.Repeat("é", 50)), ""},
		{checkSlug(""), "Workspace URL is required."},
		{checkSlug("ab"), "Workspace URL must be 3 to 32 characters."},
		{checkSlug(strings.Repeat("a", 33)), "Workspace URL must be 3 to 32 characters."},
		{checkSlug(strings.Repeat("a", 32)), ""},
		{checkSlug("-acme"), "Use lowercase letters, numbers and single hyphens."},
		{checkSlug("acme-"), "Use lowercase letters, numbers and single hyphens."},
		{checkSlug("ac--me"), "Use lowercase letters, numbers and single hyphens."},
		{checkSlug("Acme"), "Use lowercase letters, numbers and single hyphens."},
		{checkSlug("ac_me"), "Use lowercase letters, numbers and single hyphens."},
		{checkSlug("new-workspace"), "That URL is reserved."},
		{checkSlug("api"), "That URL is reserved."},
		{checkSlug("acme-2"), ""},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
	if normSlug(" Acme ") != "acme" {
		t.Error("normSlug does not lowercase and trim")
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Ada":                            "ada",
		"  Jean-Luc Picard ":             "jean-luc-picard",
		"O'Brien":                        "o-brien",
		"__":                             "",
		strings.Repeat("ab", 20):         strings.Repeat("ab", 16),
		"a b " + strings.Repeat("c", 40): "a-b-" + strings.Repeat("c", 28),
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEmail(t *testing.T) {
	for in, want := range map[string]string{
		"":                                 "Email is required.",
		"  ":                               "Email is required.",
		"bob":                              "Enter a valid email address.",
		"bob@acme":                         "Enter a valid email address.",
		"b ob@acme.com":                    "Enter a valid email address.",
		strings.Repeat("a", 250) + "@a.co": "Enter a valid email address.",
		" Bob@Acme.com ":                   "",
	} {
		if got := web.CheckEmail(in); got != want {
			t.Errorf("web.CheckEmail(%q) = %q, want %q", in, got, want)
		}
	}
	if web.NormEmail(" Bob@Acme.COM ") != "bob@acme.com" {
		t.Error("normEmail does not lowercase and trim")
	}
}

func TestPermissions(t *testing.T) {
	if got := effective(roleAdmin, nil); !slices.Equal(got, allPermissions()) || len(got) != len(catalog) {
		t.Errorf("admin effective = %v, want the whole catalog", got)
	}
	if got := effective("member", []string{"issues.create"}); !slices.Equal(got, []string{"issues.create"}) {
		t.Errorf("member effective = %v", got)
	}
	member := []string{"issues.create", "comments.create"}
	if !subset(nil, member) || !subset([]string{"comments.create"}, member) {
		t.Error("subset rejects a subset")
	}
	if subset([]string{"issues.create", permRolesManage}, member) {
		t.Error("subset allows escalation")
	}
	lead := []string{permRolesManage, PermIssuesEditAny}
	if subset(lead, []string{permRolesManage}) {
		t.Error("a roles manager can reach up to a role with more permissions")
	}
	if !subset(member, allPermissions()) {
		t.Error("admin can't grant everything")
	}
	if got, ok := normPerms([]string{"comments.create", permWorkspaceEdit, "comments.create"}); !ok ||
		!slices.Equal(got, []string{permWorkspaceEdit, "comments.create"}) {
		t.Errorf("normPerms = %v, %v", got, ok)
	}
	if _, ok := normPerms([]string{"nope"}); ok {
		t.Error("normPerms accepts an unknown key")
	}
	for in, want := range map[string]string{
		"":                      "Role name is required.",
		strings.Repeat("é", 40): "",
		strings.Repeat("é", 41): "Role name must be 40 characters or fewer.",
	} {
		if got := checkRoleName(in); got != want {
			t.Errorf("checkRoleName(%q) = %q, want %q", in, got, want)
		}
	}
}
