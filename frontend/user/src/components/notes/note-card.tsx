import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent, type MouseEvent, type CSSProperties } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { CheckIcon } from '@taskwell/shared/icons'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { RichText, type Doc } from '@taskwell/shared/rich-editor'
import { fullTime, relativeTime } from '@taskwell/shared/time'
import { noteTitle } from '@taskwell/shared/validate'
import { avatarSrc, saveNote, toFormErrors, type FieldErrors, type Note, type PersonRef } from '@/api'
import { firstName } from '@/components/chat/chat'
import { FormError } from '@/components/auth/auth-card'
import { SavedAt, useAutosave } from './autosave'
import { NoteBody } from './note-body'
import { NoteTools, focusNote, tint, titleKeyDown, useNoteAction } from './note-tools'

type Props = { slug: string; note: Note; labels: string[]; me: string }

export function NoteCard({ slug, note, labels, me, onOpen, selected, onPick }: Props & { onOpen: () => void; selected: boolean; onPick: () => void }) {
  const act = useNoteAction()
  // A ticked item shows at once; the next load of this note replaces it.
  const [ticked, setTicked] = useState<{ base: Doc | null; doc: Doc } | null>(null)
  const body = ticked && ticked.base === note.body ? ticked.doc : note.body
  const others = [note.owner, ...note.members].filter((p) => p.id !== me)

  async function toggle(doc: Doc) {
    setTicked({ base: note.body, doc })
    if (!(await act(() => saveNote(slug, { body: JSON.stringify(doc) }, note.id)))) setTicked(null)
  }

  // While some notes are selected, clicking a card picks it instead of opening it.
  function open(e: MouseEvent<HTMLElement>) {
    const target = e.target as Element
    if (!e.currentTarget.contains(target) || target.closest('button, a, input, label')) return
    if (e.currentTarget.closest('[data-selecting]')) onPick()
    else onOpen()
  }

  return (
    <article
      className="note-card grid gap-3"
      data-selected={selected || undefined}
      style={{ background: tint(note.color) }}
      tabIndex={0}
      aria-label={note.title || 'Note'}
      onClick={open}
      onKeyDown={(e: KeyboardEvent<HTMLElement>) => e.key === 'Enter' && e.target === e.currentTarget && onOpen()}
    >
      <button type="button" className="note-select" aria-label={selected ? 'Deselect note' : 'Select note'} aria-pressed={selected} onClick={onPick}>
        <CheckIcon size={14} />
      </button>
      {others.length ? <Faces people={others} /> : null}
      {note.title ? <h3 className="note-title">{note.title}</h3> : null}
      {body ? (
        <div className="note-body">
          <RichText doc={body} aria-label="Note" onToggleTask={note.can_edit ? toggle : undefined} />
        </div>
      ) : null}
      {!note.title && !body ? <p className="text-body-sm text-secondary">Empty note</p> : null}
      <NoteMeta note={note} />
      <footer className="flex flex-wrap items-center gap-2">
        <NoteTools slug={slug} note={note} labels={labels} me={me} />
        <p className="text-caption text-secondary ellipsis m-0" style={{ marginLeft: 'auto', paddingBlock: 8 }}>
          {[note.owner.id !== me ? `From ${firstName(note.owner.name)}` : null, note.can_edit ? null : 'View only'].filter(Boolean).map((t) => `${t} · `)}
          <time dateTime={note.updated_at} title={fullTime(note.updated_at)}>
            Edited {relativeTime(note.updated_at)}
          </time>
        </p>
      </footer>
    </article>
  )
}

const MAX_FACES = 6

// Everyone else on the note, overlapping on the card's top right edge.
function Faces({ people }: { people: PersonRef[] }) {
  const names = people.map((p) => p.name).join(', ')
  const extra = people.length - MAX_FACES
  return (
    <span className="note-faces" title={names}>
      {people.slice(0, extra > 0 ? MAX_FACES - 1 : MAX_FACES).map((p) => (
        <Avatar key={p.id} name={p.name} size={28} src={avatarSrc(p.id)} />
      ))}
      {extra > 0 ? <span className="note-faces-more" aria-hidden>+{extra + 1}</span> : null}
      <span className="sr-only">Shared with {names}</span>
    </span>
  )
}

function NoteMeta({ note }: { note: Note }) {
  if (!note.labels.length) return null
  return (
    <div className="flex flex-wrap items-center gap-2">
      {note.labels.map((l) => (
        <span key={l} className="note-chip">
          {l}
        </span>
      ))}
    </div>
  )
}

// The note in a dialog: the body is live (./note-body); the title autosaves 500ms after typing stops (and on close);
// the tools save as they're used. Viewers can only pin, archive and label it.
export function NoteDialog({ slug, note, labels, me, onClose }: Props & { onClose: () => void }) {
  const [title, setTitle] = useState(note.title)
  const [errors, setErrors] = useState<FieldErrors>({})
  const [error, setError] = useState<string | null>(null)
  const [savedAt, setSavedAt] = useState(note.updated_at)
  const saved = useRef(note.title.trim())
  // The later of this popup's own title save and the note's last change (body saves arrive as reloads).
  const at = Date.parse(note.updated_at) >= Date.parse(savedAt) ? note.updated_at : savedAt

  const { saving, schedule, flush } = useAutosave(async () => {
    const next = title.trim()
    if (!note.can_edit || next === saved.current) return true
    const found = { title: noteTitle(title) }
    setErrors(found)
    setError(null)
    if (found.title) return false
    try {
      const n = await saveNote(slug, { title: next }, note.id)
      saved.current = next
      setSavedAt(n.updated_at)
      return true
    } catch (err) {
      const f = toFormErrors(err)
      setErrors(f.errors)
      setError(f.error)
      return false
    }
  })

  async function close(e?: FormEvent) {
    e?.preventDefault()
    if (await flush()) onClose()
  }

  useEffect(() => {
    if (!note.title && note.can_edit) focusNote(false)
  }, [note.id])

  const heading = (
    <div className="flex items-center gap-3">
      <Input
        className="note-heading-title"
        aria-label="Title"
        placeholder={note.can_edit ? 'Title' : 'Untitled note'}
        value={title}
        readOnly={!note.can_edit}
        onChange={(e) => (setTitle(e.target.value), schedule())}
        onKeyDown={titleKeyDown}
        error={errors.title}
      />
      {note.can_edit ? null : <span className="note-chip flex-noshrink text-secondary">View only</span>}
      <span className="flex-noshrink" style={{ marginLeft: 'auto' }}>
        <SavedAt saving={saving} at={at} by={at === note.updated_at && note.updated_by && note.updated_by.id !== me ? note.updated_by.name : undefined} />
      </span>
    </div>
  )

  return (
    <Dialog open onClose={close} title={title.trim() || 'Untitled note'} heading={heading} full>
      <form noValidate onSubmit={close} className="note-sheet" style={{ '--page': tint(note.color) } as CSSProperties}>
        <div className="note-sheet-body grid gap-3">
          <NoteBody key={note.id} slug={slug} note={note} />
          <FormError>{error}</FormError>
        </div>
        <footer className="note-sheet-footer">
          <div className="flex flex-wrap items-center gap-3">
            <NoteTools slug={slug} note={note} labels={labels} me={me} onGone={onClose} />
            <Button type="submit" variant="secondary" style={{ marginLeft: 'auto' }}>
              Close
            </Button>
          </div>
        </footer>
      </form>
    </Dialog>
  )
}
