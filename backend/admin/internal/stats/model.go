package stats

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Totals struct {
	Users      int   `json:"users"`
	Workspaces int   `json:"workspaces"`
	Projects   int   `json:"projects"`
	Issues     int   `json:"issues"`
	Comments   int   `json:"comments"`
	FilesBytes int64 `json:"files_bytes"`
	Notes      int   `json:"notes"`
	Feedback   int   `json:"feedback"`
}

type Plans struct {
	Free     int `json:"free"`
	Starter  int `json:"starter"`
	Pro      int `json:"pro"`
	Advanced int `json:"advanced"`
}

type Billing struct {
	Paying        int `json:"paying"`
	Trialing      int `json:"trialing"`
	PastDue       int `json:"past_due"`
	Cancelling    int `json:"cancelling"`
	Monthly       int `json:"monthly"`
	Yearly        int `json:"yearly"`
	Renewing7d    int `json:"renewing_7d"`
	Comped        int `json:"comped"`
	CompsEnding7d int `json:"comps_ending_7d"`
}

type Subscription struct {
	UserID          string     `json:"user_id"`
	Email           string     `json:"email"`
	Name            string     `json:"name"`
	Plan            string     `json:"plan"`
	Status          string     `json:"status"`
	Interval        string     `json:"interval"`
	NextBilledAt    *time.Time `json:"next_billed_at"`
	ScheduledChange *string    `json:"scheduled_change"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type CompEnding struct {
	UserID string    `json:"user_id"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	Plan   string    `json:"plan"`
	Until  time.Time `json:"until"`
}

type Signup struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Plan      string    `json:"plan"`
	CreatedAt time.Time `json:"created_at"`
}

type TopWorkspace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Issues  int    `json:"issues"`
	Members int    `json:"members"`
	Plan    string `json:"plan"`
}

type Health struct {
	MailPending       int   `json:"mail_pending"`
	MailFailed24h     int   `json:"mail_failed_24h"`
	WebhooksFailed24h int   `json:"webhooks_failed_24h"`
	AIRuns30d         int   `json:"ai_runs_30d"`
	AIRunsFailed30d   int   `json:"ai_runs_failed_30d"`
	AICostMicros30d   int64 `json:"ai_cost_micros_30d"`
}

type Day struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type Currency struct {
	Currency       string `json:"currency"`
	MRR            int64  `json:"mrr"`
	Revenue30d     int64  `json:"revenue_30d"`
	Earnings30d    int64  `json:"earnings_30d"`
	Refunds30d     int64  `json:"refunds_30d"`
	Chargebacks30d int64  `json:"chargebacks_30d"`
	RevenueAll     int64  `json:"revenue_all"`
	PerDay         []Day  `json:"per_day"`
}

type Payment struct {
	TransactionID string    `json:"transaction_id"`
	UserID        string    `json:"user_id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	Currency      string    `json:"currency"`
	Total         int64     `json:"total"`
	Status        string    `json:"status"`
	BilledAt      time.Time `json:"billed_at"`
}

type Revenue struct {
	Currencies []Currency `json:"currencies"`
	Recent     []Payment  `json:"recent"`
}

type Stats struct {
	Totals            Totals         `json:"totals"`
	ActiveUsers7d     int            `json:"active_users_7d"`
	ActiveUsers30d    int            `json:"active_users_30d"`
	Signups7d         int            `json:"signups_7d"`
	SuspendedUsers    int            `json:"suspended_users"`
	MFAUsers          int            `json:"mfa_users"`
	Plans             Plans          `json:"plans"`
	Billing           Billing        `json:"billing"`
	Subscriptions     []Subscription `json:"subscriptions"`
	CompsEnding       []CompEnding   `json:"comps_ending"`
	RecentSignups     []Signup       `json:"recent_signups"`
	TopWorkspaces     []TopWorkspace `json:"top_workspaces"`
	Health            Health         `json:"health"`
	Signups           []Day          `json:"signups"`
	IssuesCreated     []Day          `json:"issues_created"`
	ActiveUsers       []Day          `json:"active_users"`
	WorkspacesCreated []Day          `json:"workspaces_created"`
	Revenue           Revenue        `json:"revenue"`
}

// model owns every stats query.
type model struct{ db *pgxpool.Pool }

// granting is the subscription statuses that give the plan, as in the user API's billing.
const granting = `status in ('active', 'trialing', 'past_due')`

func (m *model) stats(ctx context.Context) (Stats, error) {
	var s Stats
	t, p, b, h := &s.Totals, &s.Plans, &s.Billing, &s.Health
	// One row of single-row aggregates. "Active" means a sign-in or token refresh: each inserts a refresh token, so
	// its created_at is the activity, read through refresh_tokens_created_at_idx. Sent mail is deleted, so a queued
	// row without failed_at is waiting. An AI run "failed" when it ended in error (it broke), not when a test failed.
	// ponytail: users, paddle_subscriptions and the 24h/30d windows on outbound_mail, webhook_deliveries and test_runs
	// are scanned whole; add created_at indexes (or cache the stats) once those tables are large.
	err := m.db.QueryRow(ctx, `
		select u.total, (select count(*) from workspaces), (select count(*) from projects), (select count(*) from issues),
		       (select count(*) from comments), (select coalesce(sum(size), 0) from files), (select count(*) from notes),
		       (select count(*) from feedback),
		       (select count(distinct user_id) from refresh_tokens where created_at > now() - interval '7 days'),
		       (select count(distinct user_id) from refresh_tokens where created_at > now() - interval '30 days'),
		       u.signups_7d, u.suspended, (select count(*) from user_mfa where enabled_at is not null),
		       u.free, u.starter, u.pro, u.advanced,
		       s.paying, s.trialing, s.past_due, s.cancelling, s.monthly, s.yearly, s.renewing, u.comped, u.comps_ending,
		       (select count(*) from outbound_mail where failed_at is null),
		       (select count(*) from outbound_mail where failed_at > now() - interval '1 day'),
		       (select count(*) from webhook_deliveries where status = 'failed' and created_at > now() - interval '1 day'),
		       r.runs, r.failed, (select coalesce(sum(cost_micros), 0) from ai_usage where created_at > now() - interval '30 days')
		from (select count(*) as total,
		             count(*) filter (where created_at > now() - interval '7 days') as signups_7d,
		             count(*) filter (where suspended_at is not null) as suspended,
		             count(*) filter (where plan = 'free') as free, count(*) filter (where plan = 'starter') as starter,
		             count(*) filter (where plan = 'pro') as pro, count(*) filter (where plan = 'advanced') as advanced,
		             count(*) filter (where plan_comped) as comped,
		             count(*) filter (where plan_comped and plan_comped_until < now() + interval '7 days') as comps_ending
		      from users) u,
		     (select count(*) filter (where status in ('active', 'past_due')) as paying,
		             count(*) filter (where status = 'trialing') as trialing,
		             count(*) filter (where status = 'past_due') as past_due,
		             count(*) filter (where `+granting+` and scheduled_change_action = 'cancel') as cancelling,
		             count(*) filter (where `+granting+` and billing_interval = 'month') as monthly,
		             count(*) filter (where `+granting+` and billing_interval = 'year') as yearly,
		             count(*) filter (where `+granting+` and next_billed_at between now() and now() + interval '7 days') as renewing
		      from paddle_subscriptions) s,
		     (select count(*) as runs, count(*) filter (where status = 'error') as failed
		      from test_runs where kind = 'ai' and created_at > now() - interval '30 days') r`).
		Scan(&t.Users, &t.Workspaces, &t.Projects, &t.Issues, &t.Comments, &t.FilesBytes, &t.Notes, &t.Feedback,
			&s.ActiveUsers7d, &s.ActiveUsers30d, &s.Signups7d, &s.SuspendedUsers, &s.MFAUsers,
			&p.Free, &p.Starter, &p.Pro, &p.Advanced,
			&b.Paying, &b.Trialing, &b.PastDue, &b.Cancelling, &b.Monthly, &b.Yearly, &b.Renewing7d, &b.Comped, &b.CompsEnding7d,
			&h.MailPending, &h.MailFailed24h, &h.WebhooksFailed24h, &h.AIRuns30d, &h.AIRunsFailed30d, &h.AICostMicros30d)
	if err != nil {
		return Stats{}, fmt.Errorf("totals: %w", err)
	}

	if s.Subscriptions, err = collect(ctx, m.db, "subscriptions", `
		select coalesce(s.user_id::text, ''), coalesce(u.email, ''), coalesce(u.name, ''), s.plan, s.status,
		       coalesce(s.billing_interval, ''), s.next_billed_at, s.scheduled_change_action, s.updated_at
		from paddle_subscriptions s left join users u on u.id = s.user_id
		order by s.updated_at desc limit 10`,
		func(row pgx.CollectableRow) (Subscription, error) {
			var v Subscription
			err := row.Scan(&v.UserID, &v.Email, &v.Name, &v.Plan, &v.Status, &v.Interval, &v.NextBilledAt, &v.ScheduledChange, &v.UpdatedAt)
			return v, err
		}); err != nil {
		return Stats{}, err
	}
	// users_plan_comped_until is partial on plan_comped_until is not null, so this reads only comped users with an end.
	if s.CompsEnding, err = collect(ctx, m.db, "comps ending", `
		select id::text, email, name, plan, plan_comped_until from users
		where plan_comped_until is not null and plan_comped order by plan_comped_until limit 10`,
		func(row pgx.CollectableRow) (CompEnding, error) {
			var v CompEnding
			err := row.Scan(&v.UserID, &v.Email, &v.Name, &v.Plan, &v.Until)
			return v, err
		}); err != nil {
		return Stats{}, err
	}
	if s.RecentSignups, err = collect(ctx, m.db, "recent signups", `
		select id::text, email, name, plan, created_at from users order by created_at desc limit 10`,
		func(row pgx.CollectableRow) (Signup, error) {
			var v Signup
			err := row.Scan(&v.UserID, &v.Email, &v.Name, &v.Plan, &v.CreatedAt)
			return v, err
		}); err != nil {
		return Stats{}, err
	}
	// A workspace has its creator's plan. ponytail: counts every issue per workspace; keep a per-workspace counter if
	// issues grow into the millions.
	if s.TopWorkspaces, err = collect(ctx, m.db, "top workspaces", `
		select w.id::text, w.name, w.slug, i.n, (select count(*) from workspace_members m where m.workspace_id = w.id),
		       coalesce(u.plan, 'free')
		from (select workspace_id, count(*) as n from issues group by 1 order by 2 desc limit 5) i
		join workspaces w on w.id = i.workspace_id left join users u on u.id = w.created_by
		order by i.n desc, w.name`,
		func(row pgx.CollectableRow) (TopWorkspace, error) {
			var v TopWorkspace
			err := row.Scan(&v.ID, &v.Name, &v.Slug, &v.Issues, &v.Members, &v.Plan)
			return v, err
		}); err != nil {
		return Stats{}, err
	}

	for _, d := range []struct {
		into         *[]Day
		table, count string
	}{
		{&s.Signups, "users", "count(*)"},
		{&s.IssuesCreated, "issues", "count(*)"},
		{&s.ActiveUsers, "refresh_tokens", "count(distinct user_id)"},
		{&s.WorkspacesCreated, "workspaces", "count(*)"},
	} {
		if *d.into, err = m.perDay(ctx, d.table, d.count); err != nil {
			return Stats{}, err
		}
	}
	if s.Revenue, err = m.revenue(ctx); err != nil {
		return Stats{}, err
	}
	return s, nil
}

// revenue is the money per currency, from the Paddle rows the user API mirrors (docs/revenue-plan.md). MRR counts the
// subscriptions that have paid and still bill (active, past_due; a trial hasn't paid), a yearly one at a twelfth.
// ponytail: paddle_transactions is summed whole for revenue_all; keep a running total if it grows large.
func (m *model) revenue(ctx context.Context) (Revenue, error) {
	r := Revenue{Recent: []Payment{}}
	var err error
	if r.Currencies, err = collect(ctx, m.db, "revenue", `
		with t as (select currency_code as c, sum(total) as all_, sum(total) filter (where billed_at > now() - interval '30 days') as t30,
		                  sum(earnings) filter (where billed_at > now() - interval '30 days') as e30
		           from paddle_transactions group by 1),
		     s as (select currency_code as c, sum(case billing_interval when 'year' then unit_amount / 12 else unit_amount end) as mrr
		           from paddle_subscriptions where status in ('active', 'past_due') and currency_code is not null group by 1),
		     a as (select currency_code as c, sum(total) filter (where action = 'refund') as refunds,
		                  sum(total) filter (where action = 'chargeback') as chargebacks
		           from paddle_adjustments where status = 'approved' and created_at > now() - interval '30 days' group by 1)
		select c, coalesce(s.mrr, 0)::bigint, coalesce(t.t30, 0)::bigint, coalesce(t.e30, 0)::bigint,
		       coalesce(a.refunds, 0)::bigint, coalesce(a.chargebacks, 0)::bigint, coalesce(t.all_, 0)::bigint
		from t full join s using (c) left join a using (c)
		where coalesce(s.mrr, 0) > 0 or t.c is not null
		order by 3 desc, 1`,
		func(row pgx.CollectableRow) (Currency, error) {
			var v Currency
			err := row.Scan(&v.Currency, &v.MRR, &v.Revenue30d, &v.Earnings30d, &v.Refunds30d, &v.Chargebacks30d, &v.RevenueAll)
			return v, err
		}); err != nil {
		return r, err
	}
	for i := range r.Currencies {
		c := &r.Currencies[i]
		if c.PerDay, err = collect(ctx, m.db, "revenue per day", `
			with first as (select (now() at time zone 'utc')::date - 29 as d)
			select to_char(days.d, 'YYYY-MM-DD'), coalesce(t.n, 0)::bigint
			from first, generate_series(first.d, first.d + 29, interval '1 day') as days(d)
			left join (select (billed_at at time zone 'utc')::date as d, sum(total) as n from paddle_transactions
			           where currency_code = $1 and billed_at >= ((select d from first)::timestamp at time zone 'utc')
			           group by 1) t on t.d = days.d::date
			order by days.d`,
			func(row pgx.CollectableRow) (Day, error) {
				var d Day
				err := row.Scan(&d.Date, &d.Count)
				return d, err
			}, c.Currency); err != nil {
			return r, err
		}
	}
	// A $0 transaction (a trial's start) isn't a payment.
	if r.Recent, err = collect(ctx, m.db, "recent payments", `
		select t.transaction_id, coalesce(t.user_id::text, ''), coalesce(u.email, ''), coalesce(u.name, ''), t.currency_code,
		       t.total, t.status, t.billed_at
		from paddle_transactions t left join users u on u.id = t.user_id
		where t.total > 0 order by t.billed_at desc limit 10`,
		func(row pgx.CollectableRow) (Payment, error) {
			var v Payment
			err := row.Scan(&v.TransactionID, &v.UserID, &v.Email, &v.Name, &v.Currency, &v.Total, &v.Status, &v.BilledAt)
			return v, err
		}); err != nil {
		return r, err
	}
	return r, nil
}

// collect runs a list query with args, naming it in the error.
func collect[T any](ctx context.Context, db *pgxpool.Pool, what, sql string, scan pgx.RowToFunc[T], args ...any) ([]T, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	list, err := pgx.CollectRows(rows, scan)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return list, nil
}

// perDay counts rows of table with count (both constants, never user input) created on each of the last 30 UTC days,
// oldest first. The created_at range reads only those days' rows through the table's created_at index (workspaces
// has none; it's small).
func (m *model) perDay(ctx context.Context, table, count string) ([]Day, error) {
	return collect(ctx, m.db, table+" per day", `
		with first as (select (now() at time zone 'utc')::date - 29 as d)
		select to_char(days.d, 'YYYY-MM-DD'), coalesce(c.n, 0)
		from first, generate_series(first.d, first.d + 29, interval '1 day') as days(d)
		left join (select (created_at at time zone 'utc')::date as d, `+count+` as n from `+table+`
		           where created_at >= ((select d from first)::timestamp at time zone 'utc') group by 1) c on c.d = days.d::date
		order by days.d`,
		func(row pgx.CollectableRow) (Day, error) {
			var d Day
			err := row.Scan(&d.Date, &d.Count)
			return d, err
		})
}

// PaymentRow is a row of GET /api/payments.csv; amounts in minor units, earnings nil until Paddle sends it.
type PaymentRow struct {
	TransactionID, Email, Currency, Status string
	Subtotal, Tax, Total                   int64
	Earnings                               *int64
	BilledAt                               time.Time
}

// ponytail: every transaction in memory; stream the rows, or add the date filters, once there are hundreds of thousands.
func (m *model) payments(ctx context.Context) ([]PaymentRow, error) {
	return collect(ctx, m.db, "export payments", `
		select t.transaction_id, coalesce(u.email, ''), t.currency_code, t.status, t.subtotal, t.tax, t.total, t.earnings, t.billed_at
		from paddle_transactions t left join users u on u.id = t.user_id order by t.billed_at desc, t.transaction_id`,
		func(row pgx.CollectableRow) (PaymentRow, error) {
			var p PaymentRow
			err := row.Scan(&p.TransactionID, &p.Email, &p.Currency, &p.Status, &p.Subtotal, &p.Tax, &p.Total, &p.Earnings, &p.BilledAt)
			return p, err
		})
}
