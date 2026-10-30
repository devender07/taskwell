package issue

import (
	"slices"
	"strings"
	"testing"
)

func TestCheckDoc(t *testing.T) {
	para := func(inner string) string {
		return `{"type":"doc","content":[{"type":"paragraph","content":[` + inner + `]}]}`
	}
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
		{"max length", para(`{"type":"text","text":"` + strings.Repeat("é", 50000) + `"}`), "", false},
	}
	for _, c := range cases {
		out, msg := checkDescription([]byte(c.doc))
		if msg != c.wantErr || (out == nil) != c.wantNil {
			t.Errorf("%s: got %q nil=%v, want %q nil=%v", c.name, msg, out == nil, c.wantErr, c.wantNil)
		}
	}
	comments := []struct{ doc, want string }{
		{"", "Write a comment."},
		{`{"type":"doc","content":[{"type":"paragraph"}]}`, "Write a comment."},
		{`{"type":`, "Comment is not valid."},
		{para(`{"type":"image"}`), "Comment has unsupported content."},
		{para(`{"type":"text","text":"` + strings.Repeat("a", 20001) + `"}`), "Comment must be 20,000 characters or fewer."},
		{para(`{"type":"text","text":"` + strings.Repeat("a", 20000) + `"}`), ""},
	}
	for _, c := range comments {
		if out, msg := checkComment(c.doc); msg != c.want || (out == nil) != (c.want != "") {
			t.Errorf("comment %.40q: got %q nil=%v, want %q", c.doc, msg, out == nil, c.want)
		}
	}
	got := mentions([]byte(para(`{"type":"mention","attrs":{"id":"a"}},{"type":"mention","attrs":{"id":"b"}},{"type":"mention","attrs":{"id":"a"}}`)))
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("mentions: got %v", got)
	}
}
