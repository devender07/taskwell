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

// forget deletes the instance's viewer and connection rows, when it shuts down, and tells every instance.
func (m *model) forget(ctx context.Context, instance string) error {
	if _, err := m.db.Exec(ctx, `with d as (delete from issue_viewers where instance = $1 returning issue_id) `+changed, instance); err != nil {
		return fmt.Errorf("delete instance viewers: %w", err)
	}
	if _, err := m.db.Exec(ctx, `delete from realtime_conns where instance = $1`, instance); err != nil {
		return fmt.Errorf("delete instance connections: %w", err)
	}
	return nil
}

// refresh keeps the instance's viewer and connection rows fresh, and deletes every instance's stale viewers (a
// crashed instance's), telling every instance about those. Stale connection rows just stop counting; the sweep
// deletes them.
func (m *model) refresh(ctx context.Context, instance string) error {
	if _, err := m.db.Exec(ctx, `update issue_viewers set seen_at = now() where instance = $1`, instance); err != nil {
		return fmt.Errorf("refresh viewers: %w", err)
	}
	if _, err := m.db.Exec(ctx, `update realtime_conns set seen_at = now() where instance = $1`, instance); err != nil {
		return fmt.Errorf("refresh connections: %w", err)
	}
	_, err := m.db.Exec(ctx, `with d as (delete from issue_viewers v where not (`+fresh+`) returning issue_id) `+changed)
	if err != nil {
		return fmt.Errorf("delete stale viewers: %w", err)
	}
	return nil
}

// --- connection cap ---

// claim records the connection when the user has fewer than limit live ones on any instance, and reports whether it
// did. A per-user advisory lock makes the count and insert atomic across instances.
func (m *model) claim(ctx context.Context, connID, userID, instance string, limit int) (bool, error) {
	var ok bool
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('realtime_conns:' || $1, 0))`, userID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			insert into realtime_conns (conn_id, user_id, instance)
			select $1, $2, $3 where (select count(*) from realtime_conns
			                         where user_id = $2 and seen_at > now() - interval '90 seconds') < $4`,
			connID, userID, instance, limit)
		ok = tag.RowsAffected() == 1
		return err
	})
	if err != nil {
		return false, fmt.Errorf("claim connection: %w", err)
	}
	return ok, nil
}

// release deletes the connection's row.
func (m *model) release(ctx context.Context, connID string) error {
	if _, err := m.db.Exec(ctx, `delete from realtime_conns where conn_id = $1`, connID); err != nil {
		return fmt.Errorf("release connection: %w", err)
	}
	return nil
}
