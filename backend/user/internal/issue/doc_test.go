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
		{"bad json", `{"type":`, errDocInvalid, true},
		{"root not doc", `{"type":"paragraph"}`, errDocInvalid, true},
		{"unknown node", para(`{"type":"image","attrs":{"src":"x"}}`), errDocUnsupported, true},
		{"unknown mark", para(`{"type":"text","text":"x","marks":[{"type":"highlight"}]}`), errDocUnsupported, true},
		{"javascript link", para(`{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"javascript:alert(1)"}}]}`), errDocUnsupported, true},
		{"heading 4", `{"type":"doc","content":[{"type":"heading","attrs":{"level":4}}]}`, errDocUnsupported, true},
		{"too deep", `{"type":"doc","content":[` + nested + `]}`, errDocUnsupported, true},
		{"too long", para(`{"type":"text","text":"` + strings.Repeat("é", 49999) + `"},{"type":"mention","attrs":{"id":"u","label":"ab"}}`), errDocTooLong, true},
		{"max length", para(`{"type":"text","text":"` + strings.Repeat("é", 50000) + `"}`), "", false},
	}
	for _, c := range cases {
		out, msg := checkDoc([]byte(c.doc))
		if msg != c.wantErr || (out == nil) != c.wantNil {
			t.Errorf("%s: got %q nil=%v, want %q nil=%v", c.name, msg, out == nil, c.wantErr, c.wantNil)
		}
	}
	got := mentions([]byte(para(`{"type":"mention","attrs":{"id":"a"}},{"type":"mention","attrs":{"id":"b"}},{"type":"mention","attrs":{"id":"a"}}`)))
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("mentions: got %v", got)
	}
}
