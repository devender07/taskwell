import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { IconButton } from '@taskwell/shared/icon-button'
import { MoreIcon } from '@taskwell/shared/icons'
import { Popover } from '@taskwell/shared/popover'
import { RichText, type Doc } from '@taskwell/shared/rich-text'
import { Tabs } from '@taskwell/shared/tabs'
import { fullTime, relativeTime } from '@taskwell/shared/time'
import { projectUpdateBody } from '@taskwell/shared/validate'
import { api, avatarSrc, projectUpdatePath, projectUpdatesPath, toFormErrors, uploadFile, type FieldErrors, type Health, type Project, type ProjectUpdate, type ProjectUpdatePage, type Workspace } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { RichEditor } from '@/components/issues/rich-editor'
import { peopleSearch } from '@/components/workspace/member-pages'
import './projects.css'
import { idSet, extractDataFromArray } from '@taskwell/shared/list'

export const HEALTHS: { value: Health; label: string }[] = [
  { value: 'on_track', label: 'On track' },
  { value: 'at_risk', label: 'At risk' },
  { value: 'off_track', label: 'Off track' },
]
export const healthLabel = (h: Health) => extractDataFromArray(HEALTHS.find((x) => x.value === h), 'label', h)

export function HealthPill({ health }: { health: Health | null }) {
  if (!health) return null
  return (
    <span className="health-pill flex-noshrink" data-health={health}>
      <span className="health-dot" aria-hidden />
      {healthLabel(health)}
    </span>
  )
}

const SHOWN = 3

type State = { errors?: FieldErrors; error?: string | null }

// `page` is the newest page (the loader's, refetched live); "Show older" reveals more and fetches older pages.
export function ProjectUpdates({ workspace, project, page, canPost }: { workspace: Workspace; project: Project; page: ProjectUpdatePage; canPost: boolean }) {
  const [older, setOlder] = useState<ProjectUpdate[]>([])
  const [olderMore, setOlderMore] = useState<boolean | null>(null)
  const [shown, setShown] = useState(SHOWN)
  const [posting, setPosting] = useState(false)
  const ids = idSet(page.updates)
  const updates = [...page.updates, ...older.filter((u) => !ids.has(u.id))]
  const more = olderMore ?? page.has_more

  const [loadError, showOlder, loading] = useActionState(async () => {
    const next = shown + 10
    const before = updates.at(-1)?.created_at
    if (updates.length < next && more && before) {
      try {
        const p = await api<ProjectUpdatePage>(`${projectUpdatesPath(workspace.slug, project.key)}?before=${encodeURIComponent(before)}`)
        setOlder((o) => [...o, ...p.updates])
        setOlderMore(p.has_more)
      } catch (e) {
        return toFormErrors(e).error
      }
    }
    setShown(next)
    return null
  }, null)

  // An edit or delete refetches the newest page; an update from an older page is patched here.
  const patch = (id: string, next: ProjectUpdate | null) => setOlder((o) => (next ? o.map((u) => (u.id === id ? next : u)) : o.filter((u) => u.id !== id)))

  return (
    <section className="grid gap-4" aria-labelledby="updates-heading">
      <div className="flex items-center justify-between gap-4">
        <h2 id="updates-heading" className="text-label">Updates</h2>
        {canPost && !posting ? (
          <Button variant="secondary" onClick={() => setPosting(true)}>
            Post update
          </Button>
        ) : null}
      </div>
      {posting ? <UpdateForm workspace={workspace} path={projectUpdatesPath(workspace.slug, project.key)} project={project.key} health={project.health ?? 'on_track'} onDone={() => setPosting(false)} /> : null}
      {updates.length === 0 && !posting ? <p className="text-body-sm text-secondary m-0">No updates yet.</p> : null}
      {updates.length ? (
        <ol className="grid gap-3 m-0 p-0" style={{ listStyle: 'none' }}>
          {updates.slice(0, shown).map((u) => (
            <li key={u.id}>
              <UpdateCard workspace={workspace} project={project.key} update={u} onChange={patch} />
            </li>
          ))}
        </ol>
      ) : null}
      {updates.length > shown || more ? (
        <form action={showOlder} className="grid gap-2" style={{ justifyItems: 'start' }}>
          <FormError>{loadError}</FormError>
          <Button type="submit" variant="ghost" disabled={loading}>
            {loading ? 'Loading…' : 'Show older'}
          </Button>
        </form>
      ) : null}
    </section>
  )
}

function UpdateCard({ workspace, project, update, onChange }: { workspace: Workspace; project: string; update: ProjectUpdate; onChange: (id: string, next: ProjectUpdate | null) => void }) {
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const author = extractDataFromArray(update.author, 'name', 'Someone')

  return (
    <article className="grid gap-2 p-4 bg-surface border rounded-lg" aria-label={`Update by ${author}`} style={{ minWidth: 0 }}>
      <header className="flex items-center gap-2" style={{ minHeight: 44, minWidth: 0 }}>
        <Avatar name={author} size={24} src={update.author && avatarSrc(update.author.id)} />
        <span className="text-label text-primary ellipsis">{author}</span>
        <span className="text-caption text-secondary flex-noshrink">
          <time dateTime={update.created_at} title={fullTime(update.created_at)}>
            {relativeTime(update.created_at)}
          </time>
          {update.edited_at ? <span title={fullTime(update.edited_at)}> (edited)</span> : null}
        </span>
        <HealthPill health={update.health} />
        {update.can_edit && !editing ? (
          <span className="flex-noshrink" style={{ marginLeft: 'auto' }}>
            <Popover
              aria-label="Update actions"
              placement="bottom-end"
              trigger={(props) => (
                <IconButton aria-label="Update actions" tooltip="Update actions" style={{ width: 44, height: 44 }} {...props}>
                  <MoreIcon size={20} />
                </IconButton>
              )}
            >
              {(close) => (
                <div style={{ width: 160 }}>
                  <button type="button" className="popover-item" style={{ height: 44 }} onClick={() => { close(); setEditing(true) }}>
                    Edit
                  </button>
                  <button type="button" className="popover-item" style={{ height: 44 }} onClick={() => { close(); setDeleting(true) }}>
                    Delete
                  </button>
                </div>
              )}
            </Popover>
          </span>
        ) : null}
      </header>
      {editing ? (
        <UpdateForm
          workspace={workspace}
          path={projectUpdatePath(workspace.slug, update.id)}
          project={project}
          health={update.health}
          body={update.body}
          onDone={(saved) => {
            if (saved) onChange(update.id, saved)
            setEditing(false)
          }}
        />
      ) : (
        <RichText aria-label={`Update by ${author}`} doc={update.body} />
      )}
      {update.can_edit ? <DeleteUpdate workspace={workspace} update={update} open={deleting} onClose={() => setDeleting(false)} onDeleted={() => onChange(update.id, null)} /> : null}
    </article>
  )
}

type FormProps = { workspace: Workspace; path: string; project: string; health: Health; body?: Doc; onDone: (saved?: ProjectUpdate) => void }

// Posts a new update (no `body`) or edits one.
function UpdateForm({ workspace, path, project, health: initial, body, onDone }: FormProps) {
  const router = useRouter()
  const [health, setHealth] = useState<Health>(initial)
  const [draft, setDraft] = useState<{ doc: Doc | null; length: number }>({ doc: body ?? null, length: 0 })

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const message = projectUpdateBody(!draft.doc, draft.length)
    if (message) return { errors: { body: message } }
    const form = new FormData()
    form.set('health', health)
    form.set('body', JSON.stringify(draft.doc))
    let saved: ProjectUpdate
    try {
      saved = await api<ProjectUpdate>(path, form)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadPage(router)
    onDone(saved)
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-3" style={{ minWidth: 0 }}>
      <div className="grid gap-2">
        <Tabs aria-label="Health" className="health-tabs w-fit" items={HEALTHS} value={health} onChange={setHealth} />
        <FormError>{errors?.health}</FormError>
      </div>
      <RichEditor
        aria-label="Update"
        placeholder="What changed, what's next, what's blocked… Type @ to mention someone"
        content={body}
        mentions={peopleSearch(workspace.slug, project)}
        minHeight={120}
        onChange={(doc, length) => setDraft({ doc, length })}
        error={errors?.body}
        upload={(file, progress) => uploadFile(workspace.slug, file, undefined, undefined, progress)}
      />
      <FormError>{error}</FormError>
      <div className="flex items-center justify-end gap-3">
        <Button variant="ghost" onClick={() => onDone()} disabled={pending}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          {body ? (pending ? 'Saving…' : 'Save') : pending ? 'Posting…' : 'Post update'}
        </Button>
      </div>
    </form>
  )
}

function DeleteUpdate({ workspace, update, open, onClose, onDeleted }: { workspace: Workspace; update: ProjectUpdate; open: boolean; onClose: () => void; onDeleted: () => void }) {
  const router = useRouter()
  const [error, remove, pending] = useActionState(async () => {
    try {
      await api(projectUpdatePath(workspace.slug, update.id), undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    await reloadPage(router)
    onDeleted()
    onClose()
    return null
  }, null)

  return (
    <Dialog open={open} onClose={onClose} title="Delete update?" description="This can't be undone.">
      <form action={remove} className="grid gap-4">
        <FormError>{error}</FormError>
        <Button type="submit" variant="danger" className="w-full" disabled={pending}>
          {pending ? 'Deleting…' : 'Delete update'}
        </Button>
      </form>
    </Dialog>
  )
}
