# Admin overview

The admin Overview has six total tiles and two 30-day charts (signups, issues created). Staff also want to see the
business side (plans, subscriptions, trials, payment trouble, comps ending), what to act on, and whether the system is
healthy, without opening Paddle or the database.

All numbers come from our own database. Revenue amounts aren't stored (prices live in Paddle), so there's no MRR in
this version.

## API

`GET /api/stats` (admin, existing) keeps its fields and gains:

```json
{
  "totals": { "users": 0, "workspaces": 0, "projects": 0, "issues": 0, "comments": 0,
              "files_bytes": 0, "notes": 0, "feedback": 0 },
  "active_users_7d": 0,
  "active_users_30d": 0,
  "signups_7d": 0,
  "suspended_users": 0,
  "mfa_users": 0,
  "plans": { "free": 0, "starter": 0, "pro": 0, "advanced": 0 },
  "billing": {
    "paying": 0,          // subscriptions active or past_due
    "trialing": 0,
    "past_due": 0,
    "cancelling": 0,      // granting access with scheduled_change_action = 'cancel'
    "monthly": 0, "yearly": 0,   // granting access (active, trialing, past_due), by billing_interval
    "renewing_7d": 0,     // granting access with next_billed_at in the next 7 days
    "comped": 0,
    "comps_ending_7d": 0  // plan_comped_until in the next 7 days
  },
  "subscriptions": [      // the 10 most recently updated paddle_subscriptions
    { "user_id": "…", "email": "…", "name": "…", "plan": "pro", "status": "active", "interval": "month",
      "next_billed_at": "…|null", "scheduled_change": "cancel|null", "updated_at": "…" }
  ],
  "comps_ending": [       // comped users with plan_comped_until set, soonest first, at most 10
    { "user_id": "…", "email": "…", "name": "…", "plan": "pro", "until": "…" }
  ],
  "recent_signups": [     // newest 10 users
    { "user_id": "…", "email": "…", "name": "…", "plan": "free", "created_at": "…" }
  ],
  "top_workspaces": [     // 5 workspaces with the most issues
    { "id": "…", "name": "…", "slug": "…", "issues": 0, "members": 0, "plan": "free" }
  ],
  "health": {
    "mail_pending": 0,        // outbound_mail not sent and not failed
    "mail_failed_24h": 0,     // outbound_mail failed_at in the last day
    "webhooks_failed_24h": 0, // webhook_deliveries status 'failed' created in the last day
    "ai_runs_30d": 0, "ai_runs_failed_30d": 0,
    "ai_cost_micros_30d": 0   // sum of ai_usage.cost_micros in the last 30 days
  },
  "signups": [Day], "issues_created": [Day],
  "active_users": [Day],      // distinct users with a refresh token created that UTC day, last 30 days
  "workspaces_created": [Day]
}
```

Plan counts use `users.plan`. "Granting access" is the user API's statuses: active, trialing, past_due. The backend
agent checks the real column names and status values (outbound_mail, webhook_deliveries, test_runs, ai_usage) and
adjusts the queries, keeping the JSON above. One `stats` call, a few queries, each bounded; a test checks the shape
and a couple of counts against seeded rows.

## Frontend (admin app, `/` Overview)

Full width, sections top to bottom, each a heading and cards (the existing tile and BarChart styles):

1. **Totals** (tiles): users, workspaces, projects, issues, comments, notes, feedback, storage used (GB),
   active users 7 days / 30 days, signups this week.
2. **Billing** (tiles): paying, trialing, past due (accent pink when > 0), cancelling, renewing in 7 days,
   monthly / yearly, comped, comps ending in 7 days. Below: **Accounts by plan** (a horizontal bar per plan with its
   count and share).
3. **Needs attention** (cards with lists, each row links to the user in Users via `?q=<email>`):
   comps ending soon (name, plan, "ends Nov 24"), recent subscription changes (name, plan, status, interval, a
   "cancels" tag, date).
4. **Activity** (charts): signups, active users, issues created, workspaces created (30 days).
5. **Recent signups** and **Top workspaces** (lists; workspaces link to Workspaces `?q=<slug>`).
6. **System health** (tiles): mail waiting, mail failed 24h, webhook failures 24h, AI test runs 30 days (failed),
   AI cost 30 days (`$12.34` from micros). Non-zero failures in accent pink.

Sentence case, tokens only, phone width works (cards stack). Empty lists say what's empty ("No comps ending.").

## Not doing

- Revenue (MRR, ARR, refunds): needs Paddle's transactions API or stored amounts.
  `ponytail: read Paddle /transactions when staff need money numbers here.`
- Date range pickers; everything is fixed windows (7 / 30 days).
