# Upgrade prompts, billing reminders and onboarding

Three small growth features built together.

1. **Upgrade prompts.** A feature that's off for the workspace's plan but on for a higher plan is shown locked with
   that plan's badge instead of hidden; opening it shows an upgrade page.
2. **Billing reminders.** Emails before a trial ends, when a payment fails and before a comp ends, plus an in-app
   banner while a payment is past due.
3. **Onboarding checklist.** New workspaces get a checklist on Home: create a project, invite a teammate, create an
   issue, connect GitHub.

## Schema

`backend/user/internal/db/migrations/038_growth.sql`:

- `paddle_subscriptions` gains `trial_reminded_at timestamptz` and `past_due_emailed_at timestamptz`.
- `users` gains `plan_comp_reminded_at timestamptz`.
- `workspaces` gains `onboarding_dismissed_at timestamptz`. Existing workspaces that already have an issue are set
  to `now()`, so established workspaces don't suddenly get a checklist.

## 1. Upgrade prompts (frontend only)

No new API. The frontend already has:
- `GET /api/workspaces/{slug}/features`: booleans for this workspace.
- Public `GET /api/features`: each feature's plan ids. It's empty when the switch is off for everyone.
- `GET /api/workspaces/{slug}/billing`: `plan`, `is_owner` and `owner.name`. It returns 403 for guests.

A feature is **locked** when it's off for the workspace and its plan list contains a plan ranked above the
workspace's plan (free < starter < pro < advanced). Its badge is the lowest such plan ("Pro"). A feature that's off
with no higher plan stays hidden, as today. Guests never see locked features.

- The `$workspace` route's `beforeLoad` fetches the plan lists and the workspace billing (in parallel with the
  features). It puts `upgrades: Partial<Record<Feature, PlanId>>` plus `owner` / `is_owner` in context.
  `useUpgrade(name)` returns the plan or undefined.
- The sidebar and the settings nav (including its bottom sheet) show locked items, dimmed, with a small plan chip
  (e.g. "Pro"). Panels inside pages (issue panels, pickers, palette commands) stay hidden as today.
- `requireFeature(name)`: when the feature is locked, it redirects to `/$workspace/upgrade?feature=<name>` instead of
  showing not found.
- Route `/$workspace/upgrade?feature=<name>` (validateSearch) shows:
  - the feature's label and one-line description (reuse the pricing page's feature labels);
  - "Available on <Plan> and up";
  - for the owner, a **See plans** button to `/pricing`;
  - for anyone else, "Ask <owner name> to upgrade the workspace."
  
  An unknown or no-longer-locked feature redirects to the workspace home.

## 2. Billing reminders

### Job (user API, `billing` package)

The existing `Handler.Run` loop (every PLAN_GRACE_INTERVAL) also runs `remind`. Each kind is claimed one row at a
time with `for update skip locked`, its email is queued in the same transaction, and its marker is set, so every
email goes out once. All emails use `mail.Render` with the branded layout and a plain-text part.

| Kind | Who | Marker | Subject | Button |
|---|---|---|---|---|
| Trial ending | `status = 'trialing'`, `next_billed_at` within the next 3 days, no scheduled cancel, `trial_reminded_at is null` | `trial_reminded_at = now()` | "Your Taskwell <Plan> trial ends on Nov 24" | "Manage billing" |
| Payment failed | `status = 'past_due'`, `past_due_emailed_at is null` | `past_due_emailed_at = now()` | "Your Taskwell payment failed" | "Update payment method" |
| Comp ending | `plan_comped`, `plan_comped_until` within the next 3 days, `plan_comp_reminded_at is null` | `plan_comp_reminded_at = now()` | "Your Taskwell <Plan> access ends on Nov 24" | "See plans" |

The bodies are:
- **Trial ending:** "You'll be charged for <Plan> on Nov 24 unless you cancel."
- **Payment failed:** "We couldn't take your payment for <Plan>. Update your payment method to keep your plan."
- **Comp ending:** "The <Plan> plan Taskwell gave you ends on Nov 24. Choose a plan to keep its features."

Each email goes to the subscription's user, or to the comped user. Dates are formatted "Nov 24" in UTC.

The buttons link to:
- Manage billing and Update payment method: `<app_base_url>/billing`.
- See plans: `<app_base_url>/pricing`.

Markers reset so a later episode emails again:
- **Payment failed:** the job also runs `update paddle_subscriptions set past_due_emailed_at = null where status <>
  'past_due' and past_due_emailed_at is not null`.
- **Comp ending:** the admin API's set-plan (`backend/admin/internal/directory`) sets `plan_comp_reminded_at = null`
  whenever it sets or clears a comp. The comp-ended job clears it too.
- **Trial ending:** a trial happens once per account, so it never resets.

A test covers each kind: one email is queued and running the job again queues nothing. It also checks that the
past_due marker resets.

### Banner (frontend)

- The `_app` route loads `GET /api/billing` (already realtime-refreshed on `/billing`). While `status ===
  'past_due'`, a banner shows at the top of the workspace layout, in the pink accent:
  - text: "Your last payment failed. Update your payment method to keep <Plan>.";
  - an **Update payment method** button that opens the Paddle portal (the existing `POST /api/billing/portal` used
    by Settings → Billing).
  
  There's no dismiss. It shows only for the paying user (their own billing). Members aren't shown it.
- Route `/billing` (top level, signed in): redirects to `/$workspace/settings/billing` for the first workspace the
  user owns, else their first workspace, else `/pricing`. This is the email link.

## 3. Onboarding checklist

### API (user API, `workspace` package or a new small `onboarding` package)

- `GET /api/workspaces/{slug}/onboarding`, for a member and 404 otherwise (guests get the workspace 403 rule):

  ```json
  { "dismissed": false,
    "steps": [ {"id": "project", "done": true}, {"id": "invite", "done": false},
               {"id": "issue", "done": false}, {"id": "github", "done": false} ] }
  ```

  The steps are:
  - `project`: the workspace has any project.
  - `invite`: another member or a pending invite. It's left out when the plan's member limit is 1 (Free).
  - `issue`: any issue.
  - `github`: any connected GitHub or GitLab integration. It's left out when the integrations feature is off for the
    workspace's plan.
- `POST /api/workspaces/{slug}/onboarding/dismiss`: needs the workspace manage permission (the one Settings →
  General uses; 403 otherwise). It sets `onboarding_dismissed_at = now()` and answers 204. It's audited as
  `workspace.onboarding_dismissed`, and the frontend audit labels add "Dismissed the getting started checklist".
- A test covers: steps flip to done as rows are added, invite is omitted on Free, and dismiss is set and 403 for a
  plain member.

### Frontend (Home, `/$workspace/` index)

- A **Getting started** card at the top of Home. It shows when `!dismissed`, at least one step isn't done, and the
  viewer can manage the workspace.
  - Each step is a row with a check (green when done, done rows struck through) and a link:
    - "Create a project": opens the new project flow or the projects page;
    - "Invite a teammate": goes to members;
    - "Create your first issue": opens quick-create;
    - "Connect GitHub": goes to Settings → Integrations.
  - It shows a progress line, "2 of 4 done".
  - A **Dismiss** text button posts dismiss and hides the card.
- Sentence case, tokens only, works at phone width.

## Not doing

- Upgrade prompts inside pages (issue panels, pickers, the command palette).
  `ponytail: add locked panels if the sidebar prompts convert.`
- A second "trial ends tomorrow" email, and dunning sequences beyond one email per past-due episode (Paddle retries
  and emails too).
- Per-user onboarding progress, or onboarding emails.
