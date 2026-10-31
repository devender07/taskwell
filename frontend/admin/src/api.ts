import { redirect } from '@tanstack/react-router'

export type Admin = { id: string; name: string; email: string }
export type Setting = { key: string; value: string; secret: boolean; has_value?: boolean; description: string; updated_at: string }
export type WorkspaceRow = { id: string; name: string; slug: string; members: number; projects: number; issues: number; created_at: string }
export type UserRow = {
  id: string
  name: string
  email: string
  workspaces: number
  created_at: string
  suspended: boolean
  platform_admin: boolean
  last_active_at: string | null
}
export type Day = { date: string; count: number }
export type Stats = {
  totals: { users: number; workspaces: number; projects: number; issues: number; comments: number }
  active_users_7d: number
  signups: Day[]
  issues_created: Day[]
}
export type FieldErrors = Record<string, string | null | undefined>

export const PAGE_SIZE = 50
const FALLBACK = 'Something went wrong. Try again.'

export class ApiError extends Error {
  status: number
  errors: FieldErrors
  error: string | null

  constructor(status: number, body?: { errors?: FieldErrors; error?: string }) {
    const error = body?.error ?? (body?.errors ? null : FALLBACK)
    super(error ?? `HTTP ${status}`)
    this.status = status
    this.errors = body?.errors ?? {}
    this.error = error
  }
}

// GET without a body, POST with one. 204 resolves to undefined. A 401 outside /auth/* and /me means the
// 8h session ran out: back to sign-in, returning here afterwards.
// The realtime connection id, so the API can tag this tab's own changes and the tab skips their events.
let client = ''
export const setClient = (id: string) => (client = id)

export async function api<T = void>(path: string, body?: FormData): Promise<T> {
  let res: Response
  try {
    res = await fetch(`/api${path}`, { method: body ? 'POST' : 'GET', body, headers: client ? { 'X-Tether-Client': client } : undefined })
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

export function safeRedirect(v: unknown): string | undefined {
  return typeof v === 'string' && v.startsWith('/') && !v.startsWith('//') && !v.startsWith('/\\') ? v : undefined
}

export async function redirectIfSignedIn({ search }: { search: { redirect?: string } }) {
  if (await getMe().catch(() => null)) throw redirect({ href: search.redirect ?? '/' })
}

export function toFormErrors(e: unknown): { errors: FieldErrors; error: string | null } {
  return e instanceof ApiError ? { errors: e.errors, error: e.error } : { errors: {}, error: FALLBACK }
}

export type ListSearch = { q?: string; page?: number }

// The router keeps digit-only strings JSON-quoted, so `q` may come back as a number.
export function validateListSearch(search: Record<string, unknown>): ListSearch {
  const q = typeof search.q === 'string' || typeof search.q === 'number' ? String(search.q).trim() : ''
  const page = Number(search.page)
  return { ...(q && { q }), ...(Number.isInteger(page) && page > 1 && { page }) }
}

export const listQuery = ({ q, page }: ListSearch) => new URLSearchParams({ q: q ?? '', page: String(page ?? 1) }).toString()

export const formatDate = (iso: string) => new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
