import { Link, useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useCallback, useEffect, useMemo, useState, type KeyboardEvent } from 'react'
import { Avatar } from '@tether/shared/avatar'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { IconButton } from '@tether/shared/icon-button'
import { Popover } from '@tether/shared/popover'
import { RichText, type Doc } from '@tether/shared/rich-text'
import { RichEditor } from './rich-editor'
import { fullTime, relativeTime } from '@tether/shared/time'
import { commentBody } from '@tether/shared/validate'
import { MoreIcon } from '@tether/shared/icons'
import { usePageEnd } from './issue-pages'
import type { PeopleSearch } from '@/components/workspace/member-pages'
import { api, issuePath, avatarSrc, uploadFile, can, toFormErrors, type Comment, type FieldErrors, type Issue, type IssueEvent, type Project, type Timeline as TimelineData, type Workspace, workspacePath } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { eventActor, eventText } from './event-text'
import { ProviderIcon } from './pull-requests'
import { StatusIcon } from './fields'

type Mentions = PeopleSearch
type Draft = { doc: Doc | null; length: number }
type State = { errors?: FieldErrors; error?: string | null }

type TimelineProps = { workspace: Workspace; issue: Issue; project: Project; timeline: TimelineData; people: PeopleSearch }

type Page = { comments: Comment[]; events: IssueEvent[] }
const NONE: Page = { comments: [], events: [] }
const at = (x: { created_at: string }) => Date.parse(x.created_at)

// `timeline` is the newest page (the loader's, replaced on every realtime refetch); older pages load on demand and
// stay below it.
export function Timeline({ workspace, issue, project, timeline, people: mentions }: TimelineProps) {
  const [older, setOlder] = useState<Page>(NONE)
  const [olderMore, setOlderMore] = useState<boolean | null>(null) // has_more of the last older page; null before one loads
  const [newest, setNewest] = useState(timeline)
  if (timeline !== newest) {
    setNewest(timeline)
    if (!timeline.has_more) {
      setOlder(NONE)
      setOlderMore(null)
    } else if (older !== NONE) {
      // Enough new activity to push items off the newest page: keep them, so there's no gap before the older pages.
      const edge = Math.min(...[...timeline.comments, ...timeline.events].map(at))
      setOlder({
        comments: [...older.comments, ...newest.comments.filter((c) => at(c) < edge)],
        events: [...older.events, ...newest.events.filter((e) => at(e) < edge)],
      })
    }
  }

  const items = useMemo(() => {
    const ids = new Set([...timeline.comments, ...timeline.events].map((x) => x.id))
    const seen = (x: { id: string }) => !ids.has(x.id) && (ids.add(x.id), true)
    return [
      ...timeline.comments.map((c) => ({ at: c.created_at, comment: c, event: null })),
      ...timeline.events.map((e) => ({ at: e.created_at, comment: null, event: e })),
      ...older.comments.filter(seen).map((c) => ({ at: c.created_at, comment: c, event: null })),
      ...older.events.filter(seen).map((e) => ({ at: e.created_at, comment: null, event: e })),
    ].sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
  }, [timeline, older])

  // An edit or delete refetches only the newest page; a comment on an older one is patched here.
  const patchOlder = useCallback(
    (id: string, next: Comment | null) =>
      setOlder((o) => (o === NONE ? o : { ...o, comments: next ? o.comments.map((c) => (c.id === id ? next : c)) : o.comments.filter((c) => c.id !== id) })),
    [],
  )

  const [loadError, loadOlder, loading] = useActionState(async () => {
    const before = items.at(-1)?.at
    if (!before) return null
    try {
      const page = await api<TimelineData>(`${issuePath(workspace, issue.identifier)}/timeline?before=${encodeURIComponent(before)}`)
      setOlder((o) => ({ comments: [...page.comments, ...o.comments], events: [...page.events, ...o.events] }))
      setOlderMore(page.has_more)
    } catch (e) {
      return toFormErrors(e).error
    }
    return null
  }, null)
  const hasOlder = olderMore ?? timeline.has_more
  const end = usePageEnd<HTMLFormElement>(hasOlder && !loading && !loadError ? () => startTransition(() => loadOlder()) : null, 'timeline', items.length)

  return (
    <section className="grid gap-4" aria-labelledby="activity-heading">
      <h2 id="activity-heading" className="text-label">Activity</h2>
      {can(workspace, 'comments.create') && !project.archived ? <Composer workspace={workspace} issue={issue} mentions={mentions} /> : null}
      <ol className="grid gap-3 m-0 p-0" style={{ listStyle: 'none' }}>
        {items.map(({ comment, event }) =>
          comment ? (
            <li key={comment.id}>
              <CommentCard workspace={workspace} comment={comment} mentions={mentions} onChange={patchOlder} />
            </li>
          ) : event ? (
            <EventRow key={event.id} workspace={workspace.slug} event={event} />
          ) : null,
        )}
      </ol>
      {hasOlder ? (
        <form ref={end} action={loadOlder} className="grid gap-2" style={{ justifyItems: 'start' }}>
          {loading ? <p className="text-caption text-secondary m-0">Loading older activity…</p> : null}
          <FormError>{loadError}</FormError>
          {loadError ? (
            <Button type="submit" variant="ghost">
              Try again
            </Button>
          ) : null}
        </form>
      ) : null}
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

function EventRow({ workspace, event }: { workspace: string; event: IssueEvent }) {
  const text = eventText(event, (issue) => <IssueLink workspace={workspace} issue={issue} />)
  if (!text) return null
  const actor = eventActor(event)
  return (
    <li className="flex items-start gap-2 text-caption text-secondary" style={{ paddingInline: 16 }}>
      <span className="inline-grid place-center flex-noshrink" style={{ width: 20, height: 20 }}>
        {event.kind === 'status' ? (
          <StatusIcon status={event.data.to} />
        ) : event.kind === 'pr_linked' || event.kind === 'pr_unlinked' ? (
          <ProviderIcon provider={event.data.provider} />
        ) : (
          <Avatar name={actor} size={20} src={event.actor && avatarSrc(event.actor.id)} />
        )}
      </span>
      <span style={{ minWidth: 0, overflowWrap: 'anywhere' }}>
        <span className="text-primary">{actor}</span> {text} · <Time at={event.created_at} />
      </span>
    </li>
  )
}

type CommentCardProps = { workspace: Workspace; comment: Comment; mentions: Mentions; onChange: (id: string, next: Comment | null) => void }

function CommentCard({ workspace, comment, mentions, onChange }: CommentCardProps) {
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
                <IconButton aria-label="Comment actions" tooltip="Comment actions" style={{ width: 44, height: 44 }} {...props}>
                  <MoreIcon size={20} />
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
        <EditComment
          workspace={workspace}
          comment={comment}
          mentions={mentions}
          onDone={(saved) => {
            if (saved) onChange(comment.id, saved)
            setEditing(false)
          }}
        />
      ) : (
        <RichText aria-label={`Comment by ${author}`} doc={comment.body} />
      )}
      {comment.can_delete ? <DeleteComment workspace={workspace} comment={comment} open={deleting} onClose={() => setDeleting(false)} onDeleted={() => onChange(comment.id, null)} /> : null}
    </article>
  )
}

const commentPath = (workspace: Workspace, id: string) => `${workspacePath(workspace.slug)}/comments/${encodeURIComponent(id)}`

function useSaveComment(path: string, draft: Draft, done: (saved: Comment) => void) {
  const router = useRouter()
  return useActionState(async (): Promise<State> => {
    const body = commentBody(!draft.doc, draft.length)
    if (body) return { errors: { body } }
    const form = new FormData()
    form.set('body', JSON.stringify(draft.doc))
    let saved: Comment
    try {
      saved = await api<Comment>(path, form)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadPage(router)
    done(saved)
    return {}
  }, {})
}

// Asks before the tab closes while `unsaved` (the browser shows its own wording).
export function useUnsavedPrompt(unsaved: boolean) {
  useEffect(() => {
    if (!unsaved) return
    const ask = (e: BeforeUnloadEvent) => e.preventDefault()
    addEventListener('beforeunload', ask)
    return () => removeEventListener('beforeunload', ask)
  }, [unsaved])
}

function Composer({ workspace, issue, mentions }: { workspace: Workspace; issue: Issue; mentions: Mentions }) {
  const [draft, setDraft] = useState<Draft>({ doc: null, length: 0 })
  const [round, setRound] = useState(0)
  useUnsavedPrompt(!!draft.doc)
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

function EditComment({ workspace, comment, mentions, onDone }: { workspace: Workspace; comment: Comment; mentions: Mentions; onDone: (saved?: Comment) => void }) {
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
        <Button variant="ghost" onClick={() => onDone()} disabled={pending}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          {pending ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </form>
  )
}

type DeleteCommentProps = { workspace: Workspace; comment: Comment; open: boolean; onClose: () => void; onDeleted: () => void }

function DeleteComment({ workspace, comment, open, onClose, onDeleted }: DeleteCommentProps) {
  const router = useRouter()
  const [error, remove, pending] = useActionState(async () => {
    try {
      await api(commentPath(workspace, comment.id), undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    await reloadPage(router)
    onDeleted()
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
