package realtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// model owns every query for presence and the realtime fan-out.
type model struct{ db *pgxpool.Pool }

type membership struct{ id, slug string }

// memberships returns the workspaces the user is in.
func memberships(ctx context.Context, tx pgx.Tx, userID string) ([]membership, error) {
	rows, err := tx.Query(ctx,
		`select w.id::text, w.slug from workspace_members m join workspaces w on w.id = m.workspace_id where m.user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	ms, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (membership, error) {
		var m membership
		err := row.Scan(&m.id, &m.slug)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	return ms, nil
}

// --- presence ---

// fresh is a viewer row its instance still refreshes.
const fresh = `v.seen_at > now() - interval '90 seconds'`

// changed notifies every instance that the viewers of the issue ids in d (a CTE) changed.
const changed = `select pg_notify('realtime', json_build_object('aud', 'issue:' || issue_id)::text) from (select distinct issue_id from d) x`

// viewers returns the users with the issue open, once each, by name.
func (m *model) viewers(ctx context.Context, issueID string) (json.RawMessage, error) {
	var vs json.RawMessage
	err := m.db.QueryRow(ctx,
		`select coalesce(json_agg(json_build_object('id', u.id, 'name', u.name) order by lower(u.name), u.id), '[]')
		 from users u where u.id in (select v.user_id from issue_viewers v where v.issue_id = $1 and `+fresh+`)`, issueID).Scan(&vs)
	if err != nil {
		return nil, fmt.Errorf("list viewers: %w", err)
	}
	return vs, nil
}

// view records that the connection has the issue open and tells every instance.
func (m *model) view(ctx context.Context, connID, issueID, userID, instance string) error {
	_, err := m.db.Exec(ctx,
		`with d as (insert into issue_viewers (conn_id, issue_id, user_id, instance) values ($1, $2, $3, $4)
		   on conflict (conn_id, issue_id) do update set seen_at = now() returning issue_id) `+changed,
		connID, issueID, userID, instance)
	if err != nil {
		return fmt.Errorf("add viewer: %w", err)
	}
	return nil
}

// unview removes the connection's viewer rows, for the issue ids or all of them when ids is nil, and tells every instance.
func (m *model) unview(ctx context.Context, connID string, ids []string) error {
	_, err := m.db.Exec(ctx,
		`with d as (delete from issue_viewers where conn_id = $1 and ($2::uuid[] is null or issue_id = any($2))
		   returning issue_id) `+changed, connID, ids)
	if err != nil {
		return fmt.Errorf("remove viewers: %w", err)
	}
	return nil
}

// refresh keeps the instance's viewer rows fresh, and deletes every instance's stale ones (a crashed instance's),
// telling every instance about those.
func (m *model) refresh(ctx context.Context, instance string) error {
	if _, err := m.db.Exec(ctx, `update issue_viewers set seen_at = now() where instance = $1`, instance); err != nil {
		return fmt.Errorf("refresh viewers: %w", err)
	}
	_, err := m.db.Exec(ctx, `with d as (delete from issue_viewers v where not (`+fresh+`) returning issue_id) `+changed)
	if err != nil {
		return fmt.Errorf("delete stale viewers: %w", err)
	}
	return nil
}
