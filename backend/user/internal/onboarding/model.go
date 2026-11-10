package onboarding

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/taskwell/backend/user/internal/workspace"
	"github.com/devender07/taskwell/backend/user/internal/wsaudit"
)

type model struct{ db *pgxpool.Pool }

type state struct{ dismissed, project, invite, issue, github bool }

// state reads whether the checklist is dismissed and which steps the workspace has done.
func (m *model) state(ctx context.Context, workspaceID string) (state, error) {
	var s state
	err := m.db.QueryRow(ctx, `select w.onboarding_dismissed_at is not null,
		exists (select 1 from projects where workspace_id = w.id),
		(select count(*) from workspace_members where workspace_id = w.id) > 1
		  or exists (select 1 from workspace_invites where workspace_id = w.id and expires_at > now()),
		exists (select 1 from issues where workspace_id = w.id),
		exists (select 1 from git_hooks where workspace_id = w.id)
		from workspaces w where w.id = $1`, workspaceID).Scan(&s.dismissed, &s.project, &s.invite, &s.issue, &s.github)
	if err != nil {
		return s, fmt.Errorf("read onboarding: %w", err)
	}
	return s, nil
}

// dismiss sets onboarding_dismissed_at, recording it in the audit log the first time.
func (m *model) dismiss(ctx context.Context, ws *workspace.Workspace, actorID string) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update workspaces set onboarding_dismissed_at = now()
			where id = $1 and onboarding_dismissed_at is null`, ws.ID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return wsaudit.Record(ctx, tx, ws.ID, actorID, "workspace.onboarding_dismissed", ws.Slug, nil)
	})
	if err != nil {
		return fmt.Errorf("dismiss onboarding: %w", err)
	}
	return nil
}
