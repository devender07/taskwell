package stats

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Totals struct {
	Users      int `json:"users"`
	Workspaces int `json:"workspaces"`
	Projects   int `json:"projects"`
	Issues     int `json:"issues"`
	Comments   int `json:"comments"`
}

type Day struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type Stats struct {
	Totals        Totals `json:"totals"`
	ActiveUsers7d int    `json:"active_users_7d"`
	Signups       []Day  `json:"signups"`
	IssuesCreated []Day  `json:"issues_created"`
}

// model owns every stats query.
type model struct{ db *pgxpool.Pool }

func (m *model) stats(ctx context.Context) (Stats, error) {
	var s Stats
	t := &s.Totals
	// "Active" means a sign-in or token refresh in the last 7 days: each inserts a refresh token, so its created_at
	// is the activity, read through refresh_tokens_created_at_idx.
	err := m.db.QueryRow(ctx, `
		select (select count(*) from users), (select count(*) from workspaces), (select count(*) from projects),
		       (select count(*) from issues), (select count(*) from comments),
		       (select count(distinct user_id) from refresh_tokens where created_at > now() - interval '7 days')`).
		Scan(&t.Users, &t.Workspaces, &t.Projects, &t.Issues, &t.Comments, &s.ActiveUsers7d)
	if err != nil {
		return Stats{}, fmt.Errorf("totals: %w", err)
	}
	if s.Signups, err = m.perDay(ctx, "users"); err != nil {
		return Stats{}, err
	}
	if s.IssuesCreated, err = m.perDay(ctx, "issues"); err != nil {
		return Stats{}, err
	}
	return s, nil
}

// perDay counts rows of table (a constant, never user input) created on each of the last 30 UTC days, oldest first.
// The created_at range reads only those days' rows through the table's created_at index.
func (m *model) perDay(ctx context.Context, table string) ([]Day, error) {
	rows, err := m.db.Query(ctx, `
		with first as (select (now() at time zone 'utc')::date - 29 as d)
		select to_char(days.d, 'YYYY-MM-DD'), coalesce(c.n, 0)
		from first, generate_series(first.d, first.d + 29, interval '1 day') as days(d)
		left join (select (created_at at time zone 'utc')::date as d, count(*) as n from `+table+`
		           where created_at >= ((select d from first)::timestamp at time zone 'utc') group by 1) c on c.d = days.d::date
		order by days.d`)
	if err != nil {
		return nil, fmt.Errorf("%s per day: %w", table, err)
	}
	days, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Day, error) {
		var d Day
		err := row.Scan(&d.Date, &d.Count)
		return d, err
	})
	if err != nil {
		return nil, fmt.Errorf("%s per day: %w", table, err)
	}
	return days, nil
}
