import { redirect } from '@tanstack/react-router'

export type User = { id: string; name: string; email: string }
export type FieldErrors = Record<string, string | null | undefined>

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

// GET when there is no body, POST the FormData otherwise. 204 resolves to undefined.
// A 401 outside /auth/* refreshes the access token once and retries.
export async function api<T = void>(path: string, body?: FormData): Promise<T> {
  let res = await send(path, body)
  if (res.status === 401 && !path.startsWith('/auth/')) {
    await refreshAccess()
    res = await send(path, body)
  }
  const data = res.status === 204 ? undefined : await res.json().catch(() => undefined)
  if (!res.ok) throw new ApiError(res.status, data)
  return data as T
}

async function send(path: string, body?: FormData) {
  try {
    return await fetch(`/api${path}`, body ? { method: 'POST', body } : undefined)
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

// beforeLoad for the sign-in pages. An unreachable API counts as signed out so the form can surface the error.
export async function redirectIfSignedIn() {
  if (await getMe().catch(() => null)) throw redirect({ to: '/' })
}

// Any thrown value → { errors, error } for a form's state.
export function toFormErrors(e: unknown): { errors: FieldErrors; error: string | null } {
  return e instanceof ApiError ? { errors: e.errors, error: e.error } : { errors: {}, error: FALLBACK }
}
