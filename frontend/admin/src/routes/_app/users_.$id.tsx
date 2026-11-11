import { Link, createFileRoute, notFound } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { ACCOUNT_EVENTS } from '@taskwell/shared/account-events'
import { relativeTime } from '@taskwell/shared/time'
import { ApiError, formatDate, money, PLANS, type UserDetail } from '@/api'
import { Section } from '@/components/overview/section'
import { PlanButton } from '@/components/users/plan-button'
import { SuspendButton } from '@/components/users/suspend-button'
import { loaderApi } from '@/realtime'

export const Route = createFileRoute('/_app/users_/$id')({
  loader: async (ctx) => {
    try {
      return await loaderApi<UserDetail>(ctx, `/users/${encodeURIComponent(ctx.params.id)}`)
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    }
  },
  component: UserPage,
})

const words = (s: string) => {
  const t = s.replace(/[._]/g, ' ')
  return t.charAt(0).toUpperCase() + t.slice(1)
}
const when = (iso: string | null) => (iso ? formatDate(iso) : '—')

function UserPage() {
  const { user, mfa, workspaces, subscriptions, payments, plan_history: history, events } = Route.useLoaderData()
  return (
    <main className="grid gap-10 w-full px-6 py-10">
      <header className="grid gap-4">
        <Link to="/users" className="text-body-sm text-secondary" style={{ justifySelf: 'start' }}>
          Users
        </Link>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="grid gap-2" style={{ minWidth: 0 }}>
            <h1 className="text-h2" style={{ overflowWrap: 'anywhere' }}>{user.name}</h1>
            <p className="text-body-sm text-secondary m-0" style={{ overflowWrap: 'anywhere' }}>{user.email}</p>
            <div className="flex flex-wrap items-center gap-2">
              <span className="chip">
                {PLANS[user.plan] ?? user.plan}
                {user.comped ? ` · comped${user.comped_until ? ` until ${formatDate(user.comped_until)}` : ''}` : ''}
              </span>
              {user.suspended ? <span className="chip chip-danger">Suspended</span> : null}
              <span className="text-caption text-secondary">
                Two-factor {mfa ? 'on' : 'off'} · Joined {formatDate(user.created_at)} · Last active {user.last_active_at ? relativeTime(user.last_active_at) : 'never'}
              </span>
            </div>
          </div>
          <div className="flex gap-2">
            <PlanButton user={user} />
            <SuspendButton user={user} />
          </div>
        </div>
      </header>

      <Table
        title="Workspaces"
        empty="No workspaces."
        head={['Name', 'Role', 'Members', 'Issues']}
        num={[2, 3]}
        rows={workspaces.map((w) => [
          w.id,
          <Link to="/workspaces" search={{ q: w.slug }} className="text-primary font-510">{w.name}</Link>,
          `${w.role}${w.owner ? ' · owner' : ''}`,
          w.members,
          w.issues,
        ])}
      />
      <Table
        title="Subscriptions"
        empty="No subscriptions."
        head={['Plan', 'Status', 'Price', 'Next bill', 'Updated']}
        rows={subscriptions.map((s) => [
          s.subscription_id,
          PLANS[s.plan] ?? s.plan,
          <span className={s.status === 'past_due' ? 'text-accent' : undefined}>{s.status.replace('_', ' ')}</span>,
          `${money(s.unit_amount, s.currency)} / ${s.interval}`,
          when(s.next_billed_at),
          when(s.updated_at),
        ])}
      />
      <Table
        title="Payments"
        empty="No payments."
        head={['Total', 'Status', 'Billed', 'Transaction']}
        num={[0]}
        rows={payments.map((p) => [p.transaction_id, money(p.total, p.currency), p.status, when(p.billed_at), <span className="text-secondary">{p.transaction_id}</span>])}
      />
      <Table
        title="Plan history"
        empty="No plan changes."
        head={['Change', 'Detail', 'By', 'When']}
        rows={history.map((h, i) => [`${h.at}-${i}`, words(h.action), h.detail, h.actor ?? '—', relativeTime(h.at)])}
      />
      <Table
        title="Sign-ins and security"
        empty="No account events."
        head={['Action', 'Detail', 'IP', 'When']}
        rows={events.map((e, i) => [
          `${e.created_at}-${i}`,
          <span className={e.action.endsWith('failed') ? 'text-accent' : undefined}>{ACCOUNT_EVENTS[e.action] ?? words(e.action)}</span>,
          e.detail,
          <span title={e.user_agent}>{e.ip}</span>,
          relativeTime(e.created_at),
        ])}
      />
    </main>
  )
}

// rows: [key, ...cells]; num: the cell indexes to right-align.
function Table({ title, empty, head, rows, num = [] }: { title: string; empty: string; head: string[]; rows: [string, ...ReactNode[]][]; num?: number[] }) {
  return (
    <Section title={title}>
      {rows.length === 0 ? (
        <p className="text-body-sm text-secondary m-0">{empty}</p>
      ) : (
        <div className="table-wrap bg-surface border rounded-lg">
          <table className="table">
            <thead>
              <tr>
                {head.map((h, i) => (
                  <th key={h} className={num.includes(i) ? 'num' : undefined}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map(([key, ...cells]) => (
                <tr key={key}>
                  {cells.map((c, i) => (
                    <td key={i} className={num.includes(i) ? 'num' : undefined}>{c}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  )
}
