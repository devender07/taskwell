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
	// "Active" means a sign-in or refresh in the last 7 days; refresh tokens are the only activity the auth tables keep.
	err := m.db.QueryRow(ctx, `
		select (select count(*) from users), (select count(*) from workspaces), (select count(*) from projects),
		       (select count(*) from issues),
		       (select count(distinct user_id) from refresh_tokens where coalesce(used_at, created_at) > now() - interval '7 days')`).
		Scan(&t.Users, &t.Workspaces, &t.Projects, &t.Issues, &s.ActiveUsers7d)
	if err != nil {
		return Stats{}, fmt.Errorf("totals: %w", err)
	}
	// ponytail: guard for databases from before the comments migration; drop it once every install has 014.
	var hasComments bool
	if err := m.db.QueryRow(ctx, `select to_regclass('comments') is not null`).Scan(&hasComments); err != nil {
		return Stats{}, fmt.Errorf("find comments table: %w", err)
	}
	if hasComments {
		if err := m.db.QueryRow(ctx, `select count(*) from comments`).Scan(&t.Comments); err != nil {
			return Stats{}, fmt.Errorf("count comments: %w", err)
		}
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
func (m *model) perDay(ctx context.Context, table string) ([]Day, error) {
	rows, err := m.db.Query(ctx, `
		with today as (select (now() at time zone 'utc')::date as d)
		select to_char(days.d, 'YYYY-MM-DD'), count(t.created_at)
		from today, generate_series(today.d - 29, today.d, interval '1 day') as days(d)
		left join `+table+` t on (t.created_at at time zone 'utc')::date = days.d::date
		group by days.d order by days.d`)
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
