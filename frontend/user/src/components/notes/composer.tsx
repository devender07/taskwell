import { useEffect, useRef, useState, type FormEvent, type CSSProperties } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { IconButton } from '@taskwell/shared/icon-button'
import { ChecklistIcon, PlusIcon } from '@taskwell/shared/icons'
import { Input } from '@taskwell/shared/input'
import { RichEditor, type Doc } from '@taskwell/shared/rich-editor'
import { noteBody, noteTitle } from '@taskwell/shared/validate'
import { saveNote, toFormErrors, type FieldErrors } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { SavedAt, useAutosave } from './autosave'
import { focusNote, NoteColors, tint, titleKeyDown } from './note-tools'

const CHECKLIST: Doc = { type: 'doc', content: [{ type: 'taskList', content: [{ type: 'taskItem', attrs: { checked: false }, content: [{ type: 'paragraph' }] }] }] }

// "Take a note…": a bar that opens the note popup (?note=new, or ?note=checklist to start with one).
export function NoteComposer({ onStart }: { onStart: (checklist: boolean) => void }) {
  return (
    <div className="flex items-center gap-2 flex-noshrink">
      <Button className="note-cta" onClick={() => onStart(false)}>
        <PlusIcon size={16} />
        Take a note
      </Button>
      <IconButton className="note-tool" aria-label="New checklist" tooltip="New checklist" onClick={() => onStart(true)}>
        <ChecklistIcon />
      </IconButton>
    </div>
  )
}

// The new note, full screen. It's created by the first autosave (500ms after typing stops), then updated; an
// empty one is never created. Once created, the URL names it, so a reload opens the saved note.
export function NewNoteDialog({ slug, checklist, label, onDone }: { slug: string; checklist: boolean; label?: string; onDone: () => void }) {
  const [title, setTitle] = useState('')
  const [body, setBody] = useState<{ doc: Doc | null; length: number }>({ doc: null, length: 0 })
  const [color, setColor] = useState('')
  const [errors, setErrors] = useState<FieldErrors>({})
  const [error, setError] = useState<string | null>(null)
  const [savedAt, setSavedAt] = useState<string | null>(null)
  const id = useRef<string | null>(null)
  const saved = useRef(JSON.stringify(['', null, '']))

  const { saving, schedule, flush } = useAutosave(async () => {
    const snap = JSON.stringify([title.trim(), body.doc, color])
    if (snap === saved.current || (!id.current && !title.trim() && !body.doc)) return true
    const found = { title: noteTitle(title), body: noteBody(body.length) }
    setErrors(found)
    setError(null)
    if (found.title || found.body) return false
    const fields: Record<string, string> = { title: title.trim(), body: body.doc ? JSON.stringify(body.doc) : '', color }
    // A note made while a label is shown gets that label, as it would otherwise vanish from view.
    if (!id.current && label) fields.labels = JSON.stringify([label])
    try {
      const n = await saveNote(slug, fields, id.current ?? undefined)
      if (!id.current) {
        // Not through the router: that would swap this popup for the saved note's while it's being typed in.
        const url = new URL(location.href)
        url.searchParams.set('note', n.id)
        history.replaceState(history.state, '', url)
      }
      id.current = n.id
      saved.current = snap
      setSavedAt(n.updated_at)
      return true
    } catch (err) {
      const f = toFormErrors(err)
      setErrors(f.errors)
      setError(f.error)
      return false
    }
  })

  useEffect(() => {
    focusNote(false)
  }, [])

  async function close(e?: FormEvent) {
    e?.preventDefault()
    if (await flush()) onDone()
  }

  const heading = (
    <div className="flex items-center gap-3">
      <Input
        className="note-heading-title"
        aria-label="Title"
        placeholder="Title"
        value={title}
        onChange={(e) => (setTitle(e.target.value), schedule())}
        onKeyDown={titleKeyDown}
        error={errors.title}
      />
      <span className="flex-noshrink" style={{ marginLeft: 'auto' }}>
        <SavedAt saving={saving} at={savedAt} />
      </span>
    </div>
  )

  return (
    <Dialog open onClose={close} title={title.trim() || 'New note'} heading={heading} full>
      <form noValidate onSubmit={close} className="note-sheet" style={{ '--page': tint(color) } as CSSProperties}>
        <div className="note-sheet-body grid gap-3">
          <RichEditor aria-label="Note" placeholder="Type here…" tasks content={checklist ? CHECKLIST : null} minHeight={240} error={errors.body} onChange={(doc, length) => (setBody({ doc, length }), schedule())} />
          <FormError>{error}</FormError>
        </div>
        <footer className="note-sheet-footer">
          <div className="flex flex-wrap items-center gap-3">
            <NoteColors value={color} onChange={(c) => (setColor(c), schedule())} />
            <Button type="submit" variant="secondary" style={{ marginLeft: 'auto' }}>
              Close
            </Button>
          </div>
        </footer>
      </form>
    </Dialog>
  )
}
