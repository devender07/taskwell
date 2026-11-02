// What both apps' API clients share: errors as forms show them, and the redirect check for sign-in links.
export type FieldErrors = Record<string, string | null | undefined>

export const FALLBACK = 'Something went wrong. Try again.'
const OFFLINE = "You're offline. Reconnect and try again."
const UNREACHABLE = "Can't reach Tether. Check your connection and try again."

export class ApiError extends Error {
  status: number
  errors: FieldErrors
  error: string | null

  constructor(status: number, body?: { errors?: FieldErrors; error?: string }) {
    // Field errors stand on their own; no response at all (status 0) says offline or unreachable; anything else
    // (no body, proxy 500) gets the generic message.
    const error = body?.error ?? (body?.errors ? null : status === 0 ? (navigator.onLine ? UNREACHABLE : OFFLINE) : FALLBACK)
    super(error ?? `HTTP ${status}`)
    this.status = status
    this.errors = body?.errors ?? {}
    this.error = error
  }
}

export function toFormErrors(e: unknown): { errors: FieldErrors; error: string | null } {
  return e instanceof ApiError ? { errors: e.errors, error: e.error } : { errors: {}, error: FALLBACK }
}

// A same-site path to go to after signing in, or undefined: no other host, no backslashes or control characters.
export function safeRedirect(v: unknown): string | undefined {
  return typeof v === 'string' && v.startsWith('/') && !v.startsWith('//') && !/[\\\x00-\x1f\x7f]/.test(v) ? v : undefined
}
