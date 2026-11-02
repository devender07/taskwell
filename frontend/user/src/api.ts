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
// type: the workspace's own label for the category ("Reopen"); missing = the category's name. Only on /statuses.
export type Status = { id: string; name: string; category: StatusCategory; position: number; type?: string }
export type Label = { id: string; name: string; color: string }
export type CycleState = 'upcoming' | 'current' | 'past'
export type CycleRef = { id: string; number: number; name: string; starts_on: string; ends_on: string; state: CycleState }
export type Cycle = CycleRef & { custom_name: string | null; progress: { total: number; started: number; completed: number; canceled: number } }
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
  watching: boolean
  parent: IssueRef | null
  cycle: CycleRef | null
  sub_issues?: IssueRef[]
  blocks?: IssueRef[]
  blocked_by?: IssueRef[]
  pull_requests?: PullRequest[]
}
export type Provider = 'github' | 'gitlab'
export type PullRequest = {
  provider: Provider
  repo: string
  number: number
  url: string
  title: string
  branch: string
  author: string
  state: 'draft' | 'open' | 'merged' | 'closed'
  closes: boolean
  updated_at: string
}
export type Integration = {
  provider: Provider
  connected: boolean
  webhook_url: string | null
  open_status: Status | null
  merge_status: Status | null
  last_delivery_at: string | null
}
export type ApiToken = { id: string; name: string; created_at: string; last_used_at: string | null }
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
  | { kind: 'status'; data: Change<StatusSnap> & { via?: Pick<PullRequest, 'provider' | 'repo' | 'number' | 'url'> } }
  | { kind: 'assignee'; data: Change<PersonRef | null> }
  | { kind: 'priority'; data: Change<number> }
  | { kind: 'due_date'; data: Change<string | null> }
  | { kind: 'labels'; data: { added: Label[]; removed: Label[] } }
  | { kind: 'parent'; data: Change<EventRef | null> }
  | { kind: 'cycle'; data: Change<Pick<CycleRef, 'number' | 'name'> | null> }
  | { kind: 'link'; data: { type: 'blocks' | 'blocked_by'; issue: EventRef; removed: boolean } }
  | { kind: 'test_run'; data: { run_id: string; kind: RunKind; status: string; passed: number; failed: number; skipped: number; error: number } }
  | { kind: 'test_cases_regenerated'; data: { run_id: string } }
  | { kind: 'pr_linked' | 'pr_unlinked'; data: Pick<PullRequest, 'provider' | 'repo' | 'number' | 'title' | 'url'> }
)
// One page, newest first by page; each list oldest first. `?before=` an item's created_at fetches the page before it.
export type Timeline = { comments: Comment[]; events: IssueEvent[]; has_more: boolean }
export type ChatKind = 'workspace' | 'self' | 'direct' | 'group'
export type Chat = {
  id: string
  kind: ChatKind
  name: string
  members: PersonRef[] // direct: both; group: all; self: you; workspace: none (it's everyone)
  created_by: string | null
  unread: number
  mentioned: boolean
  last_message: { author: PersonRef | null; excerpt: string; created_at: string } | null
  last_message_at: string | null
}
export type Reaction = { emoji: string; count: number; mine: boolean; people: string[] }
export type Message = {
  id: string
  chat_id: string
  parent_id: string | null
  author: PersonRef | null
  body: Doc | null // null once deleted
  deleted: boolean
  created_at: string
  edited_at: string | null
  reply_count: number
  last_reply_at: string | null
  repliers: PersonRef[]
  reactions: Reaction[]
  can_edit: boolean
  can_delete: boolean
}
// Oldest first; `?before=` the first message's id fetches the page before it.
export type MessagePage = { messages: Message[]; has_more: boolean }
export type Replies = { root: Message; replies: Message[]; has_more: boolean }
export type Notification = {
  id: string
  actor: PersonRef | null
  data: { from?: StatusSnap; to?: StatusSnap; comment_id?: string; excerpt?: string; message_id?: string; via?: { provider: Provider } }
  read: boolean
  created_at: string
} & (
  | { kind: 'mention' | 'assigned' | 'status' | 'comment'; issue: { identifier: string; title: string; status: Status }; chat: null }
  | { kind: 'chat_mention' | 'message'; issue: null; chat: { id: string; kind: ChatKind; name: string } }
)
export type NotificationPage = { notifications: Notification[]; has_more: boolean }
type ActivityBase = { id: string; created_at: string; actor: PersonRef | null; issue: { identifier: string; title: string; status: Status } }
export type ActivityItem = ({ type: 'event' } & ActivityBase & IssueEvent) | ({ type: 'comment'; excerpt: string } & ActivityBase)
// Newest first; `?before=` the last item's created_at fetches the next page.
export type ActivityPage = { items: ActivityItem[]; has_more: boolean }
export type ProjectStats = { key: string; open: number; done: number; canceled: number; overdue: number }
export type NotificationSettings = {
  email: 'instant' | 'digest' | 'off'
  mentions: boolean
  assignments: boolean
  status: boolean
  comments: boolean
  chat: boolean
  timezone: string | null // null until chosen
}
export type FieldErrors = Record<string, string | null | undefined>

export const issuePath = (workspace: Workspace, identifier: string) =>
  `/workspaces/${encodeURIComponent(workspace.slug)}/issues/${encodeURIComponent(identifier)}`

// The caller's effective permission, for gating UI; the API enforces it.
export const can = (workspace: Workspace, key: string) => workspace.permissions.includes(key)

export const cyclesPath = (workspace: string, project: string) =>
  `/workspaces/${encodeURIComponent(workspace)}/projects/${encodeURIComponent(project)}/cycles`
const cyclePath = (workspace: string, project: string, number: number) => `${cyclesPath(workspace, project)}/${number}`

export const listCycles = (workspace: string, project: string, signal?: AbortSignal) => api<Cycle[]>(cyclesPath(workspace, project), undefined, 'GET', signal)
export const createCycle = (workspace: string, project: string, form: FormData) => api<Cycle>(cyclesPath(workspace, project), form)
export const updateCycle = (workspace: string, project: string, number: number, form: FormData) => api<Cycle>(cyclePath(workspace, project, number), form)
export const deleteCycle = (workspace: string, project: string, number: number) => api(cyclePath(workspace, project, number), undefined, 'DELETE')
export function rolloverCycle(workspace: string, project: string, number: number, to: number) {
  const form = new FormData()
  form.set('to', String(to))
  return api<{ moved: number; cycle: Cycle }>(`${cyclePath(workspace, project, number)}/rollover`, form)
}

export const integrationsPath = (workspace: string) => `/workspaces/${encodeURIComponent(workspace)}/integrations`
const integrationPath = (workspace: string, provider: Provider) => `${integrationsPath(workspace)}/${provider}`

export const connectIntegration = (workspace: string, provider: Provider) =>
  api<{ integration: Integration; secret: string }>(integrationPath(workspace, provider), new FormData())
export const updateIntegration = (workspace: string, provider: Provider, form: FormData) => api<Integration>(`${integrationPath(workspace, provider)}/settings`, form)
export const disconnectIntegration = (workspace: string, provider: Provider) => api(integrationPath(workspace, provider), undefined, 'DELETE')

export const tokensPath = '/me/tokens'
export const listTokens = () => api<ApiToken[]>(tokensPath)
export function createToken(name: string) {
  const form = new FormData()
  form.set('name', name)
  return api<{ token: ApiToken; secret: string }>(tokensPath, form)
}
export const deleteToken = (id: string) => api(`${tokensPath}/${encodeURIComponent(id)}`, undefined, 'DELETE')

const FALLBACK = 'Something went wrong. Try again.'
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

// GET when there is no body, POST the FormData otherwise (or pass `method`, e.g. 'DELETE'). 204 resolves to undefined.
// `signal` aborts the request (e.g. a search superseded by the next keystroke).
// A 401 outside /auth/* (except password change, which needs a session) refreshes the access token once and retries.
// A failed refresh in a signed-in tab means the session ended: it goes to sign-in and the call never settles.
export async function api<T = void>(path: string, body?: FormData, method = body ? 'POST' : 'GET', signal?: AbortSignal): Promise<T> {
  let res = await send(path, body, method, signal)
  if (res.status === 401 && (!path.startsWith('/auth/') || path === '/auth/password/change')) {
    if ((await refreshAccess()).status === 401 && signedIn) {
      sessionEnded()
      return new Promise<never>(() => {})
    }
    res = await send(path, body, method, signal)
  }
  const data = res.status === 204 ? undefined : await res.json().catch(() => undefined)
  if (!res.ok) throw new ApiError(res.status, data)
  return data as T
}

// The realtime connection id, so the API can tag this tab's own changes and the tab skips their events.
let client = ''
export const setClient = (id: string) => (client = id)

async function send(path: string, body?: FormData, method = body ? 'POST' : 'GET', signal?: AbortSignal) {
  try {
    return await fetch(`/api${path}`, { method, body, signal, headers: client ? { 'X-Tether-Client': client } : undefined })
  } catch {
    throw new ApiError(0)
  }
}

// One refresh at a time per tab; concurrent 401s wait on the same request.
let refreshing: Promise<Response> | null = null
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

// True once this tab has seen a session; a failed refresh or another tab's sign-out then sends it to sign-in.
let signedIn = false

// Tells other tabs about sign-in and sign-out: signed-out tabs reload into the app, signed-in ones go to sign-in.
const authChannel = typeof window !== 'undefined' && 'BroadcastChannel' in window ? new BroadcastChannel('tether-auth') : null
if (authChannel) {
  authChannel.onmessage = (e) => {
    if (e.data === 'signout') sessionEnded()
    else if (e.data === 'signin' && !signedIn) location.reload()
  }
}

// Full-page navigation to sign-in and back here; the reload also drops the socket and every cached page.
export function sessionEnded() {
  if (!signedIn) return
  signedIn = false
  location.assign(`/signin?redirect=${encodeURIComponent(location.pathname + location.search)}`)
}

export async function signOut() {
  await api('/auth/signout', new FormData())
  signedIn = false
  authChannel?.postMessage('signout')
}

export async function getMe(): Promise<User | null> {
  try {
    const user = await api<User>('/me')
    if (!signedIn) authChannel?.postMessage('signin')
    signedIn = true
    return user
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null
    throw e
  }
}

// An in-app path to return to after signing in. Anything else (another origin, "//x", "/\x", control characters
// that browsers strip, such as "/\t/x") is dropped.
export function safeRedirect(v: unknown): string | undefined {
  return typeof v === 'string' && v.startsWith('/') && !v.startsWith('//') && !/[\\\x00-\x1f\x7f]/.test(v) ? v : undefined
}

// beforeLoad for the sign-in pages. An unreachable API counts as signed out so the form can surface the error.
export async function redirectIfSignedIn({ search }: { search: { redirect?: string } }) {
  if (await getMe().catch(() => null)) throw redirect({ href: search.redirect ?? '/' })
}

// Any thrown value → { errors, error } for a form's state.
export function toFormErrors(e: unknown): { errors: FieldErrors; error: string | null } {
  return e instanceof ApiError ? { errors: e.errors, error: e.error } : { errors: {}, error: FALLBACK }
}

export type RunKind = 'ai' | 'manual' | 'replay'
export type TestStatus = 'pending' | 'running' | 'passed' | 'failed' | 'skipped' | 'error'
export type RunStatus = 'queued' | 'generating' | 'running' | 'passed' | 'failed' | 'error' | 'cancelled' | 'limit_reached'
export type TestCase = {
  id: string
  position: number
  title: string
  steps: string[]
  expected: string
  source: 'ai' | 'manual'
  updated_at: string
  last_result: { status: TestStatus; run_id: string; kind: RunKind; at: string } | null
  recorded: boolean
}
export type TestStep = { position: number; action: string; url: string | null; screenshot_url: string | null }
export type TestResult = {
  id: string
  case_id: string
  title: string
  steps: string[]
  expected: string
  status: TestStatus
  summary: string | null
  reproduction: string[] | null
  marked_by: PersonRef | null
  marked_at: string | null
  log: TestStep[] | null
}
export type TestRun = {
  id: string
  kind: RunKind
  issue: { identifier: string; title: string }
  status: RunStatus
  error: string | null
  started_by: PersonRef | null
  created_at: string
  finished_at: string | null
  cost_usd: number | null
  results: TestResult[]
  latest_screenshot_url: string | null
}
// The contract doesn't spell the summary out: the run without results, with per-status counts when the API sends them.
export type TestRunSummary = Omit<TestRun, 'results' | 'latest_screenshot_url'> & { counts?: Partial<Record<TestStatus, number>> }
export type TestSite = { id: string; base_url: string; host: string }
export type TestAccount = { id: string; label: string; username: string; password_set: boolean; site_id: string | null }
export type AiUsage = { spent_usd: number; limit_usd: number; resets_at: string }

export const workspacePath = (workspace: Workspace) => `/workspaces/${encodeURIComponent(workspace.slug)}`
export const testCasesPath = (workspace: Workspace, identifier: string) => `${issuePath(workspace, identifier)}/test-cases`
export const testRunsPath = (workspace: Workspace, identifier: string) => `${issuePath(workspace, identifier)}/test-runs`
export const testRunPath = (workspace: Workspace, id: string) => `${workspacePath(workspace)}/test-runs/${encodeURIComponent(id)}`
export const testSitesPath = (workspace: Workspace) => `${workspacePath(workspace)}/test-sites`
export const testAccountsPath = (workspace: Workspace) => `${workspacePath(workspace)}/test-accounts`

const caseForm = (c: Pick<TestCase, 'title' | 'steps' | 'expected'>) => {
  const form = new FormData()
  form.set('title', c.title)
  for (const s of c.steps) form.append('steps', s)
  form.set('expected', c.expected)
  return form
}

export const saveTestCase = (workspace: Workspace, identifier: string, c: Pick<TestCase, 'title' | 'steps' | 'expected'>, id?: string) =>
  api<TestCase>(id ? `${workspacePath(workspace)}/test-cases/${encodeURIComponent(id)}` : testCasesPath(workspace, identifier), caseForm(c))

export const deleteTestCase = (workspace: Workspace, id: string) => api(`${workspacePath(workspace)}/test-cases/${encodeURIComponent(id)}`, undefined, 'DELETE')

export function orderTestCases(workspace: Workspace, identifier: string, ids: string[]) {
  const form = new FormData()
  for (const id of ids) form.append('ids', id)
  return api<TestCase[]>(`${testCasesPath(workspace, identifier)}/order`, form)
}

export function startTestRun(workspace: Workspace, identifier: string, kind: RunKind, regenerate = false, caseId?: string) {
  const form = new FormData()
  form.set('kind', kind)
  if (regenerate) form.set('regenerate', 'true')
  if (caseId) form.set('case_id', caseId)
  return api<TestRun>(testRunsPath(workspace, identifier), form)
}

export const startRecordedRun = (workspace: Workspace, identifier: string) => startTestRun(workspace, identifier, 'replay')

export const playwrightURL = (workspace: Workspace, identifier: string) => `/api${issuePath(workspace, identifier)}/playwright`

export function markTestResult(workspace: Workspace, run: string, result: string, status: 'passed' | 'failed' | 'skipped', note: string) {
  const form = new FormData()
  form.set('status', status)
  form.set('note', note)
  return api<TestRun>(`${testRunPath(workspace, run)}/results/${encodeURIComponent(result)}`, form)
}

export const cancelTestRun = (workspace: Workspace, run: string) => api<TestRun>(`${testRunPath(workspace, run)}/cancel`, new FormData())
