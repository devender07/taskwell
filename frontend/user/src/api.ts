import { redirect } from '@tanstack/react-router'
import type { Doc } from '@taskwell/shared/rich-editor'
import { ApiError, FALLBACK } from '@taskwell/shared/api-errors'
import { apiUrl } from '@taskwell/shared/api-url'

export { ApiError, safeRedirect, toFormErrors, type FieldErrors } from '@taskwell/shared/api-errors'

export type User = { id: string; name: string; email: string; has_password: boolean; avatar_url: string | null; can_create_workspaces: boolean }
export type RoleRef = { id: string; name: string }
// guest: the caller is a guest here (sees only the projects they're added to). Optional: only the single-workspace GET promises it.
export type Workspace = { id: string; name: string; slug: string; role: RoleRef; permissions: string[]; guest?: boolean }
export type Member = { id: string; name: string; email: string; role: RoleRef; guest: boolean }
export type Invite = { id: string; email: string; invited_by: string; expires_at: string; role: RoleRef }
export type Role = { id: string; name: string; builtin: 'admin' | 'member' | 'guest' | null; permissions: string[]; member_count: number }
export type Permission = { key: string; group: string; label: string; active: boolean }
export type Project = { id: string; name: string; key: string; description: string; archived: boolean; private: boolean; manual_order: boolean; members: PersonRef[]; created_at: string; start_date: string | null; target_date: string | null; health: Health | null }
// The latest project update's health.
export type Health = 'on_track' | 'at_risk' | 'off_track'
export type Milestone = { id: string; name: string; date: string }
export type PersonRef = { id: string; name: string }
export type StatusCategory = 'backlog' | 'unstarted' | 'started' | 'completed' | 'canceled'
// icon/color: its custom type's look (null = the category's icon). type: the custom type, only on /statuses.
export type Status = {
  id: string
  name: string
  category: StatusCategory
  position: number
  icon: string | null
  color: string | null
  type?: { id: string; name: string } | null
}
export type StatusType = { id: string; name: string; category: StatusCategory; icon: string; color: string | null; status_count: number }
export type Label = { id: string; name: string; color: string }
export type CycleState = 'upcoming' | 'current' | 'past'
export type CycleRef = { id: string; number: number; name: string; starts_on: string; ends_on: string; state: CycleState }
export type RoadmapProject = Pick<Project, 'key' | 'name' | 'private' | 'start_date' | 'target_date' | 'health'> & {
  progress: { total: number; completed: number; canceled: number }
  milestones: Milestone[]
  cycles: Omit<CycleRef, 'id'>[]
}
export type Cycle = CycleRef & { custom_name: string | null; progress: { total: number; started: number; completed: number; canceled: number } }
export type IssueRef = { id: string; identifier: string; title: string; status: Status; assignee: PersonRef | null }
export type SearchIssue = IssueRef & { project: { key: string; name: string }; match: 'title' | 'description' | 'identifier' | null }
export type SearchResult = { issues: SearchIssue[]; projects: Pick<Project, 'key' | 'name' | 'archived'>[] }
export type Issue = {
  id: string
  identifier: string
  number: number
  rank: string
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
  watchers?: PersonRef[]
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
type StatusSnap = Pick<Status, 'id' | 'name' | 'category'> & Partial<Pick<Status, 'icon' | 'color'>>
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
export type ChatKind = 'workspace' | 'direct' | 'group'
export type Chat = {
  id: string
  kind: ChatKind
  name: string
  members: PersonRef[] // direct: both; group: all; workspace: none (it's everyone)
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
  pinned_until: string | null
  pinned_by: PersonRef | null
}
// Oldest first; `?before=` the first message's id fetches the page before it. `pinned` is the chat's pins, newest first.
export type MessagePage = { messages: Message[]; has_more: boolean; pinned: Message[] }
export type Replies = { root: Message; replies: Message[]; has_more: boolean }
export type Notification = {
  id: string
  actor: PersonRef | null
  data: { from?: StatusSnap; to?: StatusSnap; comment_id?: string; excerpt?: string; message_id?: string; via?: { provider: Provider } }
  read: boolean
  created_at: string
} & (
  | { kind: 'mention' | 'assigned' | 'status' | 'comment'; issue: { identifier: string; title: string; status: Status }; chat: null; project: null }
  | { kind: 'chat_mention' | 'message'; issue: null; chat: { id: string; kind: ChatKind; name: string }; project: null }
  | { kind: 'project_added'; issue: null; chat: null; project: { key: string; name: string } }
)
export type NotificationPage = { notifications: Notification[]; has_more: boolean }
type ActivityBase = { id: string; created_at: string; actor: PersonRef | null; issue: { identifier: string; title: string; status: Status } }
export type ActivityItem =
  | ({ type: 'event' } & ActivityBase & IssueEvent)
  | ({ type: 'comment'; excerpt: string } & ActivityBase)
  | { type: 'project_update'; id: string; created_at: string; actor: PersonRef | null; project: { key: string; name: string }; health: Health }
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

export type FeedbackState = 'new' | 'linked' | 'archived'
// issue: null when unlinked, or when it's in a private project the caller can't see.
export type Feedback = {
  id: string
  body: string
  email: string | null
  name: string | null
  source: 'form' | 'member'
  issue: { identifier: string; title: string; status: Pick<Status, 'name' | 'color' | 'category'> } | null
  archived: boolean
  created_by: PersonRef | null
  created_at: string
}
// Newest first; `&before=` the last item's id fetches the next page.
export type FeedbackPage = { items: Feedback[]; has_more: boolean; total?: number } // total: only on an issue's feedback
export type FeedbackForm = { enabled: boolean; token: string | null }

export const feedbackPath = (workspace: string) => `${workspacePath(workspace)}/feedback`
export const feedbackListPath = (workspace: string, state: FeedbackState, q: string) => `${feedbackPath(workspace)}?${new URLSearchParams({ state, q })}`
export const feedbackFormPath = (workspace: string) => `${workspacePath(workspace)}/feedback-form`
export const updateFeedback = (workspace: string, id: string, fields: Record<string, string>) => {
  const form = new FormData()
  for (const [k, v] of Object.entries(fields)) form.set(k, v)
  return api<Feedback>(`${feedbackPath(workspace)}/${encodeURIComponent(id)}`, form)
}
export const deleteFeedback = (workspace: string, id: string) => api(`${feedbackPath(workspace)}/${encodeURIComponent(id)}`, undefined, 'DELETE')

export const issuePath = (workspace: Workspace, identifier: string) =>
  `${workspacePath(workspace.slug)}/issues/${encodeURIComponent(identifier)}`
export const bulkIssuesPath = (workspace: string) => `${workspacePath(workspace)}/issues/bulk`

// Newest deleted first, 50 a page; `?before=` the last item's deleted_at fetches the next.
export type TrashedIssue = {
  id: string
  identifier: string
  title: string
  project: { key: string; name: string }
  deleted_at: string
  deleted_by: PersonRef | null
  can_restore: boolean
}
export type TrashPage = { issues: TrashedIssue[]; has_more: boolean }
export const trashPath = (workspace: string) => `${workspacePath(workspace)}/trash`
const trashedPath = (workspace: string, identifier: string) => `${trashPath(workspace)}/${encodeURIComponent(identifier)}`
export const restoreIssue = (workspace: string, identifier: string) => api<Issue>(`${trashedPath(workspace, identifier)}/restore`, new FormData())
export const purgeIssue = (workspace: string, identifier: string) => api(trashedPath(workspace, identifier), undefined, 'DELETE')
export const emptyTrash = (workspace: string) => api(trashPath(workspace), undefined, 'DELETE')

// path: the page under the workspace with its search, e.g. `/projects/ABC?view=board`.
export type SavedView = { id: string; name: string; path: string; shared: boolean; owner: PersonRef; can_edit: boolean }
export const viewsPath = (workspace: string) => `${workspacePath(workspace)}/views`
export const viewPath = (workspace: string, id: string) => `${viewsPath(workspace)}/${encodeURIComponent(id)}`

// pinned, archived and labels are the caller's own; members is everyone but the owner. can_delete: the caller owns it;
// can_edit: the caller owns it or is an 'edit' member (a viewer can still pin, archive and label it).
export type NoteRole = 'edit' | 'view'
export type Note = {
  id: string
  title: string
  body: Doc | null
  color: string
  owner: PersonRef
  members: (PersonRef & { role: NoteRole })[]
  pinned: boolean
  archived: boolean
  labels: string[]
  can_delete: boolean
  can_edit: boolean
  created_at: string
  updated_at: string
  updated_by: PersonRef | null
}
export const notesPath = (workspace: string) => `${workspacePath(workspace)}/notes`
const notePath = (workspace: string, id: string) => `${notesPath(workspace)}/${encodeURIComponent(id)}`
// Only the fields given change; pinned/archived are '1' or '0', labels a JSON array, body a doc's JSON or ''.
export function saveNote(workspace: string, fields: Record<string, string>, id?: string) {
  const form = new FormData()
  for (const [k, v] of Object.entries(fields)) form.set(k, v)
  return api<Note>(id ? notePath(workspace, id) : notesPath(workspace), form)
}
// The owner deletes it for everyone; anyone else leaves it.
export const deleteNote = (workspace: string, id: string) => api(notePath(workspace, id), undefined, 'DELETE')
export function addNoteMember(workspace: string, id: string, userId: string, role: NoteRole) {
  const form = new FormData()
  form.set('user_id', userId)
  form.set('role', role)
  return api<Note>(`${notePath(workspace, id)}/members`, form)
}
// Owner only.
export function setNoteMemberRole(workspace: string, id: string, userId: string, role: NoteRole) {
  const form = new FormData()
  form.set('role', role)
  return api<Note>(`${notePath(workspace, id)}/members/${encodeURIComponent(userId)}`, form)
}
export const removeNoteMember = (workspace: string, id: string, userId: string) =>
  api(`${notePath(workspace, id)}/members/${encodeURIComponent(userId)}`, undefined, 'DELETE')

export type ImportSource = 'linear' | 'jira' | 'csv' | 'github'
// imported and first: only once imported, not on a dry run. row: the CSV data row (1-based) or the GitHub issue number.
export type ImportSummary = {
  source: ImportSource
  total: number
  statuses: { from: string; to: { id: string; name: string }; count: number }[]
  new_labels: string[]
  unmatched_assignees: string[]
  comments: number
  skipped: { row: number; error: string }[]
  imported?: number
  first?: string
}

// The caller's effective permission, for gating UI; the API enforces it.
export const can = (workspace: Workspace, key: string) => workspace.permissions.includes(key)

export type AnalyticsWeek = { week: string; completed: number; created: number; comments: number }
export type MemberAnalytics = {
  member: Member
  open: { total: number; by_category: { backlog: number; unstarted: number; started: number }; overdue: number; due_this_week: number }
  weeks: AnalyticsWeek[]
  cycle_time_days: number | null
  completed_30d: number
  tests: { runs_started_30d: number }
  current_cycles: { project: { key: string; name: string }; number: number; name: string; assigned: number; completed: number }[]
}
export type TeamMember = { id: string; name: string; open: number; overdue: number; completed_30d: number; cycle_time_days: number | null }
export const memberAnalyticsPath = (workspace: string, id: string) => `${workspacePath(workspace)}/members/${encodeURIComponent(id)}/analytics`
export const teamAnalyticsPath = (workspace: string) => `${workspacePath(workspace)}/analytics/team`
// Your own analytics always; anyone else's with analytics.view.
export const canViewMember = (workspace: Workspace, id: string, me: string) => id === me || can(workspace, 'analytics.view')

export const projectPath = (workspace: string, project: string) => `${workspacePath(workspace)}/projects/${encodeURIComponent(project)}`
// Everyone who can see the project, for its assignee and mention pickers: the whole workspace for a public project.
export const projectPeoplePath = (workspace: string, project: string) => `${projectPath(workspace, project)}/people`
export function addProjectMembers(workspace: string, project: string, ids: string[]) {
  const form = new FormData()
  for (const id of ids) form.append('user_ids', id)
  return api<Project>(`${projectPath(workspace, project)}/members`, form)
}
export const removeProjectMember = (workspace: string, project: string, userId: string) =>
  api<Project>(`${projectPath(workspace, project)}/members/${encodeURIComponent(userId)}`, undefined, 'DELETE')

export type ProjectUpdate = { id: string; health: Health; body: Doc; author: PersonRef | null; created_at: string; edited_at: string | null; can_edit: boolean }
// Newest first, 20 a page; `?before=` the last update's created_at fetches the next page.
export type ProjectUpdatePage = { updates: ProjectUpdate[]; has_more: boolean }
export const projectUpdatesPath = (workspace: string, project: string) => `${projectPath(workspace, project)}/updates`
export const projectUpdatePath = (workspace: string, id: string) => `${workspacePath(workspace)}/project-updates/${encodeURIComponent(id)}`

export type RepeatUnit = 'day' | 'week' | 'month'
export type Template = {
  id: string
  name: string
  title: string
  description: Doc | null
  project: { id: string; key: string; name: string } | null
  status: Status | null
  assignee: PersonRef | null
  priority: number
  labels: Label[]
  repeat: { every: number; unit: RepeatUnit; next_on: string; paused: boolean } | null
  can_edit: boolean
}
export const templatesPath = (workspace: string) => `${workspacePath(workspace)}/templates`
export const templatePath = (workspace: string, id: string) => `${templatesPath(workspace)}/${encodeURIComponent(id)}`

export const roadmapPath = (workspace: string) => `${workspacePath(workspace)}/roadmap`
export const milestonesPath = (workspace: string, project: string) => `${projectPath(workspace, project)}/milestones`
const milestonePath = (workspace: string, project: string, id: string) => `${milestonesPath(workspace, project)}/${encodeURIComponent(id)}`
export const listMilestones = (workspace: string, project: string) => api<Milestone[]>(milestonesPath(workspace, project))
export const saveMilestone = (workspace: string, project: string, form: FormData, id?: string) =>
  api<Milestone>(id ? milestonePath(workspace, project, id) : milestonesPath(workspace, project), form)
export const deleteMilestone = (workspace: string, project: string, id: string) => api(milestonePath(workspace, project, id), undefined, 'DELETE')

export const cyclesPath = (workspace: string, project: string) => `${projectPath(workspace, project)}/cycles`
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

export const statusTypesPath = (workspace: string) => `${workspacePath(workspace)}/status-types`
const statusTypePath = (workspace: string, id: string) => `${statusTypesPath(workspace)}/${encodeURIComponent(id)}`
export const saveStatusType = (workspace: string, form: FormData, id?: string) => api<StatusType>(id ? statusTypePath(workspace, id) : statusTypesPath(workspace), form)
export const deleteStatusType = (workspace: string, id: string) => api(statusTypePath(workspace, id), undefined, 'DELETE')

export const integrationsPath = (workspace: string) => `${workspacePath(workspace)}/integrations`
const integrationPath = (workspace: string, provider: Provider) => `${integrationsPath(workspace)}/${provider}`

export const connectIntegration = (workspace: string, provider: Provider) =>
  api<{ integration: Integration; secret: string }>(integrationPath(workspace, provider), new FormData())
export const updateIntegration = (workspace: string, provider: Provider, form: FormData) => api<Integration>(`${integrationPath(workspace, provider)}/settings`, form)
export const disconnectIntegration = (workspace: string, provider: Provider) => api(integrationPath(workspace, provider), undefined, 'DELETE')

export type WebhookEvent = 'issue.created' | 'issue.updated' | 'issue.deleted' | 'comment.created'
export type DeliveryStatus = 'pending' | 'delivered' | 'failed'
export type Webhook = {
  id: string
  url: string
  events: WebhookEvent[]
  active: boolean
  created_at: string
  last_delivery: { status: DeliveryStatus; response_code: number | null; created_at: string } | null
}
export type WebhookDelivery = {
  id: string
  event: WebhookEvent | 'ping'
  status: DeliveryStatus
  attempts: number
  response_code: number | null
  response_error: string | null
  created_at: string
  delivered_at: string | null
  next_at: string
  payload: unknown
}

export const webhooksPath = (workspace: string) => `${workspacePath(workspace)}/webhooks`
export const webhookPath = (workspace: string, id: string) => `${webhooksPath(workspace)}/${encodeURIComponent(id)}`
export const deliveriesPath = (workspace: string, id: string) => `${webhookPath(workspace, id)}/deliveries`
export const createWebhook = (workspace: string, form: FormData) => api<{ webhook: Webhook; secret: string }>(webhooksPath(workspace), form)
export const updateWebhook = (workspace: string, id: string, form: FormData) => api<Webhook>(webhookPath(workspace, id), form)
export const rollWebhookSecret = (workspace: string, id: string) => api<{ secret: string }>(`${webhookPath(workspace, id)}/secret`, new FormData())
export const testWebhook = (workspace: string, id: string) => api(`${webhookPath(workspace, id)}/test`, new FormData())
export const deleteWebhook = (workspace: string, id: string) => api(webhookPath(workspace, id), undefined, 'DELETE')
export const redeliver = (workspace: string, id: string, delivery: string) =>
  api(`${deliveriesPath(workspace, id)}/${encodeURIComponent(delivery)}/redeliver`, new FormData())

export type MfaStatus = { enabled: boolean; recovery_left: number }
export const mfaPath = '/me/mfa'

export const tokensPath = '/me/tokens'
export function createToken(name: string) {
  const form = new FormData()
  form.set('name', name)
  return api<{ token: ApiToken; secret: string }>(tokensPath, form)
}
export const deleteToken = (id: string) => api(`${tokensPath}/${encodeURIComponent(id)}`, undefined, 'DELETE')

// The /auth/ calls that need a session (they sit under /auth/ for the refresh cookie), so a 401 refreshes like any other.
const SIGNED_IN_AUTH = ['/auth/password/change', '/auth/email/code', '/auth/email/change']

// GET when there is no body, POST the FormData otherwise (or pass `method`, e.g. 'DELETE'). 204 resolves to undefined.
// `signal` aborts the request (e.g. a search superseded by the next keystroke).
// A 401 outside /auth/* (except SIGNED_IN_AUTH) refreshes the access token once and retries.
// A failed refresh in a signed-in tab means the session ended: it goes to sign-in and the call never settles.
export async function api<T = void>(path: string, body?: FormData, method = body ? 'POST' : 'GET', signal?: AbortSignal): Promise<T> {
  let res = await send(path, body, method, signal)
  if (res.status === 401 && (!path.startsWith('/auth/') || SIGNED_IN_AUTH.includes(path))) {
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
    return await fetch(apiUrl(`/api${path}`), { method, body, signal, credentials: 'include', headers: client ? { 'X-Taskwell-Client': client } : undefined })
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
    return await api<UploadedFile>(`${workspacePath(workspace)}/files`, form)
  } catch (e) {
    throw new Error(e instanceof ApiError ? (e.errors.file ?? e.error ?? FALLBACK) : FALLBACK)
  }
}

// The redirect is cached for 5 minutes; after changing their own avatar a user bumps it so this tab refetches.
const avatarVersions = new Map<string, number>()
export const avatarSrc = (userId: string) => `/api/users/${encodeURIComponent(userId)}/avatar${avatarVersions.has(userId) ? `?v=${avatarVersions.get(userId)}` : ''}`
export const bumpAvatar = (userId: string) => avatarVersions.set(userId, Date.now())

// Where AI tools connect: the MCP server, beside the API.
export const mcpUrl = (): string => import.meta.env.VITE_MCP_URL ?? `${location.origin}/mcp`

// True once this tab has seen a session; a failed refresh or another tab's sign-out then sends it to sign-in.
let signedIn = false

// Tells other tabs about sign-in and sign-out: signed-out tabs reload into the app, signed-in ones go to sign-in.
const authChannel = typeof window !== 'undefined' && 'BroadcastChannel' in window ? new BroadcastChannel('taskwell-auth') : null
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

// beforeLoad for the sign-in pages. An unreachable API counts as signed out so the form can surface the error.
export async function redirectIfSignedIn({ search }: { search: { redirect?: string } }) {
  if (await getMe().catch(() => null)) throw redirect({ href: search.redirect ?? (await homePath()) })
}

// Where a signed-in user starts: their most recently opened workspace, or creating one when they have none.
export async function homePath() {
  const [last] = await api<Workspace[]>('/workspaces')
  return last ? `/${encodeURIComponent(last.slug)}` : '/new-workspace'
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

export const workspacePath = (workspace: Workspace | string) => `/workspaces/${encodeURIComponent(typeof workspace === 'string' ? workspace : workspace.slug)}`
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

export function markTestResult(workspace: Workspace, run: string, result: string, status: 'passed' | 'failed' | 'skipped', note: string) {
  const form = new FormData()
  form.set('status', status)
  form.set('note', note)
  return api<TestRun>(`${testRunPath(workspace, run)}/results/${encodeURIComponent(result)}`, form)
}

export const cancelTestRun = (workspace: Workspace, run: string) => api<TestRun>(`${testRunPath(workspace, run)}/cancel`, new FormData())
