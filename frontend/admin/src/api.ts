import { redirect } from '@tanstack/react-router'
import { ApiError } from '@taskwell/shared/api-errors'
import { apiUrl } from '@taskwell/shared/api-url'

export { ApiError, safeRedirect, toFormErrors, type FieldErrors } from '@taskwell/shared/api-errors'

export type Admin = { id: string; name: string; email: string }
export type AdminAccount = Admin & { created_at: string; mfa: boolean; last_seen_at: string | null }
export type Enrolment = { secret: string; url: string; recovery_codes: string[] }
export type AuditEntry = { id: string; at: string; actor_id: string | null; actor_email: string; action: string; target: string; ip: string }
export type Setting = { key: string; value: string; secret: boolean; has_value?: boolean; description: string; updated_at: string }
export const PLANS = { free: 'Free', starter: 'Starter', pro: 'Pro', advanced: 'Advanced' }
// plan, comped: the owner's.
export type WorkspaceRow = { id: string; name: string; slug: string; members: number; projects: number; issues: number; created_at: string; plan: keyof typeof PLANS; comped?: boolean }
export type UserRow = {
  id: string
  name: string
  email: string
  workspaces: number
  created_at: string
  suspended: boolean
  last_active_at: string | null
  plan: keyof typeof PLANS
  comped: boolean
  comped_until: string | null
}
export type Day = { date: string; count: number }
type Plan = keyof typeof PLANS
export type Stats = {
  totals: { users: number; workspaces: number; projects: number; issues: number; comments: number; files_bytes: number; notes: number; feedback: number }
  active_users_7d: number
  active_users_30d: number
  signups_7d: number
  suspended_users: number
  mfa_users: number
  plans: Record<Plan, number>
  billing: {
    paying: number
    trialing: number
    past_due: number
    cancelling: number
    monthly: number
    yearly: number
    renewing_7d: number
    comped: number
    comps_ending_7d: number
  }
  subscriptions: { user_id: string; email: string; name: string; plan: Plan; status: string; interval: string; next_billed_at: string | null; scheduled_change: string | null; updated_at: string }[]
  comps_ending: { user_id: string; email: string; name: string; plan: Plan; until: string }[]
  recent_signups: { user_id: string; email: string; name: string; plan: Plan; created_at: string }[]
  top_workspaces: { id: string; name: string; slug: string; issues: number; members: number; plan: Plan }[]
  health: { mail_pending: number; mail_failed_24h: number; webhooks_failed_24h: number; ai_runs_30d: number; ai_runs_failed_30d: number; ai_cost_micros_30d: number }
  signups: Day[]
  issues_created: Day[]
  active_users: Day[]
  workspaces_created: Day[]
  revenue: {
    currencies: { currency: string; mrr: number; revenue_30d: number; earnings_30d: number; refunds_30d: number; chargebacks_30d: number; revenue_all: number; per_day: Day[] }[]
    recent: { transaction_id: string; email: string; name: string; currency: string; total: number; status: string; billed_at: string }[]
  }
}

export type UserDetail = {
  user: UserRow
  mfa: boolean
  workspaces: { id: string; name: string; slug: string; role: string; owner: boolean; members: number; issues: number }[]
  subscriptions: { subscription_id: string; plan: Plan; status: string; interval: string; currency: string; unit_amount: number; next_billed_at: string | null; updated_at: string }[]
  payments: { transaction_id: string; currency: string; total: number; status: string; billed_at: string }[]
  plan_history: { at: string; actor: string | null; action: string; detail: string }[]
  events: { action: string; detail: string; ip: string; user_agent: string; created_at: string }[]
}

// An amount in the currency's minor units (cents), e.g. "$12" or "$12.50".
export const money = (minor: number, currency: string) => {
  const f = new Intl.NumberFormat(undefined, { style: 'currency', currency, trailingZeroDisplay: 'stripIfInteger' } as Intl.NumberFormatOptions)
  return f.format(minor / 10 ** (f.resolvedOptions().maximumFractionDigits ?? 2))
}

export const PAGE_SIZE = 50

// GET without a body, POST with one. 204 resolves to undefined. A 401 outside /auth/* and /me means the
// session ended (8h, or 1h unused): back to sign-in, returning here afterwards.
// The realtime connection id, so the API can tag this tab's own changes and the tab skips their events.
let client = ''
export const setClient = (id: string) => (client = id)

export async function api<T = void>(path: string, body?: FormData): Promise<T> {
  let res: Response
  try {
    res = await fetch(apiUrl(`/api${path}`), { method: body ? 'POST' : 'GET', body, credentials: 'include', headers: client ? { 'X-Taskwell-Client': client } : undefined })
  } catch {
    throw new ApiError(0)
  }
  const data = res.status === 204 ? undefined : await res.json().catch(() => undefined)
  if (res.status === 401 && !path.startsWith('/auth/') && path !== '/me') {
    location.assign(`/signin?redirect=${encodeURIComponent(location.pathname + location.search)}`)
  }
  if (!res.ok) throw new ApiError(res.status, data)
  return data as T
}

export async function getMe(): Promise<Admin | null> {
  try {
    return await api<Admin>('/me')
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null
    throw e
  }
}

export async function redirectIfSignedIn({ search }: { search: { redirect?: string } }) {
  if (await getMe().catch(() => null)) throw redirect({ href: search.redirect ?? '/' })
}

export type ListSearch = { q?: string; page?: number }

// The router keeps digit-only strings JSON-quoted, so `q` may come back as a number.
export function validateListSearch(search: Record<string, unknown>): ListSearch {
  const q = typeof search.q === 'string' || typeof search.q === 'number' ? String(search.q).trim() : ''
  const page = Number(search.page)
  return { ...(q && { q }), ...(Number.isInteger(page) && page > 1 && { page }) }
}

export const listQuery = ({ q, page }: ListSearch) => new URLSearchParams({ q: q ?? '', page: String(page ?? 1) }).toString()

export { longDate as formatDate } from '@taskwell/shared/time'
