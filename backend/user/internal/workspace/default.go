package workspace

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/devender07/taskwell/backend/user/internal/doc"
	"github.com/devender07/taskwell/backend/user/internal/realtime"
)

// CreateFirst gives a new user their first workspace, named workspaceName (the company or team they gave at sign-up)
// or else "Ada's workspace", at a URL made from that name, adding a random suffix when it's taken. Called by auth
// inside the transaction that creates or verifies the account. Someone with a pending invite joins the workspaces
// they were invited to instead, with none of their own.
func CreateFirst(ctx context.Context, tx pgx.Tx, userID, userName, workspaceName string) error {
	if joined, err := joinInvited(ctx, tx, userID); err != nil || joined {
		return err
	}
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

// joinInvited adds the user to every workspace with a pending invite for their email, which is verified by now; false
// when there's none. The invites stay, so the emailed link still accepts (as a member already) and lands there.
func joinInvited(ctx context.Context, tx pgx.Tx, userID string) (bool, error) {
	rows, err := tx.Query(ctx,
		`insert into workspace_members (workspace_id, user_id, role_id)
		 select i.workspace_id, u.id, coalesce(i.role_id, r.id)
		   from users u
		   join workspace_invites i on i.email = u.email and i.expires_at > now()
		   join workspace_roles r on r.workspace_id = i.workspace_id and r.builtin = 'member'
		  where u.id = $1
		 on conflict do nothing
		 returning workspace_id::text`, userID)
	if err != nil {
		return false, fmt.Errorf("join invited workspaces: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return false, fmt.Errorf("join invited workspaces: %w", err)
	}
	for _, id := range ids {
		ws, err := scanWorkspace(tx.QueryRow(ctx, `select `+wsCols+` from `+wsFrom+` where w.id = $1 and m.user_id = $2`, id, userID))
		if err != nil {
			return false, fmt.Errorf("join invited workspaces: %w", err)
		}
		if err := realtime.Publish(ctx, tx, invitesChanged(&ws, userID)); err != nil {
			return false, err
		}
	}
	if len(ids) > 0 {
		if err := realtime.Publish(ctx, tx, realtime.AdminWorkspaces(userID)); err != nil {
			return false, err
		}
	}
	return len(ids) > 0, nil
}
