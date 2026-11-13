package gitlink

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	many := make([]string, 25)
	for i := range many {
		many[i] = fmt.Sprintf("ENG-%d", i+1)
	}
	tests := []struct {
		name, branch, title, body string
		want                      []Ref
	}{
		{"branch closes", "eng-142-fix-login", "", "", []Ref{{"ENG", 142, true}}},
		{"title closes", "main", "Fix login (WEB-7)", "", []Ref{{"WEB", 7, true}}},
		{"body mention only references", "", "", "Refs ENG-2, see also api-3.", []Ref{{"ENG", 2, false}, {"API", 3, false}}},
		{"closing words", "", "", "Fixes ENG-1\ncloses: ENG-2\nResolved ENG-3\nfix eng-4\nclose ENG-5, ENG-6",
			[]Ref{{"ENG", 1, true}, {"ENG", 2, true}, {"ENG", 3, true}, {"ENG", 4, true}, {"ENG", 5, true}, {"ENG", 6, false}}},
		{"not a closing word", "", "", "prefixes ENG-1 and unfixed ENG-2", []Ref{{"ENG", 1, false}, {"ENG", 2, false}}},
		{"case-insensitive, deduplicated, closes wins", "Eng-9-x", "", "see ENG-9", []Ref{{"ENG", 9, true}}},
		{"body closes a title-less ref", "", "ENG-1", "fixes eng-1", []Ref{{"ENG", 1, true}}},
		{"unknown keys still parse; the model ignores them", "", "Bump UTF-8 handling", "", []Ref{{"UTF", 8, true}}},
		{"no match", "feature/login", "Login page", "e-1 x-1 ENG-12345678 1ENG-2 ENG_3", nil},
		{"cap", "", "", strings.Join(many, " "), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.branch, tt.title, tt.body)
			if tt.name == "cap" {
				if len(got) != maxRefs || got[maxRefs-1] != (Ref{"ENG", maxRefs, false}) {
					t.Fatalf("got %d refs, last %v", len(got), got[len(got)-1])
				}
				return
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Parse(%q, %q, %q) = %v, want %v", tt.branch, tt.title, tt.body, got, tt.want)
			}
		})
	}
}

func TestParsePayloads(t *testing.T) {
	gh := `{"action":"closed","pull_request":{"number":12,"html_url":"https://github.com/acme/web/pull/12","title":"T",
		"body":null,"state":"closed","merged":true,"draft":false,"head":{"ref":"eng-1-x"},"user":{"login":"ann"}},
		"repository":{"full_name":"acme/web"}}`
	p, ok, err := parseGitHub("pull_request", []byte(gh))
	if err != nil || !ok || p.State != "merged" || p.Repo != "acme/web" || p.Number != 12 || p.Branch != "eng-1-x" || p.Author != "ann" {
		t.Errorf("github: %+v %v %v", p, ok, err)
	}
	if _, ok, _ := parseGitHub("ping", []byte(`{}`)); ok {
		t.Error("ping handled")
	}
	if _, ok, _ := parseGitHub("pull_request", []byte(`{"action":"labeled"}`)); ok {
		t.Error("labeled handled")
	}
	gl := `{"object_kind":"merge_request","user":{"username":"bob"},"project":{"path_with_namespace":"acme/api"},
		"object_attributes":{"iid":3,"url":"https://gitlab.com/acme/api/-/merge_requests/3","title":"Draft: x",
		"description":"Fixes API-1","source_branch":"b","state":"opened","action":"open","draft":true}}`
	p, ok, err = parseGitLab("Merge Request Hook", []byte(gl))
	if err != nil || !ok || p.State != "draft" || p.Repo != "acme/api" || p.Number != 3 || p.Body != "Fixes API-1" || p.Author != "bob" {
		t.Errorf("gitlab: %+v %v %v", p, ok, err)
	}
	if _, ok, _ := parseGitLab("Push Hook", []byte(gl)); ok {
		t.Error("push handled")
	}
}

func TestValidURL(t *testing.T) {
	for url, want := range map[string]bool{
		"https://github.com/acme/web/pull/1": true, "HTTP://gitlab.example/x/-/merge_requests/2": true,
		"javascript:alert(1)": false, "data:text/html,x": false, "": false, "//evil.example": false,
	} {
		if got := valid(pr{Repo: "acme/web", Number: 1, URL: url}); got != want {
			t.Errorf("valid(%q) = %v, want %v", url, got, want)
		}
	}
}
