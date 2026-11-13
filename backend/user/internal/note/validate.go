package note

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/devender07/taskwell/backend/shared/doc"
	"github.com/devender07/taskwell/backend/user/internal/web"
)

const (
	maxTitle     = 200
	maxBodyChars = 20000
	maxLabels    = 20
	maxLabel     = 30

	errLabels = "Use up to 20 labels of 30 characters or fewer."
)

var colorRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// fields are a create or update form's fields; nil when absent.
type fields struct {
	title, color     *string
	body             *json.RawMessage // set to nil for "no body"
	pinned, archived *bool
	labels           []string // nil when absent
}

// shared reports whether a field everyone on the note sees changes.
func (f *fields) shared() bool { return f.title != nil || f.body != nil || f.color != nil }

// read returns the form's fields, or writes a 400 with field errors and returns ok=false. On create, a note needs a
// title or a body.
func read(w http.ResponseWriter, r *http.Request, create bool) (f fields, ok bool) {
	_ = r.ParseMultipartForm(maxBody)
	form := r.PostForm
	errs := map[string]string{}
	if _, has := form["title"]; has {
		t := strings.TrimSpace(form.Get("title"))
		f.title = &t
		if doc.Len(t) > maxTitle {
			errs["title"] = "Use 200 characters or fewer."
		}
	}
	if _, has := form["body"]; has {
		var b json.RawMessage
		if raw := form.Get("body"); raw != "" {
			b, errs["body"] = doc.Check([]byte(raw), "Note", maxBodyChars)
		}
		f.body = &b
	}
	if _, has := form["color"]; has {
		c := strings.ToLower(strings.TrimSpace(form.Get("color")))
		f.color = &c
		if c != "" && !colorRe.MatchString(c) {
			errs["color"] = "Choose a color from the list."
		}
	}
	if _, has := form["pinned"]; has {
		p := form.Get("pinned") == "1"
		f.pinned = &p
	}
	if _, has := form["archived"]; has {
		a := form.Get("archived") == "1"
		f.archived = &a
	}
	if _, has := form["labels"]; has {
		f.labels, errs["labels"] = checkLabels(form.Get("labels"))
	}
	if create && errs["body"] == "" && (f.title == nil || *f.title == "") && (f.body == nil || *f.body == nil) {
		errs["body"] = "Write something first."
	}
	return f, web.Check(w, errs)
}

// checkLabels parses a JSON array of labels, trimmed and without case-insensitive repeats (the first spelling stays).
func checkLabels(raw string) ([]string, string) {
	var in []string
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, errLabels
	}
	out := []string{}
	for _, l := range in {
		l = strings.TrimSpace(l)
		if l == "" || doc.Len(l) > maxLabel {
			return nil, errLabels
		}
		if !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, l) }) {
			out = append(out, l)
		}
	}
	if len(out) > maxLabels {
		return nil, errLabels
	}
	return out, ""
}
