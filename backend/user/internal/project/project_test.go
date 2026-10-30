package project

import (
	"strings"
	"testing"
)

func TestValidation(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{checkName(""), "Project name is required."},
		{checkName(strings.Repeat("é", 61)), "Project name must be 60 characters or fewer."},
		{checkName(strings.Repeat("é", 60)), ""},
		{checkKey(""), "Project key is required."},
		{checkKey("E"), "Use 2 to 5 letters or numbers, starting with a letter."},
		{checkKey("ENGINE"), "Use 2 to 5 letters or numbers, starting with a letter."},
		{checkKey("1ENG"), "Use 2 to 5 letters or numbers, starting with a letter."},
		{checkKey("EN-G"), "Use 2 to 5 letters or numbers, starting with a letter."},
		{checkKey("eng"), "Use 2 to 5 letters or numbers, starting with a letter."},
		{checkKey("EN"), ""},
		{checkKey("ENG42"), ""},
		{checkDescription(strings.Repeat("a", 10001)), "Description must be 10,000 characters or fewer."},
		{checkDescription(strings.Repeat("a", 10000)), ""},
		{checkDescription(""), ""},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
	if normKey(" eng ") != "ENG" {
		t.Error("normKey does not uppercase and trim")
	}
}
