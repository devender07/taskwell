package workspace

import (
	"strings"
	"testing"
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
