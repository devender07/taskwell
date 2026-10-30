import { redirect } from '@tanstack/react-router'
import type { Doc } from '@tether/shared/rich-editor'

export type User = { id: string; name: string; email: string; has_password: boolean; avatar_url: string | null }
export type RoleRef = { id: string; name: string }
export type Workspace = { id: string; name: string; slug: string; role: RoleRef; permissions: string[] }
export type Member = { id: string; name: string; email: string; role: RoleRef }
export type Invite = { id: string; email: string; invited_by: string; expires_at: string; role: RoleRef }
export type Role = { id: string; name: string; builtin: 'admin' | 'member' | null; permissions: string[]; member_count: number }
export type Permission = { key: string; group: string; label: string; active: boolean }
export type Project = { id: string; name: string; key: string; description: string; archived: boolean; created_at: string }
export type PersonRef = { id: string; name: string }
export type StatusCategory = 'backlog' | 'unstarted' | 'started' | 'completed' | 'canceled'
export type Status = { id: string; name: string; category: StatusCategory; position: number }
export type Label = { id: string; name: string; color: string }
export type IssueRef = { id: string; identifier: string; title: string; status: Status; assignee: PersonRef | null }
export type SearchIssue = IssueRef & { project: { key: string; name: string }; match: 'title' | 'description' | 'identifier' | null }
export type SearchResult = { issues: SearchIssue[]; projects: Pick<Project, 'key' | 'name' | 'archived'>[] }
export type Issue = {
  id: string
  identifier: string
  number: number
  project: { key: string; name: string }
  title: string
  description: Doc | null
  status: Status
  assignee: PersonRef | null
  priority: number
  due_date: string | null
  labels: Label[]
  created_by: PersonRef | null
  created_at: string
  updated_at: string
  can_edit: boolean
  can_delete: boolean
  parent: IssueRef | null
  sub_issues?: IssueRef[]
  blocks?: IssueRef[]
  blocked_by?: IssueRef[]
}
export type Comment = {
  id: string
  author: PersonRef | null
  body: Doc
  created_at: string
  edited_at: string | null
  can_edit: boolean
  can_delete: boolean
}
type StatusSnap = Pick<Status, 'id' | 'name' | 'category'>
type EventRef = Pick<IssueRef, 'id' | 'identifier' | 'title'>
type Change<T> = { from: T; to: T }
export type IssueEvent = { id: string; actor: PersonRef | null; created_at: string } & (
  | { kind: 'created' | 'description'; data: Record<string, never> }
  | { kind: 'title'; data: Change<string> }
  | { kind: 'status'; data: Change<StatusSnap> }
  | { kind: 'assignee'; data: Change<PersonRef | null> }
  | { kind: 'priority'; data: Change<number> }
  | { kind: 'due_date'; data: Change<string | null> }
  | { kind: 'labels'; data: { added: Label[]; removed: Label[] } }
  | { kind: 'parent'; data: Change<EventRef | null> }
  | { kind: 'link'; data: { type: 'blocks' | 'blocked_by'; issue: EventRef; removed: boolean } }
)
export type Timeline = { comments: Comment[]; events: IssueEvent[] }
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
// `signal` aborts the request (e.g. a search superseded by the next keystroke).
// A 401 outside /auth/* (except password change, which needs a session) refreshes the access token once and retries.
export async function api<T = void>(path: string, body?: FormData, method = body ? 'POST' : 'GET', signal?: AbortSignal): Promise<T> {
  let res = await send(path, body, method, signal)
  if (res.status === 401 && (!path.startsWith('/auth/') || path === '/auth/password/change')) {
    await refreshAccess()
    res = await send(path, body, method, signal)
  }
  const data = res.status === 204 ? undefined : await res.json().catch(() => undefined)
  if (!res.ok) throw new ApiError(res.status, data)
  return data as T
}

async function send(path: string, body?: FormData, method = body ? 'POST' : 'GET', signal?: AbortSignal) {
  try {
    return await fetch(`/api${path}`, { method, body, signal })
  } catch {
    throw new ApiError(0)
  }
}

// One refresh at a time per tab; concurrent 401s wait on the same request.
let refreshing: Promise<unknown> | null = null
export function refreshAccess() {
  refreshing ??= send('/auth/refresh', new FormData()).finally(() => (refreshing = null))
  return refreshing
}

export type UploadedFile = { id: string; url: string; name: string; content_type: string; size: number; image: boolean }

// Rejects with an Error whose message is ready to show ("Files must be 10 MB or smaller.").
export async function uploadFile(workspace: string, file: File) {
  const form = new FormData()
  form.set('file', file)
  try {
    return await api<UploadedFile>(`/workspaces/${encodeURIComponent(workspace)}/files`, form)
  } catch (e) {
    throw new Error(e instanceof ApiError ? (e.errors.file ?? e.error ?? FALLBACK) : FALLBACK)
  }
}

// The redirect is cached for 5 minutes; after changing their own avatar a user bumps it so this tab refetches.
const avatarVersions = new Map<string, number>()
export const avatarSrc = (userId: string) => `/api/users/${encodeURIComponent(userId)}/avatar${avatarVersions.has(userId) ? `?v=${avatarVersions.get(userId)}` : ''}`
export const bumpAvatar = (userId: string) => avatarVersions.set(userId, Date.now())

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
