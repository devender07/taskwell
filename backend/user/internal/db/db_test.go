package db

import (
	"slices"
	"testing"
)

func TestStatements(t *testing.T) {
	sql := "-- comment;\ndrop index concurrently if exists a;\ncreate index concurrently a\n  on t (x);\n\ncreate index b on t (y)\n"
	want := []string{"drop index concurrently if exists a;", "create index concurrently a\n  on t (x);", "create index b on t (y)"}
	if got := statements(sql); !slices.Equal(got, want) {
		t.Fatalf("statements = %q", got)
	}
}
