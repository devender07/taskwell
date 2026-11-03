package gitlink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devender07/tether/backend/user/internal/issue"
	"github.com/devender07/tether/backend/user/internal/realtime"
)

var errNoHook = errors.New("webhook not found")

// Integration is a provider's connection as the settings page shows it; OpenStatus and MergeStatus are statuses or
// null (merge null: the workspace's first completed status).
type Integration struct {
	Provider       string          `json:"provider"`
	Connected      bool            `json:"connected"`
	WebhookURL     *string         `json:"webhook_url"`
	OpenStatus     json.RawMessage `json:"open_status"`
	MergeStatus    json.RawMessage `json:"merge_status"`
	LastDeliveryAt *time.Time      `json:"last_delivery_at"`
	token          string
}

type model struct{ db *pgxpool.Pool }

const statusJSON = `(select status_json(s) from statuses s where s.id = %s)`

// integration returns the workspace's connection to the provider; nil when there's none.
func (m *model) integration(ctx context.Context, workspaceID, provider string) (*Integration, error) {
	in := Integration{Provider: provider, Connected: true}
	err := m.db.QueryRow(ctx,
		`select h.token, `+fmt.Sprintf(statusJSON, "h.open_status_id")+`, `+fmt.Sprintf(statusJSON, "h.merge_status_id")+`,
		        h.last_delivery_at
		 from git_hooks h where h.workspace_id = $1 and h.provider = $2`, workspaceID, provider).
		Scan(&in.token, &in.OpenStatus, &in.MergeStatus, &in.LastDeliveryAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read integration: %w", err)
	}
	if in.LastDeliveryAt != nil {
		*in.LastDeliveryAt = in.LastDeliveryAt.UTC()
	}
	return &in, nil
}

// connect creates the workspace's hook for the provider, or gives an existing one a new token and sealed secret.
func (m *model) connect(ctx context.Context, workspaceID, provider, token, sealed, userID string, e realtime.Event) error {
	return m.publishing(ctx, e, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`insert into git_hooks (workspace_id, provider, token, secret, created_by) values ($1, $2, $3, $4, $5)
			 on conflict (workspace_id, provider) do update set token = excluded.token, secret = excluded.secret`,
			workspaceID, provider, token, sealed, userID)
		return err
	})
}

// setting is a status setting from the form: set false leaves it unchanged, statusID "" clears it.
type setting struct {
	set      bool
	statusID string
}

// statusExists reports whether the status is the workspace's.
func (m *model) statusExists(ctx context.Context, workspaceID, id string) (bool, error) {
	var ok bool
	err := m.db.QueryRow(ctx, `select exists(select 1 from statuses where id = $1 and workspace_id = $2)`, id, workspaceID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("find status: %w", err)
	}
	return ok, nil
}

// settings changes the hook's statuses; false when the workspace has no hook for the provider.
func (m *model) settings(ctx context.Context, workspaceID, provider string, open, merge setting, e realtime.Event) (bool, error) {
	found := false
	err := m.publishing(ctx, e, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`update git_hooks set open_status_id = case when $3 then nullif($4, '')::uuid else open_status_id end,
			   merge_status_id = case when $5 then nullif($6, '')::uuid else merge_status_id end
			 where workspace_id = $1 and provider = $2`,
			workspaceID, provider, open.set, open.statusID, merge.set, merge.statusID)
		found = tag.RowsAffected() > 0
		return err
	})
	return found, err
}

// disconnect deletes the hook; false when there's none. Linked pull requests stay on their issues.
func (m *model) disconnect(ctx context.Context, workspaceID, provider string, e realtime.Event) (bool, error) {
	found := false
	err := m.publishing(ctx, e, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `delete from git_hooks where workspace_id = $1 and provider = $2`, workspaceID, provider)
		found = tag.RowsAffected() > 0
		return err
	})
	return found, err
}

// publishing runs fn in a transaction that publishes e when it commits.
func (m *model) publishing(ctx context.Context, e realtime.Event, fn func(pgx.Tx) error) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		return realtime.Publish(ctx, tx, e)
	})
	if err != nil {
		return fmt.Errorf("change integration: %w", err)
	}
	return nil
}

// hook is a webhook as a delivery finds it by its token.
type hook struct{ id, workspaceID, secret string }

// hookByToken returns the provider's hook with the token; nil when there's none.
func (m *model) hookByToken(ctx context.Context, provider, token string) (*hook, error) {
	var h hook
	err := m.db.QueryRow(ctx, `select id::text, workspace_id::text, secret from git_hooks where provider = $1 and token = $2`,
		provider, token).Scan(&h.id, &h.workspaceID, &h.secret)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find webhook: %w", err)
	}
	return &h, nil
}

// lockHook locks the hook (one delivery at a time per hook), notes the delivery and publishes the settings page.
// It returns the hook's workspace slug and statuses, or errNoHook when it's gone.
func lockHook(ctx context.Context, tx pgx.Tx, hookID string) (wsID, slug string, open, merge *string, err error) {
	err = tx.QueryRow(ctx,
		`update git_hooks h set last_delivery_at = now() from workspaces w where h.id = $1 and w.id = h.workspace_id
		 returning w.id::text, w.slug, h.open_status_id::text, h.merge_status_id::text`, hookID).Scan(&wsID, &slug, &open, &merge)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, nil, errNoHook
	}
	if err != nil {
		return "", "", nil, nil, fmt.Errorf("lock webhook: %w", err)
	}
	err = realtime.Publish(ctx, tx, realtime.Event{Kind: "integrations.changed", Workspace: slug,
		Audience: realtime.ToWorkspace(wsID), Paths: []string{integrationsPath(slug)}})
	return wsID, slug, open, merge, err
}

func integrationsPath(slug string) string { return realtime.Workspace(slug) + "/integrations" }

// touch notes a delivery that changes nothing (a ping, an unhandled action).
func (m *model) touch(ctx context.Context, hookID string) error {
	return pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		_, _, _, _, err := lockHook(ctx, tx, hookID)
		return err
	})
}

// linked is an issue a pull request gained or lost.
type linked struct{ issueID, identifier string }

// deliver stores the hook's pull request, re-derives its issue links (logging pr_linked / pr_unlinked) and, when its
// state changed to open or merged, moves the closing links' issues to the hook's open or merge status.
func (m *model) deliver(ctx context.Context, hookID, provider string, p pr) error {
	err := pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		wsID, slug, openStatus, mergeStatus, err := lockHook(ctx, tx, hookID)
		if err != nil {
			return err
		}
		var before string // "" for a pull request seen first
		err = tx.QueryRow(ctx, `select state from pull_requests where workspace_id = $1 and provider = $2 and repo = $3 and number = $4`,
			wsID, provider, p.Repo, p.Number).Scan(&before)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read pull request: %w", err)
		}
		var prID string
		if err := tx.QueryRow(ctx,
			`insert into pull_requests (workspace_id, provider, repo, number, url, title, branch, author, state)
			 values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			 on conflict (workspace_id, provider, repo, number) do update set url = excluded.url, title = excluded.title,
			   branch = excluded.branch, author = excluded.author, state = excluded.state, updated_at = now()
			 returning id::text`,
			wsID, provider, p.Repo, p.Number, p.URL, p.Title, p.Branch, p.Author, p.State).Scan(&prID); err != nil {
			return fmt.Errorf("save pull request: %w", err)
		}
		gained, lost, err := relink(ctx, tx, wsID, prID, Parse(p.Branch, p.Title, p.Body))
		if err != nil {
			return err
		}
		data, err := json.Marshal(map[string]any{"provider": provider, "repo": p.Repo, "number": p.Number, "title": p.Title, "url": p.URL})
		if err != nil {
			return err
		}
		var ids []string
		for kind, ls := range map[string][]linked{"pr_linked": gained, "pr_unlinked": lost} {
			for _, l := range ls {
				if _, err := tx.Exec(ctx, `insert into issue_events (issue_id, kind, data) values ($1, $2, $3)`, l.issueID, kind, data); err != nil {
					return fmt.Errorf("add %s event: %w", kind, err)
				}
				ids = append(ids, l.issueID)
			}
		}
		// Every linked issue's page shows the pull request, so all of them refetch; each project's issues are told only
		// to who can see that project.
		rows, err := tx.Query(ctx,
			`select p.id::text, array_agg(p.key || '-' || i.number) from issues i join projects p on p.id = i.project_id
			 where i.id in (select issue_id from issue_pull_requests where pr_id = $1) or i.id = any($2::uuid[])
			 group by p.id`, prID, ids)
		if err != nil {
			return fmt.Errorf("linked issues: %w", err)
		}
		type group struct {
			projectID   string
			identifiers []string
		}
		groups, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (group, error) {
			var g group
			err := row.Scan(&g.projectID, &g.identifiers)
			return g, err
		})
		if err != nil {
			return fmt.Errorf("linked issues: %w", err)
		}
		for _, g := range groups {
			var paths []string
			for _, id := range g.identifiers {
				paths = append(paths, realtime.Issue(slug, id), realtime.Timeline(slug, id))
			}
			if len(ids) > 0 {
				paths = append(paths, realtime.Activity(slug))
			}
			if err := realtime.Publish(ctx, tx, realtime.Event{Kind: "issue.updated", Workspace: slug,
				Audience: realtime.ToWorkspace(wsID), Paths: paths, Project: g.projectID}); err != nil {
				return err
			}
		}
		return move(ctx, tx, wsID, prID, before, provider, p, openStatus, mergeStatus)
	})
	if errors.Is(err, errNoHook) {
		return err
	}
	if err != nil {
		return fmt.Errorf("deliver pull request: %w", err)
	}
	return nil
}

// relink replaces the pull request's links with the refs that name the workspace's issues, and returns the issues
// that gained and lost a link.
func relink(ctx context.Context, tx pgx.Tx, wsID, prID string, refs []Ref) (gained, lost []linked, err error) {
	keys, numbers, closes := make([]string, len(refs)), make([]int, len(refs)), make([]bool, len(refs))
	for i, r := range refs {
		keys[i], numbers[i], closes[i] = r.Key, r.Number, r.Closes
	}
	rows, err := tx.Query(ctx, `delete from issue_pull_requests where pr_id = $1 returning issue_id::text`, prID)
	if err != nil {
		return nil, nil, fmt.Errorf("unlink issues: %w", err)
	}
	old, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, nil, fmt.Errorf("unlink issues: %w", err)
	}
	rows, err = tx.Query(ctx,
		`insert into issue_pull_requests (issue_id, pr_id, closes)
		 select i.id, $5, bool_or(r.closes) from unnest($2::text[], $3::int[], $4::bool[]) as r(key, number, closes)
		 join projects p on p.workspace_id = $1 and p.key = canonical_key($1, r.key)
		 join issues i on i.project_id = p.id and i.number = r.number
		 group by i.id
		 returning issue_id::text`, wsID, keys, numbers, closes, prID)
	if err != nil {
		return nil, nil, fmt.Errorf("link issues: %w", err)
	}
	cur, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, nil, fmt.Errorf("link issues: %w", err)
	}
	var changed []string
	for _, id := range cur {
		if !slices.Contains(old, id) {
			changed = append(changed, id)
		}
	}
	for _, id := range old {
		if !slices.Contains(cur, id) {
			changed = append(changed, id)
		}
	}
	if len(changed) == 0 {
		return nil, nil, nil
	}
	rows, err = tx.Query(ctx, `select i.id::text, p.key || '-' || i.number from issues i join projects p on p.id = i.project_id
		where i.id = any($1::uuid[])`, changed)
	if err != nil {
		return nil, nil, fmt.Errorf("relinked issues: %w", err)
	}
	var l linked
	_, err = pgx.ForEachRow(rows, []any{&l.issueID, &l.identifier}, func() error {
		if slices.Contains(cur, l.issueID) {
			gained = append(gained, l)
		} else {
			lost = append(lost, l)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("relinked issues: %w", err)
	}
	return gained, lost, nil
}

// move moves the pull request's closing links' issues when its state changed from before: to openStatus (if set)
// from backlog or unstarted when it opened, to mergeStatus (default the first completed status) from anything but
// completed or canceled when it merged. Issues in archived projects stay.
func move(ctx context.Context, tx pgx.Tx, wsID, prID, before, provider string, p pr, openStatus, mergeStatus *string) error {
	var to *string
	var from []string
	switch {
	case p.State == before:
		return nil
	case p.State == "open" && openStatus != nil:
		to, from = openStatus, []string{"backlog", "unstarted"}
	case p.State == "merged":
		to, from = mergeStatus, []string{"backlog", "unstarted", "started"}
		if to == nil {
			err := tx.QueryRow(ctx, `select id::text from statuses where workspace_id = $1 and category = 'completed'
				order by position limit 1`, wsID).Scan(&to)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("find done status: %w", err)
			}
		}
	default:
		return nil
	}
	rows, err := tx.Query(ctx,
		`select i.id::text from issue_pull_requests l join issues i on i.id = l.issue_id join statuses s on s.id = i.status_id
		 join projects p on p.id = i.project_id
		 where l.pr_id = $1 and l.closes and s.category = any($2) and i.status_id <> $3 and p.archived_at is null
		 order by i.id`, prID, from, *to)
	if err != nil {
		return fmt.Errorf("issues to move: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("issues to move: %w", err)
	}
	via := map[string]any{"provider": provider, "repo": p.Repo, "number": p.Number, "url": p.URL}
	for _, id := range ids {
		if err := issue.SetStatus(ctx, tx, id, *to, via); err != nil {
			return err
		}
	}
	return nil
}
