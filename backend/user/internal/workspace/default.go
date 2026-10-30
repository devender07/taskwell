package workspace

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateDefault gives a new user their first workspace, "Ada's workspace" at /ada, adding a random
// suffix to the URL when it's taken. Called by auth when an account is created.
func CreateDefault(ctx context.Context, db *pgxpool.Pool, userID, userName string) error {
	first, _, _ := strings.Cut(strings.TrimSpace(userName), " ")
	name := first + "'s workspace"
	if first == "" || jsLen(name) > 50 {
		name = "My workspace"
	}
	base := slugify(first)
	if checkSlug(base) != "" {
		base = "workspace"
	}
	m := &model{db: db}
	slug := base
	for range 5 {
		_, err := m.create(ctx, userID, name, slug)
		if !errors.Is(err, errTaken) {
			return err
		}
		slug = base + "-" + strings.ToLower(rand.Text()[:4])
	}
	return fmt.Errorf("default workspace: no free URL for %q", base)
}

// slugify mirrors the frontend's: lowercase, runs of anything else become one hyphen, trimmed, at most 32 characters.
func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 32 {
		out = strings.TrimRight(out[:32], "-")
	}
	return out
}
