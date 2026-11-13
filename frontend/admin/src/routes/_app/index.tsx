import { createFileRoute } from '@tanstack/react-router'
import { relativeTime, shortDate } from '@taskwell/shared/time'
import { api, money, PLANS, type Setting, type Stats } from '@/api'
import { ExportLink } from '@/components/app/list-controls'
import { BarChart } from '@/components/overview/bar-chart'
import { BetaCard } from '@/components/overview/beta-card'
import { ListCard } from '@/components/overview/list-card'
import { PlanBars } from '@/components/overview/plan-bars'
import { ResetCard } from '@/components/overview/reset-card'
import { Grid, Section, Tile } from '@/components/overview/section'
import { loaderApi } from '@/realtime'

export const Route = createFileRoute('/_app/')({
  loader: async (ctx) => {
    const [stats, settings, local] = await Promise.all([
      loaderApi<Stats>(ctx, '/stats'),
      loaderApi<Setting[]>(ctx, '/config'),
      api('/dev/reset').then(() => true, () => false), // 404 in production or off localhost
    ])
    return { stats, beta: settings.find((s) => s.key === 'beta_mode')?.value === 'true', local }
  },
  component: Overview,
})

type Loose<T> = { [K in keyof T]?: T[K] extends unknown[] ? T[K] | null : T[K] extends object ? Partial<T[K]> : T[K] }

const zero = <K extends string>(o: Partial<Record<K, number>> | undefined, keys: K[]) =>
  Object.fromEntries(keys.map((k) => [k, o?.[k] ?? 0])) as Record<K, number>

// ponytail: fills fields an older API doesn't send yet; drop once every deploy serves the full contract.
function withDefaults(s: Loose<Stats>): Stats {
  return {
    totals: zero(s.totals, ['users', 'workspaces', 'projects', 'issues', 'comments', 'files_bytes', 'notes', 'feedback']),
    active_users_7d: s.active_users_7d ?? 0,
    active_users_30d: s.active_users_30d ?? 0,
    signups_7d: s.signups_7d ?? 0,
    suspended_users: s.suspended_users ?? 0,
    mfa_users: s.mfa_users ?? 0,
    plans: zero(s.plans, Object.keys(PLANS) as (keyof typeof PLANS)[]),
    billing: zero(s.billing, ['paying', 'trialing', 'past_due', 'cancelling', 'monthly', 'yearly', 'renewing_7d', 'comped', 'comps_ending_7d']),
    health: zero(s.health, ['mail_pending', 'mail_failed_24h', 'webhooks_failed_24h', 'ai_runs_30d', 'ai_runs_failed_30d', 'ai_cost_micros_30d']),
    subscriptions: s.subscriptions ?? [],
    comps_ending: s.comps_ending ?? [],
    recent_signups: s.recent_signups ?? [],
    top_workspaces: s.top_workspaces ?? [],
    signups: s.signups ?? [],
    issues_created: s.issues_created ?? [],
    active_users: s.active_users ?? [],
    workspaces_created: s.workspaces_created ?? [],
    revenue: { currencies: s.revenue?.currencies ?? [], recent: s.revenue?.recent ?? [] },
  }
}

const num = (n: number) => n.toLocaleString(undefined)
const gb = (bytes: number) => `${(bytes / 1024 ** 3).toFixed(bytes > 0 && bytes < 1024 ** 3 / 10 ? 2 : 1)} GB`
const dollars = (micros: number) => `$${(micros / 1e6).toFixed(2)}`
const planName = (plan: string) => PLANS[plan as keyof typeof PLANS] ?? plan

function Overview() {
  const { stats, beta, local } = Route.useLoaderData()
  const s = withDefaults(stats)
  const { totals, billing, health } = s

  return (
    <main className="grid gap-10 w-full px-6 py-10">
      <h1 className="text-h2">Overview</h1>
      <BetaCard on={beta} />
      {local ? <ResetCard /> : null}

      <Section title="Totals">
        <Grid>
          <Tile label="Users" value={totals.users} sub={`${num(s.signups_7d)} new this week`} />
          <Tile label="Active users, 7 days" value={s.active_users_7d} sub={`${num(s.active_users_30d)} in 30 days`} />
          <Tile label="Workspaces" value={totals.workspaces} />
          <Tile label="Projects" value={totals.projects} />
          <Tile label="Issues" value={totals.issues} />
          <Tile label="Comments" value={totals.comments} />
          <Tile label="Notes" value={totals.notes} />
          <Tile label="Feedback" value={totals.feedback} />
          <Tile label="Storage used" value={gb(totals.files_bytes)} />
        </Grid>
      </Section>

      <Section title="Billing">
        <Grid>
          <Tile label="Paying" value={billing.paying} />
          <Tile label="Trialing" value={billing.trialing} />
          <Tile label="Past due" value={billing.past_due} alert={billing.past_due > 0} />
          <Tile label="Cancelling" value={billing.cancelling} />
          <Tile label="Renewing in 7 days" value={billing.renewing_7d} />
          <Tile label="Monthly / yearly" value={`${num(billing.monthly)} / ${num(billing.yearly)}`} />
          <Tile label="Comped" value={billing.comped} sub={`${num(billing.comps_ending_7d)} ending in 7 days`} />
        </Grid>
        <PlanBars plans={s.plans} />
      </Section>

      <Section title="Revenue">
        {s.revenue.currencies.length === 0 ? (
          <p className="text-body-sm text-secondary m-0">No payments yet. Revenue shows here once Paddle sends payments.</p>
        ) : (
          <>
            {s.revenue.currencies.map((c) => {
              const m = (n: number) => money(n, c.currency)
              return (
                <Grid key={c.currency}>
                  <Tile label="MRR" value={m(c.mrr)} />
                  <Tile label="Revenue, 30 days" value={m(c.revenue_30d)} sub={c.earnings_30d > 0 ? `earnings ${m(c.earnings_30d)}` : undefined} />
                  <Tile label="Refunds, 30 days" value={m(c.refunds_30d)} alert={c.refunds_30d > 0} />
                  <Tile label="Chargebacks, 30 days" value={m(c.chargebacks_30d)} alert={c.chargebacks_30d > 0} />
                  <Tile label="All-time revenue" value={m(c.revenue_all)} />
                </Grid>
              )
            })}
            <Grid min={420}>
              {s.revenue.currencies.map((c) => (
                <BarChart key={c.currency} title={`Revenue, ${c.currency.toUpperCase()}`} days={c.per_day ?? []} format={(n) => money(n, c.currency)} />
              ))}
              <ListCard
                title="Recent payments"
                empty="No payments yet."
                action={<ExportLink path="/payments.csv" />}
                rows={s.revenue.recent.map((t) => ({
                  key: t.transaction_id,
                  to: '/users',
                  q: t.email,
                  title: t.name || t.email,
                  sub: `${money(t.total, t.currency)} · ${t.status}`,
                  end: shortDate(t.billed_at),
                }))}
              />
            </Grid>
          </>
        )}
      </Section>

      <Section title="Needs attention">
        <Grid min={420}>
          <ListCard
            title="Comps ending soon"
            empty="No comps ending."
            rows={s.comps_ending.map((c) => ({
              key: c.user_id,
              to: '/users',
              q: c.email,
              title: c.name || c.email,
              sub: planName(c.plan),
              end: `ends ${shortDate(c.until)}`,
            }))}
          />
          <ListCard
            title="Recent subscription changes"
            empty="No subscriptions yet."
            rows={s.subscriptions.map((x) => ({
              key: x.user_id,
              to: '/users',
              q: x.email,
              title: x.name || x.email,
              sub: (
                <>
                  {planName(x.plan)} · <span className={x.status === 'past_due' ? 'text-accent' : undefined}>{x.status.replace('_', ' ')}</span>
                  {x.interval ? ` · ${x.interval}ly` : ''}
                </>
              ),
              end: (
                <>
                  {x.scheduled_change === 'cancel' ? <span className="chip chip-danger">Cancels</span> : null}
                  {shortDate(x.updated_at)}
                </>
              ),
            }))}
          />
        </Grid>
      </Section>

      <Section title="Activity">
        <Grid min={420}>
          <BarChart title="Signups" days={s.signups} />
          <BarChart title="Active users" days={s.active_users} />
          <BarChart title="Issues created" days={s.issues_created} />
          <BarChart title="Workspaces created" days={s.workspaces_created} />
        </Grid>
      </Section>

      <Section title="People and workspaces">
        <Grid min={420}>
          <ListCard
            title="Recent signups"
            empty="No signups yet."
            rows={s.recent_signups.map((u) => ({
              key: u.user_id,
              to: '/users',
              q: u.email,
              title: u.name || u.email,
              sub: u.name ? `${u.email} · ${planName(u.plan)}` : planName(u.plan),
              end: relativeTime(u.created_at),
            }))}
          />
          <ListCard
            title="Top workspaces"
            empty="No workspaces yet."
            rows={s.top_workspaces.map((w) => ({
              key: w.id,
              to: '/workspaces',
              q: w.slug,
              title: w.name,
              sub: `${planName(w.plan)} · ${num(w.members)} ${w.members === 1 ? 'member' : 'members'}`,
              end: `${num(w.issues)} ${w.issues === 1 ? 'issue' : 'issues'}`,
            }))}
          />
        </Grid>
      </Section>

      <Section title="System health">
        <Grid>
          <Tile label="Mail waiting" value={health.mail_pending} />
          <Tile label="Mail failed, 24 hours" value={health.mail_failed_24h} alert={health.mail_failed_24h > 0} />
          <Tile label="Webhook failures, 24 hours" value={health.webhooks_failed_24h} alert={health.webhooks_failed_24h > 0} />
          <Tile
            label="AI test runs, 30 days"
            value={health.ai_runs_30d}
            sub={<span className={health.ai_runs_failed_30d > 0 ? 'text-accent' : undefined}>{num(health.ai_runs_failed_30d)} failed</span>}
          />
          <Tile label="AI cost, 30 days" value={dollars(health.ai_cost_micros_30d)} />
        </Grid>
      </Section>
    </main>
  )
}
