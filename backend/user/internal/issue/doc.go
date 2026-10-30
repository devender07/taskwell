package issue

import (
	"encoding/json"
	"fmt"
	"regexp"
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
		"codeBlock", "horizontalRule", "hardBreak", "mention", "image"}
	markTypes   = []string{"bold", "italic", "strike", "underline", "code", "link"}
	hrefSchemes = []string{"http://", "https://", "mailto:"}
	// imageParents are the nodes an image (a block) may sit in.
	imageParents = []string{"doc", "blockquote", "listItem"}
	// fileURL is an upload served by GET /api/files/{id}/{name}.
	fileURL = regexp.MustCompile(`^/api/files/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/[^/?#]+$`)
)

const (
	maxDocDepth       = 32
	maxDescriptionLen = 50000
	maxCommentLen     = 20000
	maxAltLen         = 200
)

// checkDoc validates a rich-text document (a description, or a comment) up to maxLen characters, and returns it
// normalized, or nil when it's empty, plus an error message naming it ("" when valid).
func checkDoc(raw []byte, name string, maxLen int) (json.RawMessage, string) {
	var doc node
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Type != "doc" {
		return nil, name + " is not valid."
	}
	length, solid := 0, false
	var walk func(n node, parent string, depth int) bool
	walk = func(n node, parent string, depth int) bool {
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
		case "image":
			src, _ := n.Attrs["src"].(string)
			alt, altOK := n.Attrs["alt"].(string)
			if !slices.Contains(imageParents, parent) || !fileURL.MatchString(src) ||
				n.Attrs["alt"] != nil && (!altOK || jsLen(alt) > maxAltLen) {
				return false
			}
			solid = true
		}
		length += jsLen(n.Text)
		for _, m := range n.Marks {
			if !slices.Contains(markTypes, m.Type) {
				return false
			}
			if m.Type == "link" {
				href, _ := m.Attrs["href"].(string)
				if !fileURL.MatchString(href) &&
					!slices.ContainsFunc(hrefSchemes, func(s string) bool { return strings.HasPrefix(strings.ToLower(href), s) }) {
					return false
				}
			}
		}
		for _, c := range n.Content {
			if !walk(c, n.Type, depth+1) {
				return false
			}
		}
		return true
	}
	if !walk(doc, "", 1) {
		return nil, name + " has unsupported content."
	}
	if length > maxLen {
		return nil, fmt.Sprintf("%s must be %d,%03d characters or fewer.", name, maxLen/1000, maxLen%1000)
	}
	if length == 0 && !solid {
		return nil, ""
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, name + " is not valid."
	}
	return out, ""
}

func checkDescription(raw []byte) (json.RawMessage, string) {
	return checkDoc(raw, "Description", maxDescriptionLen)
}

// checkComment is checkDoc for a comment, which can't be empty.
func checkComment(raw string) (json.RawMessage, string) {
	if raw == "" {
		return nil, "Write a comment."
	}
	doc, msg := checkDoc([]byte(raw), "Comment", maxCommentLen)
	if doc == nil && msg == "" {
		msg = "Write a comment."
	}
	return doc, msg
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

// plainText returns a document's searchable text: its text and mention labels, with a newline after each block and
// hard break. A nil or invalid doc has none.
func plainText(raw []byte) string {
	var doc node
	if json.Unmarshal(raw, &doc) != nil {
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
	walk(doc)
	return strings.TrimSpace(b.String())
}
