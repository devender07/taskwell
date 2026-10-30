import { redirect } from '@tanstack/react-router'

export type User = { id: string; name: string; email: string }
export type RoleRef = { id: string; name: string }
export type Workspace = { id: string; name: string; slug: string; role: RoleRef; permissions: string[] }
export type Member = { id: string; name: string; email: string; role: RoleRef }
export type Invite = { id: string; email: string; invited_by: string; expires_at: string; role: RoleRef }
export type Role = { id: string; name: string; builtin: 'admin' | 'member' | null; permissions: string[]; member_count: number }
export type Permission = { key: string; group: string; label: string; active: boolean }
export type FieldErrors = Record<string, string | null | undefined>

// The caller's effective permission, for gating UI; the API enforces it.
export const can = (workspace: Workspace, key: string) => workspace.permissions.includes(key)

const FALLBACK = 'Something went wrong. Try again.'

export class ApiError extends Error {
  status: number
  errors: FieldErrors
  error: string | null

  constructor(status: number, body?: { errors?: FieldErrors; error?: string }) {
    // Field errors stand on their own; anything else (no body, proxy 500, network) gets the generic message.
    const error = body?.error ?? (body?.errors ? null : FALLBACK)
    super(error ?? `HTTP ${status}`)
    this.status = status
    this.errors = body?.errors ?? {}
    this.error = error
  }
}

// GET when there is no body, POST the FormData otherwise (or pass `method`, e.g. 'DELETE'). 204 resolves to undefined.
// A 401 outside /auth/* refreshes the access token once and retries.
export async function api<T = void>(path: string, body?: FormData, method = body ? 'POST' : 'GET'): Promise<T> {
  let res = await send(path, body, method)
  if (res.status === 401 && !path.startsWith('/auth/')) {
    await refreshAccess()
    res = await send(path, body, method)
  }
  const data = res.status === 204 ? undefined : await res.json().catch(() => undefined)
  if (!res.ok) throw new ApiError(res.status, data)
  return data as T
}

async function send(path: string, body?: FormData, method = body ? 'POST' : 'GET') {
  try {
    return await fetch(`/api${path}`, { method, body })
  } catch {
    throw new ApiError(0)
  }
}

// One refresh at a time per tab; concurrent 401s wait on the same request.
let refreshing: Promise<unknown> | null = null
function refreshAccess() {
  refreshing ??= send('/auth/refresh', new FormData()).finally(() => (refreshing = null))
  return refreshing
}

export async function getMe(): Promise<User | null> {
  try {
    return await api<User>('/me')
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null
    throw e
  }
}

// An in-app path to return to after signing in. Anything else (another origin, "//x", "/\x") is dropped.
export function safeRedirect(v: unknown): string | undefined {
  return typeof v === 'string' && v.startsWith('/') && !v.startsWith('//') && !v.startsWith('/\\') ? v : undefined
}

// beforeLoad for the sign-in pages. An unreachable API counts as signed out so the form can surface the error.
export async function redirectIfSignedIn({ search }: { search: { redirect?: string } }) {
  if (await getMe().catch(() => null)) throw redirect({ href: search.redirect ?? '/' })
}

// Any thrown value → { errors, error } for a form's state.
export function toFormErrors(e: unknown): { errors: FieldErrors; error: string | null } {
  return e instanceof ApiError ? { errors: e.errors, error: e.error } : { errors: {}, error: FALLBACK }
}
