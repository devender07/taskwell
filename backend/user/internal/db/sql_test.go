package db

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// uuidAsText finds a column cast to text and compared, which can't use the column's index; compare uuids instead.
var uuidAsText = regexp.MustCompile(`\w::text\s*(=|<>|!=)`)

// pending are packages another change still has to convert.
// ponytail: delete the entry once workspace/model.go compares uuids.
var pending = map[string]bool{"workspace": true}

func TestNoTextComparisons(t *testing.T) {
	files, err := filepath.Glob("../*/model.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no model files: %v", err)
	}
	for _, f := range files {
		if pending[filepath.Base(filepath.Dir(f))] {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range uuidAsText.FindAllIndex(src, -1) {
			t.Errorf("%s: %q compares a column as text", f, src[m[0]:min(len(src), m[1]+10)])
		}
	}
}
