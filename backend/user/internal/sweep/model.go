package sweep

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// querier is the sweep's connection, or a transaction in tests.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// model owns every sweep query.
type model struct{ db querier }

// retention is what's deleted from each table: constants, never input.
var retention = []struct{ table, where string }{
	// Used tokens stay two weeks: presenting one revokes its sign-in (reuse detection).
	{"refresh_tokens", `expires_at < now() or used_at < now() - interval '14 days'`},
	{"auth_tokens", `coalesce(used_at, expires_at) < now() - interval '1 day'`},
	{"notifications", `read_at < now() - interval '90 days'`},
	{"outbound_mail", `failed_at < now() - interval '30 days'`},
	{"webhook_deliveries", `created_at < now() - interval '30 days'`},
	// The longest failure window is an hour; sign-in attempts also prune them as they go.
	{"auth_failures", `at < now() - interval '1 day'`},
	// Presence prunes rows past 90s every 30s while any instance runs; this catches the rest.
	{"issue_viewers", `seen_at < now() - interval '1 hour'`},
	{"realtime_conns", `seen_at < now() - interval '1 hour'`},
	// Every bucket refills within an hour (the slowest, emails per address, in 15 minutes); a missing row is a full one.
	{"rate_limits", `at < now() - interval '1 hour'`},
	// Registration is open, so clients with no code in flight go after 30 days; their tokens live on in api_tokens.
	{"oauth_codes", `expires_at < now()`},
	// Trashed issues go for good after 30 days, with their comments and the rest (cascades).
	{"issues_all", `deleted_at < now() - interval '30 days'`},
	{"workspace_audit", `at < now() - interval '1 year'`},
	{"oauth_clients", `created_at < now() - interval '30 days' and not exists (select 1 from oauth_codes c where c.client_id = oauth_clients.id)`},
}

const deleteBatch = 5000

// deleteAll deletes table's rows matching where, deleteBatch at a time so no statement runs or locks for long.
func (m model) deleteAll(ctx context.Context, table, where string) (int64, error) {
	var total int64
	for {
		tag, err := m.db.Exec(ctx, fmt.Sprintf(
			`delete from %[1]s where ctid = any(array(select ctid from %[1]s where %[2]s limit %[3]d))`, table, where, deleteBatch))
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < deleteBatch {
			return total, nil
		}
	}
}

// bigDocLimit is how many stored live-editing updates an issue may have before the sweep logs it.
const bigDocLimit = 1000

type bigDoc struct {
	issueID string
	rows    int
}

// bigDocs returns the issues with more than bigDocLimit stored updates.
func (m model) bigDocs(ctx context.Context) ([]bigDoc, error) {
	rows, err := m.db.Query(ctx, `select issue_id::text, count(*) from issue_doc_updates group by issue_id having count(*) > $1`, bigDocLimit)
	if err != nil {
		return nil, fmt.Errorf("count doc updates: %w", err)
	}
	ds, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (bigDoc, error) {
		var d bigDoc
		err := row.Scan(&d.issueID, &d.rows)
		return d, err
	})
	if err != nil {
		return nil, fmt.Errorf("count doc updates: %w", err)
	}
	return ds, nil
}

// fileRef finds an upload's id in a URL such as /api/files/<id>/<name>.
const fileRef = `/api/files/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/`

// referenced is every file id a document points at: an image's src, a link's href or any other string in an issue
// description (trashed ones too) or a comment that starts with a file URL, and any file URL in a live-editing update
// not yet saved (Yjs stores strings as UTF-8, so the URL shows in the bytes).
// ponytail: reads every document each sweep; keep a file_refs table maintained on save when this gets slow.
const referenced = `
	select substring(s #>> '{}' from '^` + fileRef + `')::uuid as id
	from (select description as d from issues_all where description is not null union all select body from comments) docs,
	     jsonb_path_query(docs.d, 'strict $.** ? (@.type() == "string" && @ starts with "/api/files/")') s
	union
	select m[1]::uuid from issue_doc_updates u, regexp_matches(encode(u.data, 'escape'), '` + fileRef + `', 'g') m`

// deleteUnreferencedFiles deletes up to 1000 rows at a time of files older than grace that no document references
// and that aren't an avatar, an AI test step's screenshot, a project's listed file or an issue's file, and returns their ids.
// ponytail: a document saved with a file between this statement's snapshot and its delete loses that file; it
// needs a >24h-old upload re-added at that instant. Lock against saves if it ever happens.
func (m model) deleteUnreferencedFiles(ctx context.Context, grace time.Duration) ([]string, error) {
	var all []string
	for {
		rows, err := m.db.Query(ctx, `
			with refs as (`+referenced+`)
			delete from files where id in (
			  select f.id from files f
			  where f.created_at < now() - make_interval(secs => $1) and not f.listed and f.issue_id is null
			    and not exists (select 1 from users u where u.avatar_file_id = f.id)
			    and not exists (select 1 from test_steps s where s.screenshot_id = f.id)
			    and not exists (select 1 from refs r where r.id = f.id)
			  limit 1000)
			returning id::text`, grace.Seconds())
		if err != nil {
			return all, fmt.Errorf("delete unreferenced files: %w", err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return all, fmt.Errorf("delete unreferenced files: %w", err)
		}
		all = append(all, ids...)
		if len(ids) < 1000 {
			return all, nil
		}
	}
}

// deleteExpiredUploads deletes direct uploads past their hour (file.liveUpload) and returns their ids.
func (m model) deleteExpiredUploads(ctx context.Context) ([]string, error) {
	rows, err := m.db.Query(ctx, `delete from uploads where created_at < now() - interval '1 hour' returning id::text`)
	if err != nil {
		return nil, fmt.Errorf("delete expired uploads: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("delete expired uploads: %w", err)
	}
	return ids, nil
}

// missingFiles returns the ids without a files row.
func (m model) missingFiles(ctx context.Context, ids []string) ([]string, error) {
	rows, err := m.db.Query(ctx, `select u.id::text from unnest($1::uuid[]) as u(id) where not exists (select 1 from files f where f.id = u.id)`, ids)
	if err != nil {
		return nil, fmt.Errorf("find missing files: %w", err)
	}
	gone, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("find missing files: %w", err)
	}
	return gone, nil
}
