package issue

import (
	"encoding/json"
	"slices"
	"strings"
)

// node is a ProseMirror JSON node, as Tiptap's editor.getJSON() returns it. Re-marshalling drops unknown fields.
type node struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []node         `json:"content,omitempty"`
	Marks   []mark         `json:"marks,omitempty"`
	Text    string         `json:"text,omitempty"`
}

type mark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

var (
	nodeTypes = []string{"doc", "paragraph", "text", "heading", "bulletList", "orderedList", "listItem", "blockquote",
		"codeBlock", "horizontalRule", "hardBreak", "mention"}
	markTypes   = []string{"bold", "italic", "strike", "underline", "code", "link"}
	hrefSchemes = []string{"http://", "https://", "mailto:"}
)

const (
	errDocInvalid     = "Description is not valid."
	errDocUnsupported = "Description has unsupported content."
	errDocTooLong     = "Description must be 50,000 characters or fewer."
	maxDocDepth       = 32
	maxDocLen         = 50000
)

// checkDoc validates a description document and returns it normalized, or nil when it's empty, plus an error
// message ("" when valid).
func checkDoc(raw []byte) (json.RawMessage, string) {
	var doc node
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Type != "doc" {
		return nil, errDocInvalid
	}
	length, solid := 0, false
	var walk func(n node, depth int) bool
	walk = func(n node, depth int) bool {
		if depth > maxDocDepth || !slices.Contains(nodeTypes, n.Type) || n.Type == "doc" && depth > 1 {
			return false
		}
		switch n.Type {
		case "heading":
			if l, _ := n.Attrs["level"].(float64); l != 1 && l != 2 && l != 3 {
				return false
			}
		case "mention":
			id, _ := n.Attrs["id"].(string)
			label, _ := n.Attrs["label"].(string)
			if id == "" {
				return false
			}
			length, solid = length+jsLen(label), true
		case "horizontalRule":
			solid = true
		}
		length += jsLen(n.Text)
		for _, m := range n.Marks {
			if !slices.Contains(markTypes, m.Type) {
				return false
			}
			if m.Type == "link" {
				href, _ := m.Attrs["href"].(string)
				if !slices.ContainsFunc(hrefSchemes, func(s string) bool { return strings.HasPrefix(strings.ToLower(href), s) }) {
					return false
				}
			}
		}
		for _, c := range n.Content {
			if !walk(c, depth+1) {
				return false
			}
		}
		return true
	}
	if !walk(doc, 1) {
		return nil, errDocUnsupported
	}
	if length > maxDocLen {
		return nil, errDocTooLong
	}
	if length == 0 && !solid {
		return nil, ""
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, errDocInvalid
	}
	return out, ""
}

// mentions returns the user ids mentioned in a document, each once, in order. A nil or invalid doc has none.
func mentions(raw []byte) []string {
	var doc node
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	var ids []string
	var walk func(n node)
	walk = func(n node) {
		if id, _ := n.Attrs["id"].(string); n.Type == "mention" && id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(doc)
	return ids
}
