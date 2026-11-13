import { useEffect, useMemo, useRef, useState } from 'react'
import { useRouteContext, useRouter } from '@tanstack/react-router'
import * as Y from 'yjs'
import { Awareness } from 'y-protocols/awareness'
import { RichEditor, RichText, seedYDoc, yDocToJSON } from '@taskwell/shared/rich-editor'
import { noteBody } from '@taskwell/shared/validate'
import type { Note } from '@/api'
import { reloadPage } from '@/realtime'
import { REMOTE, caretColor, connectCollab, type CollabState } from '@/components/issues/collab'
import { useUnsavedPrompt } from '@/components/issues/timeline'
import { focusNote } from './note-tools'

type Session = { doc: Y.Doc; awareness: Awareness }

const NOTICES = {
  tooLarge: 'That change was too large to save (over 256 KB), so it was undone.',
  replaced: 'This note was replaced while you were offline, so your offline edits were discarded.',
}

// The note's body, edited live with everyone it's shared with (the issue description's protocol). Viewers see it
// update but can't type. Until the session is ready it shows the saved body.
export function NoteBody({ slug, note }: { slug: string; note: Note }) {
  const user = useRouteContext({ from: '/_app', select: (c) => c.user })
  const [session, setSession] = useState<Session | null>(null)
  const [live, setLive] = useState<CollabState | null>(null)
  const [offline, setOffline] = useState(false)
  const [unsent, setUnsent] = useState(false)
  const [tooLong, setTooLong] = useState<string | null>(null)
  const [notice, setNotice] = useState<keyof typeof NOTICES | null>(null)
  const [round, setRound] = useState(0) // a new doc after a change too large to sync, or a reset
  const seed = useRef(note.body)
  const long = useRef(false)
  const focused = useRef(false)
  const router = useRouter()
  useUnsavedPrompt(unsent)
  const id = note.id

  // Declared before the doc's effect, so a new round seeds from the body just loaded.
  useEffect(() => {
    seed.current = note.body
  }, [note.body])

  useEffect(() => {
    const doc = new Y.Doc()
    const awareness = new Awareness(doc)
    let dirty = false
    let seeding = false
    let discard = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const flush = () => {
      clearTimeout(timer)
      if (dirty && !discard && !long.current && conn.save(JSON.stringify(yDocToJSON(doc)))) dirty = false
    }
    const conn = connectCollab(slug, 'note', id, doc, awareness, (s) => {
      if (!s) return setOffline(true)
      if (s.seed && seed.current && doc.getXmlFragment('default').length === 0) {
        seeding = true
        seedYDoc(doc, seed.current, { tasks: true })
        seeding = false
      }
      setLive(s)
      setOffline(false)
      setUnsent(false)
      if (dirty) flush()
    }, () => {}, () => {
      discard = true
      setNotice('tooLarge')
      setRound((r) => r + 1)
    }, (lost) => {
      discard = true
      if (lost) setNotice('replaced')
      void reloadPage(router).finally(() => setRound((r) => r + 1))
    })
    doc.on('update', (_: Uint8Array, origin: unknown) => {
      if (origin === REMOTE || seeding || discard) return
      if (!conn.ready()) setUnsent(true)
      setNotice(null)
      dirty = true
      clearTimeout(timer)
      timer = setTimeout(flush, 1000)
    })
    setSession({ doc, awareness })
    addEventListener('pagehide', flush)
    return () => {
      removeEventListener('pagehide', flush)
      flush()
      conn.destroy()
      awareness.destroy()
      doc.destroy()
    }
  }, [slug, id, round, router])

  const collab = useMemo(() => session && { ...session, user: { name: user.name, color: caretColor(user.id) } }, [session, user.name, user.id])
  const editable = note.can_edit && !!live?.canEdit

  // The popup focuses the title when there's none; otherwise the editor, once it's live.
  useEffect(() => {
    if (!editable || focused.current) return
    focused.current = true
    if (note.title && !document.activeElement?.matches('input, [contenteditable="true"]')) focusNote(true)
  }, [editable, note.title])

  return (
    <>
      {collab && live ? (
        <RichEditor
          aria-label="Note"
          placeholder={editable ? 'Type here…' : undefined}
          tasks
          collab={collab}
          editable={editable}
          minHeight={240}
          error={tooLong}
          onChange={(_, length) => {
            const error = noteBody(length)
            long.current = !!error
            setTooLong(error)
          }}
        />
      ) : note.body ? (
        <RichText doc={note.body} aria-label="Note" />
      ) : null}
      {notice ? (
        <p role="alert" className="text-caption text-accent m-0">
          {NOTICES[notice]}
        </p>
      ) : null}
      {offline ? <p className="text-caption text-secondary m-0">Offline, reconnecting…</p> : null}
    </>
  )
}
