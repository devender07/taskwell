import { useRouter } from '@tanstack/react-router'
import { useEffect, useState, type FormEvent } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { SWATCHES } from '@taskwell/shared/color-picker'
import { Dialog } from '@taskwell/shared/dialog'
import { IconButton } from '@taskwell/shared/icon-button'
import { ArchiveIcon, MoreIcon, PaletteIcon, PinIcon, TagIcon, UserPlusIcon, XIcon } from '@taskwell/shared/icons'
import { Input } from '@taskwell/shared/input'
import { Popover } from '@taskwell/shared/popover'
import { SmartAutocomplete } from '@taskwell/shared/smart-autocomplete'
import { useToast } from '@taskwell/shared/toast'
import { noteLabel } from '@taskwell/shared/validate'
import { addNoteMember, avatarSrc, deleteNote, removeNoteMember, saveNote, setNoteMemberRole, toFormErrors, type Member, type Note, type NoteRole } from '@/api'
import { reloadPage } from '@/realtime'
import { searchMembers } from '@/components/workspace/member-pages'
import { FormError } from '@/components/auth/auth-card'

const NAMES = ['Indigo', 'Pink', 'Green', 'Orange', 'Yellow', 'Teal', 'Purple', 'Gray']
export const COLORS = [{ value: '', label: 'Default' }, ...SWATCHES.map((s, i) => ({ value: s, label: NAMES[i] ?? s }))]

// Focus in the note popup: the editor (caret at the end) once there's a title, else the title.
export function focusNote(hasTitle: boolean) {
  requestAnimationFrame(() => {
    const el = document.querySelector<HTMLElement>(hasTitle ? '.note-sheet [contenteditable="true"]' : '.dialog-full .note-heading-title')
    if (!el) return
    el.focus()
    if (hasTitle) {
      getSelection()?.selectAllChildren(el)
      getSelection()?.collapseToEnd()
    }
  })
}

// Enter in the title moves on to the editor.
export const titleKeyDown = (e: { key: string; preventDefault: () => void }) => {
  if (e.key !== 'Enter') return
  e.preventDefault()
  focusNote(true)
}

export const tint = (color: string) => (color ? `color-mix(in srgb, ${color} 16%, var(--surface))` : undefined)

const message = (e: unknown) => {
  const { error, errors } = toFormErrors(e)
  return error ?? Object.values(errors).find(Boolean) ?? 'Something went wrong. Try again.'
}

// Runs a note change, then reloads the page (this tab's own changes don't come back over realtime). Errors show as a toast.
export function useNoteAction() {
  const router = useRouter()
  const { show } = useToast()
  return async (run: () => Promise<unknown>, done?: { message: string; undo?: () => Promise<unknown> }) => {
    try {
      await run()
    } catch (e) {
      show({ message: message(e) })
      return false
    }
    await reloadPage(router)
    if (done) show({ message: done.message, action: done.undo && { label: 'Undo', run: () => void done.undo!().then(() => reloadPage(router)) } })
    return true
  }
}

type ToolsProps = { slug: string; note: Note; labels: string[]; me: string; onGone?: () => void }

const ROLES = [
  { value: 'edit', label: 'Can edit' },
  { value: 'view', label: 'Can view' },
]
const roleOption = (role: NoteRole) => ROLES.find((r) => r.value === role)!

// Pin, color, labels, share, archive and more, on a card and in the note dialog. Viewers get no color or share.
export function NoteTools({ slug, note, labels, me, onGone }: ToolsProps) {
  const act = useNoteAction()
  const [dialog, setDialog] = useState<'share' | 'delete' | null>(null)
  const save = (fields: Record<string, string>) => saveNote(slug, fields, note.id)

  return (
    <div className="note-tools flex flex-wrap items-center" role="toolbar" aria-label="Note actions">
      <IconButton
        className="note-tool"
        aria-label={note.pinned ? 'Unpin' : 'Pin'}
        tooltip={note.pinned ? 'Unpin' : 'Pin'}
        aria-pressed={note.pinned}
        onClick={() => act(() => save(note.pinned ? { pinned: '0' } : { pinned: '1', archived: '0' }))}
      >
        <PinIcon />
      </IconButton>
      {note.can_edit ? (
        <Popover
          aria-label="Color"
          trigger={(p) => (
            <IconButton {...p} className="note-tool" aria-label="Color" tooltip="Color" aria-haspopup="dialog">
              <PaletteIcon />
            </IconButton>
          )}
        >
          {(close) => (
            <div className="note-swatches p-2">
              {COLORS.map((c) => (
                <button
                  key={c.value}
                  type="button"
                  className="note-swatch"
                  aria-label={c.label}
                  title={c.label}
                  aria-pressed={note.color === c.value}
                  style={{ background: c.value || 'var(--surface)' }}
                  onClick={() => {
                    close()
                    if (c.value !== note.color) act(() => save({ color: c.value }))
                  }}
                />
              ))}
            </div>
          )}
        </Popover>
      ) : null}
      <Popover
        aria-label="Labels"
        trigger={(p) => (
          <IconButton {...p} className="note-tool" aria-label="Labels" tooltip="Labels" aria-haspopup="dialog">
            <TagIcon />
          </IconButton>
        )}
      >
        <LabelsPanel note={note} labels={labels} onSave={(next) => act(() => save({ labels: JSON.stringify(next) }))} />
      </Popover>
      {note.can_edit ? (
        <IconButton className="note-tool" aria-label="Share" tooltip="Share" onClick={() => setDialog('share')}>
          <UserPlusIcon />
        </IconButton>
      ) : null}
      <IconButton
        className="note-tool"
        aria-label={note.archived ? 'Unarchive' : 'Archive'}
        tooltip={note.archived ? 'Unarchive' : 'Archive'}
        onClick={() =>
          act(() => save({ archived: note.archived ? '0' : '1' }), {
            message: note.archived ? 'Note unarchived' : 'Note archived',
            undo: () => save({ archived: note.archived ? '1' : '0' }),
          })
        }
      >
        <ArchiveIcon />
      </IconButton>
      <Popover
        aria-label="More"
        placement="bottom-end"
        trigger={(p) => (
          <IconButton {...p} className="note-tool" aria-label="More" tooltip="More" aria-haspopup="dialog">
            <MoreIcon />
          </IconButton>
        )}
      >
        {(close) => (
          <div className="grid p-1" style={{ minWidth: 200 }}>
            {note.can_delete ? (
              <button type="button" className="popover-item" style={{ color: 'var(--danger)' }} onClick={() => (close(), setDialog('delete'))}>
                Delete note
              </button>
            ) : (
              <button
                type="button"
                className="popover-item"
                onClick={async () => {
                  close()
                  if (await act(() => deleteNote(slug, note.id), { message: `Removed from your notes` })) onGone?.()
                }}
              >
                Remove from my notes
              </button>
            )}
          </div>
        )}
      </Popover>
      {dialog === 'share' && note.can_edit ? <ShareDialog slug={slug} note={note} me={me} onClose={() => setDialog(null)} onGone={onGone} /> : null}
      <DeleteNote slug={slug} note={note} open={dialog === 'delete'} onClose={() => setDialog(null)} onGone={onGone} />
    </div>
  )
}

function LabelsPanel({ note, labels, onSave }: { note: Note; labels: string[]; onSave: (labels: string[]) => void }) {
  const [draft, setDraft] = useState('')
  const [error, setError] = useState<string | null>(null)
  const has = (l: string) => note.labels.some((x) => x.toLowerCase() === l.toLowerCase())

  function create(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    e.stopPropagation() // the note dialog's form is its React parent
    const name = draft.trim()
    if (has(name) && name) return setDraft('')
    const err = noteLabel(name, note.labels)
    setError(err)
    if (err) return
    onSave([...note.labels, labels.find((l) => l.toLowerCase() === name.toLowerCase()) ?? name])
    setDraft('')
  }

  return (
    <div className="grid gap-3 p-3" style={{ width: 240 }}>
      <span className="text-label text-secondary">Label note</span>
      {labels.length ? (
        <div className="grid gap-2" style={{ maxHeight: 240, overflowY: 'auto' }}>
          {labels.map((l) => (
            <Checkbox
              key={l}
              label={l}
              checked={has(l)}
              onChange={() => onSave(has(l) ? note.labels.filter((x) => x.toLowerCase() !== l.toLowerCase()) : [...note.labels, l])}
            />
          ))}
        </div>
      ) : null}
      <form noValidate onSubmit={create} className="grid gap-2">
        <Input aria-label="Create label" placeholder="Create label" value={draft} onChange={(e) => setDraft(e.target.value)} error={error} />
      </form>
    </div>
  )
}

function DeleteNote({ slug, note, open, onClose, onGone }: { slug: string; note: Note; open: boolean; onClose: () => void; onGone?: () => void }) {
  const router = useRouter()
  const [state, setState] = useState<{ pending?: boolean; error?: string | null }>({})
  async function remove() {
    setState({ pending: true })
    try {
      await deleteNote(slug, note.id)
    } catch (e) {
      setState({ error: toFormErrors(e).error })
      return
    }
    onClose()
    onGone?.()
    await reloadPage(router)
  }
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Delete note?"
      description={note.members.length ? "It's deleted for everyone it's shared with. This can't be undone." : "This can't be undone."}
    >
      <FormError>{state.error}</FormError>
      <div className="flex justify-end gap-3">
        <Button variant="ghost" onClick={onClose} disabled={state.pending}>
          Cancel
        </Button>
        <Button variant="danger" onClick={remove} disabled={state.pending}>
          {state.pending ? 'Deleting…' : 'Delete note'}
        </Button>
      </div>
    </Dialog>
  )
}

// The owner, then everyone it's shared with. The owner can remove anyone and change roles; others only leave.
// Only editors open it, and they can add people.
function ShareDialog({ slug, note, me, onClose, onGone }: { slug: string; note: Note; me: string; onClose: () => void; onGone?: () => void }) {
  const router = useRouter()
  const [q, setQ] = useState('')
  const [found, setFound] = useState<Member[]>([])
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [role, setRole] = useState<NoteRole>('edit')
  const owner = note.owner.id === me
  const on = new Set([note.owner.id, ...note.members.map((m) => m.id)])

  useEffect(() => {
    if (!q.trim()) return setFound([])
    const ctl = new AbortController()
    searchMembers(slug, q.trim(), 8, ctl.signal).then(
      (list) => setFound(list.filter((m) => !m.guest)),
      () => {},
    )
    return () => ctl.abort()
  }, [slug, q])

  async function run(id: string, call: () => Promise<unknown>, after?: () => void) {
    setBusy(id)
    setError(null)
    try {
      await call()
    } catch (e) {
      setError(message(e))
      setBusy(null)
      return
    }
    setBusy(null)
    after?.()
    await reloadPage(router)
  }

  return (
    <Dialog open onClose={onClose} title="Share note" description="People who can view see changes live but can't edit.">
      <ul className="grid gap-1 m-0 p-0" style={{ listStyle: 'none' }}>
        <li className="flex items-center gap-3" style={{ minHeight: 44 }}>
          <Avatar name={note.owner.name} size={28} src={avatarSrc(note.owner.id)} />
          <span className="text-body-sm ellipsis flex-grow">{note.owner.name}</span>
          <span className="text-caption text-secondary">Owner</span>
        </li>
        {note.members.map((m) => (
          <li key={m.id} className="flex items-center gap-3" style={{ minHeight: 44 }}>
            <Avatar name={m.name} size={28} src={avatarSrc(m.id)} />
            <span className="text-body-sm ellipsis flex-grow">{m.id === me ? `${m.name} (you)` : m.name}</span>
            {owner ? (
              <div className="flex-noshrink" style={{ width: 128 }}>
                <SmartAutocomplete
                  aria-label={`Role for ${m.name}`}
                  options={ROLES}
                  value={roleOption(m.role)}
                  disabled={busy !== null}
                  onChange={(o) => o && o.value !== m.role && run(m.id, () => setNoteMemberRole(slug, note.id, m.id, o.value as NoteRole))}
                />
              </div>
            ) : (
              <span className="text-caption text-secondary flex-noshrink">{roleOption(m.role).label}</span>
            )}
            {owner || m.id === me ? (
              <IconButton
                className="icon-button-danger note-tool"
                aria-label={m.id === me ? 'Leave note' : `Remove ${m.name}`}
                tooltip={m.id === me ? 'Leave note' : 'Remove'}
                disabled={busy !== null}
                onClick={() => run(m.id, () => removeNoteMember(slug, note.id, m.id), m.id === me ? () => (onClose(), onGone?.()) : undefined)}
              >
                <XIcon size={16} />
              </IconButton>
            ) : null}
          </li>
        ))}
      </ul>
      <div className="grid gap-2">
        <div className="flex gap-2">
          <div className="flex-grow">
            <Input type="search" aria-label="Add people" placeholder="Add people by name or email" value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
          <div className="flex-noshrink" style={{ width: 128 }}>
            <SmartAutocomplete aria-label="Role for people added" options={ROLES} value={roleOption(role)} onChange={(o) => o && setRole(o.value as NoteRole)} />
          </div>
        </div>
        {found.length ? (
          <ul className="grid m-0 p-0" style={{ listStyle: 'none' }}>
            {found.map((m) => (
              <li key={m.id}>
                <button
                  type="button"
                  className="popover-item w-full gap-3"
                  style={{ height: 44 }}
                  disabled={busy !== null || on.has(m.id)}
                  onClick={() => run(m.id, () => addNoteMember(slug, note.id, m.id, role), () => setQ(''))}
                >
                  <Avatar name={m.name} size={24} src={avatarSrc(m.id)} />
                  <span className="ellipsis flex-grow">{m.name}</span>
                  {on.has(m.id) ? <span className="text-caption text-secondary">Added</span> : null}
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </div>
      <FormError>{error}</FormError>
      <div className="flex justify-end">
        <Button variant="secondary" onClick={onClose}>
          Done
        </Button>
      </div>
    </Dialog>
  )
}
