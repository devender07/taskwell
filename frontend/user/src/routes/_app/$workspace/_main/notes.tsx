import { useEffect, useState } from 'react'
import { createFileRoute, useRouteContext } from '@tanstack/react-router'
import { Input } from '@taskwell/shared/input'
import type { Doc } from '@taskwell/shared/rich-editor'
import { notesPath, type Note, requireFeature } from '@/api'
import { loaderApi } from '@/realtime'
import { NewNoteDialog, NoteComposer } from '@/components/notes/composer'
import { NoteCard, NoteDialog } from '@/components/notes/note-card'
import { NotesBulkBar } from '@/components/notes/bulk-bar'
import '@/components/notes/notes.css'
import { byId, extractDataFromArray } from '@taskwell/shared/list'

// note: the open popup, kept across reloads: 'new', 'checklist' (a new note starting with one) or a note's id.
type Search = { label?: string; archived?: true; q?: string; note?: string }

export const Route = createFileRoute('/_app/$workspace/_main/notes')({
  beforeLoad: requireFeature('notes'),
  // Filters run on the loaded notes: the API returns all of the caller's, archived too.
  validateSearch: (search): Search => ({
    ...(typeof search.label === 'string' && search.label && { label: search.label }),
    ...(search.archived === true && { archived: true }),
    ...(typeof search.q === 'string' && search.q && { q: search.q }),
    ...(typeof search.note === 'string' && search.note && { note: search.note }),
  }),
  loader: (ctx) => loaderApi<Note[]>(ctx, notesPath(ctx.params.workspace)),
  // Unlike other pages, a search change (opening or closing a note) refetches, so the grid is fresh after the popup.
  shouldReload: ({ cause }) => (cause === 'stay' ? true : undefined),
  component: NotesPage,
})

const text = (d: Doc): string => (d.text ?? '') + (d.type === 'mention' ? String(extractDataFromArray(d.attrs, 'label', '')) : '') + ' ' + (d.content ?? []).map(text).join(' ')

function NotesPage() {
  const { workspace: slug } = Route.useParams()
  const me = useRouteContext({ from: '/_app', select: (c) => c.user.id })
  const notes = Route.useLoaderData()
  const { label, archived, q = '', note } = Route.useSearch()
  const navigate = Route.useNavigate()

  const labels = [...new Map(notes.flatMap((n) => n.labels).map((l) => [l.toLowerCase(), l])).values()].sort((a, b) => a.localeCompare(b))
  const needle = q.trim().toLowerCase()
  const shown = notes.filter(
    (n) =>
      n.archived === !!archived &&
      (!label || n.labels.some((l) => l.toLowerCase() === label.toLowerCase())) &&
      (!needle || `${n.title} ${n.body ? text(n.body) : ''}`.toLowerCase().includes(needle)),
  )
  const pinned = archived ? [] : shown.filter((n) => n.pinned)
  const others = archived ? shown : shown.filter((n) => !n.pinned)
  const open = byId(notes, note)
  // Closing replaces the entry, so Back doesn't reopen a note that was just closed.
  const popup = (next?: string) => navigate({ search: (prev) => ({ ...prev, note: next }), replace: !next })
  const filter = (next: Search) => navigate({ search: (prev) => ({ q: prev.q, ...next }), replace: true })

  // Selected notes by id, among those shown; changing the filter or search starts over. Escape clears.
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const view = `${label ?? ''}|${archived ?? ''}|${needle}`
  const [seen, setSeen] = useState(view)
  if (view !== seen) {
    setSeen(view)
    setSelected(new Set())
  }
  const picked = shown.filter((n) => selected.has(n.id))
  const pick = (id: string) => {
    const next = new Set(selected)
    if (!next.delete(id)) next.add(id)
    setSelected(next)
  }
  useEffect(() => {
    if (!picked.length) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && !document.querySelector('dialog[open]') && setSelected(new Set())
    addEventListener('keydown', onKey)
    return () => removeEventListener('keydown', onKey)
  }, [picked.length])

  const grid = (list: Note[]) => (
    <div className="note-grid" data-selecting={picked.length ? '' : undefined}>
      {list.map((n) => (
        <div key={n.id} className="note-cell">
          <NoteCard slug={slug} note={n} labels={labels} me={me} onOpen={() => popup(n.id)} selected={selected.has(n.id)} onPick={() => pick(n.id)} />
        </div>
      ))}
    </div>
  )

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start', minWidth: 0, ...(picked.length && { paddingBottom: 120 }) }}>
      <div className="page-top grid gap-6">
        <header className="grid gap-4">
          <h1 className="text-h2">Notes</h1>
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex-grow" style={{ flexBasis: 280 }}>
              <Input
                type="search"
                aria-label="Search notes"
                placeholder="Search notes"
                defaultValue={q}
                onDebouncedChange={(next) => navigate({ search: (prev) => ({ ...prev, q: next.trim() || undefined }), replace: true })}
              />
            </div>
            {archived ? null : <NoteComposer onStart={(checklist) => popup(checklist ? 'checklist' : 'new')} />}
          </div>
          <div className="flex flex-wrap gap-2" role="group" aria-label="Show">
            <button type="button" className="note-filter" aria-pressed={!label && !archived} onClick={() => filter({})}>
              Notes
            </button>
            {labels.map((l) => (
              <button key={l} type="button" className="note-filter" aria-pressed={label?.toLowerCase() === l.toLowerCase()} onClick={() => filter({ label: l })}>
                {l}
              </button>
            ))}
            <button type="button" className="note-filter" aria-pressed={!!archived} onClick={() => filter({ archived: true })}>
              Archive
            </button>
          </div>
        </header>
      </div>
      {pinned.length ? (
        <section className="grid gap-3" aria-labelledby="notes-pinned">
          <h2 id="notes-pinned" className="text-label text-secondary">Pinned</h2>
          {grid(pinned)}
        </section>
      ) : null}
      {others.length ? (
        <section className="grid gap-3" aria-labelledby={pinned.length ? 'notes-others' : undefined}>
          {pinned.length ? <h2 id="notes-others" className="text-label text-secondary">Others</h2> : null}
          {grid(others)}
        </section>
      ) : null}
      {shown.length ? null : (
        <p className="text-body-sm text-secondary p-6 bg-surface border rounded-lg text-center">
          {needle ? 'No notes match your search.' : archived ? 'Archived notes show up here.' : label ? 'No notes with this label.' : 'Notes you add show up here.'}
        </p>
      )}
      {picked.length ? <NotesBulkBar slug={slug} notes={picked} me={me} onClear={() => setSelected(new Set())} /> : null}
      {note === 'new' || note === 'checklist' ? (
        <NewNoteDialog slug={slug} checklist={note === 'checklist'} label={label} onDone={() => popup()} />
      ) : open ? (
        <NoteDialog key={open.id} slug={slug} note={open} labels={labels} me={me} onClose={() => popup()} />
      ) : null}
    </main>
  )
}
