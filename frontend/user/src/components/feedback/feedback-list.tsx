import { Link } from '@tanstack/react-router'
import { startTransition, useActionState, useCallback, useLayoutEffect, useRef, useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { Input } from '@tether/shared/input'
import { Popover } from '@tether/shared/popover'
import { SmartAutocomplete } from '@tether/shared/smart-autocomplete'
import { Textarea } from '@tether/shared/textarea'
import { fullTime, relativeTime } from '@tether/shared/time'
import { feedbackBody, feedbackEmail, feedbackName } from '@tether/shared/validate'
import {
  api,
  can,
  deleteFeedback,
  feedbackListPath,
  feedbackPath,
  toFormErrors,
  updateFeedback,
  workspacePath,
  type Feedback,
  type FeedbackPage,
  type FeedbackState,
  type FieldErrors,
  type Workspace,
} from '@/api'
import { refetchPaths } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { useShortcuts } from '@/components/app/shortcuts'
import { StatusIcon } from '@/components/issues/fields'
import { usePageEnd } from '@/components/issues/issue-pages'
import { refOption, searchIssues, subscribeIssues } from '@/components/issues/relations'
import { ConfirmButton } from '@/components/tests/tests'
import { feedbackPrefill, feedbackWho } from './feedback'
import '@/components/issues/issues.css'

// This tab's own writes skip their realtime events, so it refetches the list, the sidebar count and the issues' sections itself.
export const feedbackChanged = (slug: string, issues: (string | null | undefined)[] = []) =>
  refetchPaths([
    feedbackPath(slug),
    `${feedbackPath(slug)}/count`,
    ...issues.filter((i): i is string => !!i).map((i) => `${workspacePath(slug)}/issues/${encodeURIComponent(i)}/feedback`),
  ])

// Page one comes from the loader; scrolling the end into view appends the page before the last item. A new page one
// (a reload after a change) starts the list over.
export function useFeedbackPages(path: string, first: FeedbackPage) {
  const [extra, setExtra] = useState<{ base: FeedbackPage; items: Feedback[]; has_more: boolean; error?: string | null } | null>(null)
  const busy = useRef(false)
  const more = extra?.base === first ? extra : null
  const ids = new Set(first.items.map((f) => f.id))
  const items = [...first.items, ...(more?.items.filter((f) => !ids.has(f.id)) ?? [])]
  const hasMore = more ? more.has_more : first.has_more
  const last = items.at(-1)?.id

  const load = useCallback(async () => {
    if (busy.current || !last) return
    busy.current = true
    const prev = more?.items ?? []
    try {
      const next = await api<FeedbackPage>(`${path}${path.includes('?') ? '&' : '?'}before=${encodeURIComponent(last)}`)
      setExtra({ base: first, items: [...prev, ...next.items], has_more: next.has_more })
    } catch (e) {
      setExtra({ base: first, items: prev, has_more: false, error: toFormErrors(e).error })
    }
    busy.current = false
  }, [path, first, more, last])

  const end = usePageEnd<HTMLDivElement>(hasMore ? load : null, 'feedback', items.length)
  return { items, hasMore, end, error: more?.error }
}

const EMPTY: Record<FeedbackState, string> = { new: 'No new feedback', linked: 'No linked feedback', archived: 'No archived feedback' }

export function FeedbackList({ workspace, state, q, page }: { workspace: Workspace; state: FeedbackState; q: string; page: FeedbackPage }) {
  const { items, hasMore, end, error } = useFeedbackPages(feedbackListPath(workspace.slug, state, q), page)

  if (items.length === 0) {
    return (
      <section className="grid gap-2 p-6 bg-surface border rounded-lg justify-items-center text-center">
        <p className="text-body-sm text-secondary m-0">{q ? `No feedback matches “${q}”.` : EMPTY[state]}</p>
        {state === 'new' && !q && can(workspace, 'feedback.manage') ? (
          <p className="text-caption text-secondary m-0">
            Share a public form with customers in{' '}
            <Link to="/$workspace/settings/feedback" params={{ workspace: workspace.slug }} style={{ color: 'var(--link)' }}>
              Settings → Feedback form
            </Link>
            .
          </p>
        ) : null}
      </section>
    )
  }
  return (
    <div className="grid gap-3">
      <ul className="grid gap-3 m-0 p-0" style={{ listStyle: 'none' }}>
        {items.map((f) => (
          <FeedbackCard key={f.id} workspace={workspace} state={state} feedback={f} />
        ))}
      </ul>
      <FormError>{error}</FormError>
      {hasMore && !error ? (
        <div ref={end} className="text-caption text-secondary text-center py-2">
          Loading more…
        </div>
      ) : null}
    </div>
  )
}

function FeedbackCard({ workspace, state, feedback: f }: { workspace: Workspace; state: FeedbackState; feedback: Feedback }) {
  const { createPrefilled } = useShortcuts()
  const manage = can(workspace, 'feedback.manage')
  const slug = workspace.slug
  const bodyRef = useRef<HTMLParagraphElement>(null)
  const [expanded, setExpanded] = useState(false)
  const [clamped, setClamped] = useState(false)

  useLayoutEffect(() => {
    const el = bodyRef.current
    if (el && !expanded) setClamped(el.scrollHeight > el.clientHeight + 1)
  }, [f.body, expanded])

  const [error, act, pending] = useActionState(async (_prev: string | null, fields: Record<string, string>) => {
    try {
      await updateFeedback(slug, f.id, fields)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return errors.issue ?? error
    }
    feedbackChanged(slug, [f.issue?.identifier, fields.issue])
    return null
  }, null)
  const run = (fields: Record<string, string>) => startTransition(() => act(fields))

  return (
    <li className="grid gap-3 p-4 bg-surface border rounded-lg">
      <div className="flex flex-wrap items-center gap-2 text-caption text-secondary">
        <span className="text-label text-primary">{feedbackWho(f)}</span>
        {f.name && f.email ? <span>{f.email}</span> : null}
        <span aria-hidden>·</span>
        <time dateTime={f.created_at} title={fullTime(f.created_at)}>
          {relativeTime(f.created_at)}
        </time>
        {f.source === 'form' ? (
          <span className="px-2" style={{ border: '1px solid rgba(255, 255, 255, 0.08)', borderRadius: 9999 }}>
            Form
          </span>
        ) : null}
      </div>
      <div className="grid gap-1" style={{ justifyItems: 'start' }}>
        <p
          ref={bodyRef}
          className="text-body-sm m-0"
          style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', ...(expanded ? {} : { display: '-webkit-box', WebkitLineClamp: 6, WebkitBoxOrient: 'vertical', overflow: 'hidden' }) }}
        >
          {f.body}
        </p>
        {clamped || expanded ? (
          <Button variant="ghost" onClick={() => setExpanded((e) => !e)}>
            {expanded ? 'Show less' : 'Show more'}
          </Button>
        ) : null}
      </div>
      {f.issue ? (
        <Link
          to="/$workspace/issue/$identifier"
          params={{ workspace: slug, identifier: f.issue.identifier }}
          className="flex items-center gap-2 text-body-sm"
          style={{ minWidth: 0, justifySelf: 'start', maxWidth: '100%' }}
        >
          <StatusIcon status={f.issue.status} label={f.issue.status.name} />
          <span className="issue-id flex-noshrink">{f.issue.identifier}</span>
          <span className="text-primary ellipsis">{f.issue.title}</span>
        </Link>
      ) : state === 'linked' ? (
        <p className="text-caption text-secondary m-0">Linked to an issue you can't see.</p>
      ) : null}
      <FormError>{error}</FormError>
      <div className="flex flex-wrap items-center gap-2">
        {state === 'new' ? (
          <>
            <LinkIssue workspace={workspace} disabled={pending} onPick={(identifier) => run({ issue: identifier })} />
            {can(workspace, 'issues.create') ? (
              <Button variant="secondary" disabled={pending} onClick={() => createPrefilled(feedbackPrefill(f))}>
                Create issue
              </Button>
            ) : null}
          </>
        ) : null}
        {state === 'linked' ? (
          <Button variant="ghost" disabled={pending} onClick={() => run({ issue: '' })}>
            Unlink
          </Button>
        ) : null}
        {manage ? (
          <>
            <Button variant="ghost" disabled={pending} onClick={() => run({ archived: String(!f.archived) })}>
              {f.archived ? 'Unarchive' : 'Archive'}
            </Button>
            <ConfirmButton
              label="Delete"
              title="Delete this feedback?"
              description="This can't be undone."
              confirm="Delete feedback"
              pending="Deleting…"
              onConfirm={async () => {
                await deleteFeedback(slug, f.id)
                feedbackChanged(slug, [f.issue?.identifier])
              }}
            />
          </>
        ) : null}
      </div>
    </li>
  )
}

function LinkIssue({ workspace, disabled, onPick }: { workspace: Workspace; disabled: boolean; onPick: (identifier: string) => void }) {
  return (
    <Popover
      aria-label="Link to issue"
      trigger={(props) => (
        <Button variant="secondary" disabled={disabled} {...props}>
          Link to issue
        </Button>
      )}
    >
      {(close) => (
        <div className="p-3" style={{ width: 320, maxWidth: '100%' }}>
          <SmartAutocomplete
            aria-label="Issue"
            placeholder="Search issues"
            loadOptions={async (q, signal) => (await searchIssues(workspace, '')(q, signal)).map(refOption)}
            subscribe={subscribeIssues(workspace)}
            value={null}
            onChange={(o) => {
              if (!o) return
              close()
              onPick(o.value)
            }}
          />
        </div>
      )}
    </Popover>
  )
}

type AddState = { errors?: FieldErrors; error?: string | null }

export function AddFeedbackDialog({ workspace, open, onClose }: { workspace: string; open: boolean; onClose: () => void }) {
  const [form, setForm] = useState({ body: '', email: '', name: '' })
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }))

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<AddState> => {
    const errors = { body: feedbackBody(form.body), email: feedbackEmail(form.email, false), name: feedbackName(form.name) }
    if (Object.values(errors).some(Boolean)) return { errors }
    const data = new FormData()
    data.set('body', form.body.trim())
    data.set('email', form.email.trim())
    data.set('name', form.name.trim())
    try {
      await api<Feedback>(feedbackPath(workspace), data)
    } catch (e) {
      return toFormErrors(e)
    }
    setForm({ body: '', email: '', name: '' })
    feedbackChanged(workspace)
    onClose()
    return {}
  }, {})

  return (
    <Dialog open={open} onClose={onClose} title="Add feedback" description="Paste what a customer asked for, from a call, an email or a ticket.">
      <form action={submit} noValidate className="grid gap-4">
        <Textarea label="Message" rows={6} autoFocus value={form.body} onChange={set('body')} error={errors?.body} />
        <Input label="Customer email" type="email" autoComplete="off" value={form.email} onChange={set('email')} error={errors?.email} />
        <Input label="Customer name" autoComplete="off" value={form.name} onChange={set('name')} error={errors?.name} />
        <FormError>{error}</FormError>
        <Button type="submit" className="w-full" disabled={pending}>
          {pending ? 'Adding…' : 'Add feedback'}
        </Button>
      </form>
    </Dialog>
  )
}
