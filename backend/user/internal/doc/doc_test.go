package doc

import (
	"slices"
	"strings"
	"testing"
)

func TestCheckDoc(t *testing.T) {
	para := func(inner string) string {
		return `{"type":"doc","content":[{"type":"paragraph","content":[` + inner + `]}]}`
	}
	file := "/api/files/0192f0c4-7b1a-7c3e-8d2f-1a2b3c4d5e6f/screen%20shot.png"
	nested := `{"type":"paragraph"}`
	for range 32 {
		nested = `{"type":"blockquote","content":[` + nested + `]}`
	}
	cases := []struct {
		name, doc, wantErr string
		wantNil            bool
	}{
		{"valid", para(`{"type":"text","text":"Hi ","marks":[{"type":"bold"},{"type":"link","attrs":{"href":"HTTPS://x.dev"}}]},
			{"type":"mention","attrs":{"id":"u1","label":"Maya"}}`), "", false},
		{"heading", `{"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"T"}]}]}`, "", false},
		{"empty", `{"type":"doc","content":[{"type":"paragraph"},{"type":"paragraph"}]}`, "", true},
		{"rule only", `{"type":"doc","content":[{"type":"horizontalRule"}]}`, "", false},
		{"bad json", `{"type":`, "Description is not valid.", true},
		{"root not doc", `{"type":"paragraph"}`, "Description is not valid.", true},
		{"unknown node", para(`{"type":"image","attrs":{"src":"x"}}`), "Description has unsupported content.", true},
		{"unknown mark", para(`{"type":"text","text":"x","marks":[{"type":"highlight"}]}`), "Description has unsupported content.", true},
		{"javascript link", para(`{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"javascript:alert(1)"}}]}`), "Description has unsupported content.", true},
		{"heading 4", `{"type":"doc","content":[{"type":"heading","attrs":{"level":4}}]}`, "Description has unsupported content.", true},
		{"too deep", `{"type":"doc","content":[` + nested + `]}`, "Description has unsupported content.", true},
		{"too long", para(`{"type":"text","text":"` + strings.Repeat("é", 49999) + `"},{"type":"mention","attrs":{"id":"u","label":"ab"}}`), "Description must be 50,000 characters or fewer.", true},
		{"image", `{"type":"doc","content":[{"type":"image","attrs":{"src":"` + file + `","alt":"Shot"}}]}`, "", false},
		{"image in list", `{"type":"doc","content":[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"image","attrs":{"src":"` + file + `"}}]}]}]}`, "", false},
		{"image external src", `{"type":"doc","content":[{"type":"image","attrs":{"src":"https://evil.dev/x.png"}}]}`, "Description has unsupported content.", true},
		{"image traversal src", `{"type":"doc","content":[{"type":"image","attrs":{"src":"/api/files/../x"}}]}`, "Description has unsupported content.", true},
		{"image long alt", `{"type":"doc","content":[{"type":"image","attrs":{"src":"` + file + `","alt":"` + strings.Repeat("a", 201) + `"}}]}`, "Description has unsupported content.", true},
		{"image inline", para(`{"type":"image","attrs":{"src":"` + file + `"}}`), "Description has unsupported content.", true},
		{"file link", para(`{"type":"text","text":"notes.txt","marks":[{"type":"link","attrs":{"href":"` + file + `"}}]}`), "", false},
		{"file link traversal", para(`{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"/api/files/../x"}}]}`), "Description has unsupported content.", true},
		{"file link query", para(`{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"` + file + `?x=1"}}]}`), "Description has unsupported content.", true},
		{"max length", para(`{"type":"text","text":"` + strings.Repeat("é", 50000) + `"}`), "", false},
	}
	for _, c := range cases {
		out, msg := Check([]byte(c.doc), "Description", 50000)
		if msg != c.wantErr || (out == nil) != c.wantNil {
			t.Errorf("%s: got %q nil=%v, want %q nil=%v", c.name, msg, out == nil, c.wantErr, c.wantNil)
		}
	}
	issue := `{"type":"mention","attrs":{"id":"ENG-12","label":"ENG-12","mentionSuggestionChar":"#"}}`
	if out, msg := Check([]byte(para(issue)), "Message", 10000); out == nil || msg != "" {
		t.Errorf("issue mention: got %q nil=%v", msg, out == nil)
	}
	got := Mentions([]byte(para(`{"type":"mention","attrs":{"id":"a"}},{"type":"mention","attrs":{"id":"b","mentionSuggestionChar":"@"}},` +
		issue + `,{"type":"mention","attrs":{"id":"a"}}`)))
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("mentions: got %v", got)
	}
	texts := []struct{ name, doc, want string }{
		{"text, marks and mention", para(`{"type":"text","text":"Fix "},{"type":"text","text":"login","marks":[{"type":"bold"}]},
			{"type":"text","text":" for "},{"type":"mention","attrs":{"id":"u1","label":"Maya"}}`), "Fix login for Maya"},
		{"blocks and break", `{"type":"doc","content":[{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Steps"}]},
			{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"},
			{"type":"hardBreak"},{"type":"text","text":"b"}]}]}]},{"type":"image","attrs":{"src":"` + file + `"}}]}`, "Steps\na\nb"},
		{"nil", ``, ""},
	}
	for _, c := range texts {
		if got := PlainText([]byte(c.doc)); got != c.want {
			t.Errorf("PlainText %s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestKeepMentions(t *testing.T) {
	in := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"a","label":"Ann"}},` +
		`{"type":"mention","attrs":{"id":"b","label":"Bob","mentionSuggestionChar":"@"}},` +
		`{"type":"mention","attrs":{"id":"ENG-1","label":"ENG-1","mentionSuggestionChar":"#"}}]}]}`
	got := string(KeepMentions([]byte(in), func(id string) bool { return id == "a" }))
	want := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"a","label":"Ann"}},` +
		`{"type":"text","text":"@Bob"},{"type":"mention","attrs":{"id":"ENG-1","label":"ENG-1","mentionSuggestionChar":"#"}}]}]}`
	if got != want {
		t.Errorf("got %s", got)
	}
	if got := string(KeepMentions([]byte(in), func(string) bool { return true })); got != in {
		t.Errorf("unchanged doc rewritten: %s", got)
	}
}
