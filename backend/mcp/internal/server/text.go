package server

import (
	"encoding/json"
	"regexp"
	"strings"
)

// node is a ProseMirror JSON node as Tiptap stores descriptions and comments.
type node struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []node         `json:"content,omitempty"`
	Text    string         `json:"text,omitempty"`
}

var blankLine = regexp.MustCompile(`\n[ \t]*\n`)

// toDoc turns plain text into a Tiptap document: one paragraph per blank-line-separated block, line breaks kept as
// hard breaks. ponytail: no markdown; parse it if models send formatting that matters.
func toDoc(text string) string {
	d := node{Type: "doc"}
	for _, block := range blankLine.Split(strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n"), -1) {
		p := node{Type: "paragraph"}
		for i, line := range strings.Split(strings.TrimSpace(block), "\n") {
			if i > 0 {
				p.Content = append(p.Content, node{Type: "hardBreak"})
			}
			if line != "" {
				p.Content = append(p.Content, node{Type: "text", Text: line})
			}
		}
		d.Content = append(d.Content, p)
	}
	b, _ := json.Marshal(d) // a node always marshals
	return string(b)
}

// plainText is the document's text and mention labels, a newline after each block and hard break. A port of the
// user service's doc.PlainText.
func plainText(raw json.RawMessage) string {
	var d node
	if json.Unmarshal(raw, &d) != nil {
		return ""
	}
	var b strings.Builder
	var walk func(n node)
	walk = func(n node) {
		b.WriteString(n.Text)
		if label, _ := n.Attrs["label"].(string); n.Type == "mention" {
			b.WriteString(label)
		}
		for _, c := range n.Content {
			walk(c)
		}
		switch n.Type {
		case "paragraph", "heading", "codeBlock", "hardBreak":
			b.WriteString("\n")
		}
	}
	walk(d)
	return strings.TrimSpace(b.String())
}
