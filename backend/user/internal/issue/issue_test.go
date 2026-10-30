package issue

import (
	"strings"
	"testing"
)

func TestValidation(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{checkTitle(""), "Title is required."},
		{checkTitle(strings.Repeat("é", 201)), "Title must be 200 characters or fewer."},
		{checkTitle(strings.Repeat("é", 200)), ""},
		{checkPriority("0"), ""},
		{checkPriority("4"), ""},
		{checkPriority("5"), "Choose a priority."},
		{checkPriority("-1"), "Choose a priority."},
		{checkPriority(""), "Choose a priority."},
		{checkPriority("1.5"), "Choose a priority."},
		{checkDueDate(""), ""},
		{checkDueDate("2026-11-02"), ""},
		{checkDueDate("2026-02-30"), "Enter a valid date."},
		{checkDueDate("2026-1-2"), "Enter a valid date."},
		{checkDueDate("tomorrow"), "Enter a valid date."},
		{checkStatusName(""), "Status name is required."},
		{checkStatusName(strings.Repeat("a", 31)), "Status name must be 30 characters or fewer."},
		{checkStatusName(strings.Repeat("a", 30)), ""},
		{checkCategory("started"), ""},
		{checkCategory("done"), "Choose a category."},
		{checkLabelName(""), "Label name is required."},
		{checkLabelName(strings.Repeat("a", 31)), "Label name must be 30 characters or fewer."},
		{checkColor(normColor(" #AABBCC ")), ""},
		{checkColor("#abc"), "Choose a color."},
		{checkColor("red"), "Choose a color."},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
}

func TestParseIdentifier(t *testing.T) {
	if key, n, ok := parseIdentifier("eng-142"); !ok || key != "ENG" || n != 142 {
		t.Errorf("eng-142: got %q %d %v", key, n, ok)
	}
	for _, s := range []string{"ENG-0", "ENG-01", "E-1", "ENGINE-1", "1EN-1", "ENG", "ENG-1x", "ENG-99999999999"} {
		if _, _, ok := parseIdentifier(s); ok {
			t.Errorf("%s: parsed, want not found", s)
		}
	}
}

func TestEscapeLike(t *testing.T) {
	if got, want := escapeLike(`50%_off\`), `50\%\_off\\`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
