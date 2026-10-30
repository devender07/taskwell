import { Link, useRouter } from '@tanstack/react-router'
import { useActionState, useMemo, useState, type KeyboardEvent, type ReactNode } from 'react'
import { Avatar } from '@tether/shared/avatar'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { IconButton } from '@tether/shared/icon-button'
import { Popover } from '@tether/shared/popover'
import { RichEditor, type Doc } from '@tether/shared/rich-editor'
import { fullTime, relativeTime } from '@tether/shared/time'
import { commentBody } from '@tether/shared/validate'
import { api, avatarSrc, uploadFile, can, toFormErrors, type Comment, type FieldErrors, type Issue, type IssueEvent, type Member, type Project, type Timeline as TimelineData, type Workspace } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { StatusIcon, formatDate, priorityLabel } from './fields'
import { issuePath } from './issue-form'

type Mentions = { id: string; name: string }[]
type Draft = { doc: Doc | null; length: number }
type State = { errors?: FieldErrors; error?: string | null }

type TimelineProps = { workspace: Workspace; issue: Issue; project: Project; timeline: TimelineData; members: Member[] }

// ponytail: refreshed on navigation and after own actions; others' live edits don't show until then. Push over the collab socket if that matters.
export function Timeline({ workspace, issue, project, timeline, members }: TimelineProps) {
  const mentions = useMemo(() => members.map((m) => ({ id: m.id, name: m.name })), [members])
  const items = useMemo(
    () =>
      [
        ...timeline.comments.map((c) => ({ at: c.created_at, comment: c, event: null })),
        ...timeline.events.map((e) => ({ at: e.created_at, comment: null, event: e })),
      ].sort((a, b) => Date.parse(a.at) - Date.parse(b.at)),
    [timeline],
  )

  return (
    <section className="grid gap-4" aria-labelledby="activity-heading">
      <h2 id="activity-heading" className="text-label">Activity</h2>
      <ol className="grid gap-3 m-0 p-0" style={{ listStyle: 'none' }}>
        {items.map(({ comment, event }) =>
          comment ? (
            <li key={comment.id}>
              <CommentCard workspace={workspace} comment={comment} mentions={mentions} />
            </li>
          ) : event ? (
            <EventRow key={event.id} workspace={workspace.slug} event={event} />
          ) : null,
        )}
      </ol>
      {can(workspace, 'comments.create') && !project.archived ? <Composer workspace={workspace} issue={issue} mentions={mentions} /> : null}
    </section>
  )
}

function Time({ at }: { at: string }) {
  return (
    <time dateTime={at} title={fullTime(at)}>
      {relativeTime(at)}
    </time>
  )
}

// ⌘/Ctrl+Enter submits; caught before the editor so it doesn't insert a line break.
function submitOnModEnter(e: KeyboardEvent<HTMLFormElement>) {
  if (e.key !== 'Enter' || !(e.metaKey || e.ctrlKey)) return
  e.preventDefault()
  e.stopPropagation()
  e.currentTarget.requestSubmit()
}

function IssueLink({ workspace, issue }: { workspace: string; issue: { identifier: string; title: string } }) {
  return (
    <Link to="/$workspace/issue/$identifier" params={{ workspace, identifier: issue.identifier }} title={issue.title} className="text-primary">
      {issue.identifier}
    </Link>
  )
}

function describe(workspace: string, e: IssueEvent): ReactNode {
  const names = (ls: { name: string }[]) => ls.map((l) => l.name).join(', ')
  switch (e.kind) {
    case 'created':
      return 'created the issue'
    case 'description':
      return 'edited the description'
    case 'title':
      return `changed the title to “${e.data.to}”`
    case 'status':
      return `moved from ${e.data.from.name} to ${e.data.to.name}`
    case 'assignee': {
      const { from, to } = e.data
      if (!to) return `unassigned ${from?.name ?? 'the issue'}`
      return to.id === e.actor?.id ? 'assigned to themselves' : `assigned to ${to.name}`
    }
    case 'priority':
      return e.data.to === 0 ? 'removed priority' : `set priority to ${priorityLabel(e.data.to)}`
    case 'due_date':
      return e.data.to ? `set the due date to ${formatDate(e.data.to)}` : 'removed the due date'
    case 'labels':
      return [e.data.added.length ? `added ${names(e.data.added)}` : '', e.data.removed.length ? `removed ${names(e.data.removed)}` : '']
        .filter(Boolean)
        .join(', ')
    case 'parent':
      return e.data.to ? (
        <>made this a sub-issue of <IssueLink workspace={workspace} issue={e.data.to} /></>
      ) : e.data.from ? (
        <>removed the parent <IssueLink workspace={workspace} issue={e.data.from} /></>
      ) : null
    case 'link':
      return (
        <>
          {e.data.removed ? 'removed the link to' : e.data.type === 'blocks' ? 'marked this as blocking' : 'marked this as blocked by'}{' '}
          <IssueLink workspace={workspace} issue={e.data.issue} />
        </>
      )
    default:
      return null
  }
}

function EventRow({ workspace, event }: { workspace: string; event: IssueEvent }) {
  const text = describe(workspace, event)
  if (!text) return null
  const actor = event.actor?.name ?? 'Someone'
  return (
    <li className="flex items-start gap-2 text-caption text-secondary" style={{ paddingInline: 16 }}>
      <span className="inline-grid place-center flex-noshrink" style={{ width: 20, height: 20 }}>
        {event.kind === 'status' ? <StatusIcon category={event.data.to.category} /> : <Avatar name={actor} size={20} src={event.actor && avatarSrc(event.actor.id)} />}
      </span>
      <span style={{ minWidth: 0, overflowWrap: 'anywhere' }}>
        <span className="text-primary">{actor}</span> {text} · <Time at={event.created_at} />
      </span>
    </li>
  )
}

function CommentCard({ workspace, comment, mentions }: { workspace: Workspace; comment: Comment; mentions: Mentions }) {
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const author = comment.author?.name ?? 'Someone'

  return (
    <article className="grid gap-2 p-4 bg-surface border rounded-lg" aria-label={`Comment by ${author}`}>
      <header className="flex items-center gap-2" style={{ minHeight: 40 }}>
        <Avatar name={author} size={24} src={comment.author && avatarSrc(comment.author.id)} />
        <span className="text-label text-primary ellipsis">{author}</span>
        <span className="text-caption text-secondary flex-noshrink">
          <Time at={comment.created_at} />
          {comment.edited_at ? <span title={fullTime(comment.edited_at)}> (edited)</span> : null}
        </span>
        {!editing && (comment.can_edit || comment.can_delete) ? (
          <span className="flex-noshrink" style={{ marginLeft: 'auto' }}>
            <Popover
              aria-label="Comment actions"
              placement="bottom-end"
              trigger={(props) => (
                <IconButton aria-label="Comment actions" style={{ width: 44, height: 44 }} {...props}>
                  <svg width={20} height={20} viewBox="0 0 24 24" fill="currentColor" aria-hidden>
                    <circle cx={5} cy={12} r={1.75} />
                    <circle cx={12} cy={12} r={1.75} />
                    <circle cx={19} cy={12} r={1.75} />
                  </svg>
                </IconButton>
              )}
            >
              {(close) => (
                <div style={{ width: 160 }}>
                  {comment.can_edit ? (
                    <button type="button" className="popover-item" style={{ height: 44 }} onClick={() => { close(); setEditing(true) }}>
                      Edit
                    </button>
                  ) : null}
                  {comment.can_delete ? (
                    <button type="button" className="popover-item" style={{ height: 44 }} onClick={() => { close(); setDeleting(true) }}>
                      Delete
                    </button>
                  ) : null}
                </div>
              )}
            </Popover>
          </span>
        ) : null}
      </header>
      {editing ? (
        <EditComment workspace={workspace} comment={comment} mentions={mentions} onDone={() => setEditing(false)} />
      ) : (
        <RichEditor key={comment.edited_at ?? ''} aria-label={`Comment by ${author}`} content={comment.body} editable={false} />
      )}
      {comment.can_delete ? <DeleteComment workspace={workspace} comment={comment} open={deleting} onClose={() => setDeleting(false)} /> : null}
    </article>
  )
}

const commentPath = (workspace: Workspace, id: string) => `/workspaces/${encodeURIComponent(workspace.slug)}/comments/${encodeURIComponent(id)}`

function useSaveComment(path: string, draft: Draft, done: () => void) {
  const router = useRouter()
  return useActionState(async (): Promise<State> => {
    const body = commentBody(!draft.doc, draft.length)
    if (body) return { errors: { body } }
    const form = new FormData()
    form.set('body', JSON.stringify(draft.doc))
    try {
      await api<Comment>(path, form)
    } catch (e) {
      return toFormErrors(e)
    }
    await router.invalidate()
    done()
    return {}
  }, {})
}

function Composer({ workspace, issue, mentions }: { workspace: Workspace; issue: Issue; mentions: Mentions }) {
  const [draft, setDraft] = useState<Draft>({ doc: null, length: 0 })
  const [round, setRound] = useState(0)
  const [{ errors, error }, submit, pending] = useSaveComment(`${issuePath(workspace, issue.identifier)}/comments`, draft, () => {
    setDraft({ doc: null, length: 0 })
    setRound((n) => n + 1)
  })

  return (
    <form action={submit} noValidate onKeyDownCapture={submitOnModEnter} className="grid gap-3">
      <RichEditor
        key={round}
        aria-label="Comment"
        placeholder="Leave a comment… Type @ to mention someone"
        mentions={mentions}
        onChange={(doc, length) => setDraft({ doc, length })}
        error={errors?.body}
        upload={(file) => uploadFile(workspace.slug, file)}
      />
      <FormError>{error}</FormError>
      <Button type="submit" style={{ justifySelf: 'end' }} disabled={pending || !draft.doc}>
        {pending ? 'Commenting…' : 'Comment'}
      </Button>
    </form>
  )
}

function EditComment({ workspace, comment, mentions, onDone }: { workspace: Workspace; comment: Comment; mentions: Mentions; onDone: () => void }) {
  const [draft, setDraft] = useState<Draft>({ doc: comment.body, length: 0 })
  const [{ errors, error }, submit, pending] = useSaveComment(commentPath(workspace, comment.id), draft, onDone)

  return (
    <form action={submit} noValidate onKeyDownCapture={submitOnModEnter} className="grid gap-3">
      <RichEditor
        aria-label="Edit comment"
        content={comment.body}
        mentions={mentions}
        onChange={(doc, length) => setDraft({ doc, length })}
        error={errors?.body}
        upload={(file) => uploadFile(workspace.slug, file)}
      />
      <FormError>{error}</FormError>
      <div className="flex items-center justify-end gap-3">
        <Button variant="ghost" onClick={onDone} disabled={pending}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          {pending ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </form>
  )
}

function DeleteComment({ workspace, comment, open, onClose }: { workspace: Workspace; comment: Comment; open: boolean; onClose: () => void }) {
  const router = useRouter()
  const [error, remove, pending] = useActionState(async () => {
    try {
      await api(commentPath(workspace, comment.id), undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    await router.invalidate()
    onClose()
    return null
  }, null)

  return (
    <Dialog open={open} onClose={onClose} title="Delete comment?" description="This can't be undone.">
      <form action={remove} className="grid gap-4">
        <FormError>{error}</FormError>
        <Button type="submit" variant="danger" className="w-full" disabled={pending}>
          {pending ? 'Deleting…' : 'Delete comment'}
        </Button>
      </form>
    </Dialog>
  )
}
