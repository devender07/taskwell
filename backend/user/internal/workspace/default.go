package workspace

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/doc"
)

// CreateFirst gives a new user their first workspace, named workspaceName (the company or team they gave at sign-up)
// or else "Ada's workspace", at a URL made from that name, adding a random suffix when it's taken. Called by auth
// inside the transaction that creates or verifies the account. Pending invites stay pending: the app offers them.
func CreateFirst(ctx context.Context, tx pgx.Tx, userID, userName, workspaceName string) error {
	first, _, _ := strings.Cut(strings.TrimSpace(userName), " ")
	name, from := first+"'s workspace", first
	if first == "" || doc.Len(name) > 50 {
		name = "My workspace"
	}
	if s := strings.TrimSpace(workspaceName); s != "" && checkName(s) == "" {
		name, from = s, s
	}
	base := slugify(from)
	if checkSlug(base) != "" {
		base = "workspace"
	}
	slug := base
	for range 5 {
		_, err := create(ctx, tx, userID, name, slug)
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
