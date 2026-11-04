import { useEffect, useMemo, useRef, useState } from 'react'
import { useRouter } from '@tanstack/react-router'
import * as Y from 'yjs'
import { Awareness } from 'y-protocols/awareness'
import { RichEditor, seedYDoc, yDocToJSON } from '@taskwell/shared/rich-editor'
import { issueDescription } from '@taskwell/shared/validate'
import { api, issuePath, uploadFile, type Issue } from '@/api'
import { reloadPage } from '@/realtime'
import { REMOTE, connectCollab, type CollabState } from './collab'
import { StaticDescription, type DescriptionProps } from './description'
import { useUnsavedPrompt } from './timeline'

type Session = { doc: Y.Doc; awareness: Awareness }

const CARET_COLORS = ['--accent', '--success', '--link']

function caretColor(id: string) {
  let h = 0
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) | 0
  return getComputedStyle(document.documentElement).getPropertyValue(CARET_COLORS[Math.abs(h) % CARET_COLORS.length]!).trim()
}

const NOTICES = {
  tooLarge: 'That change was too large to save (over 256 KB), so it was undone.',
  replaced: 'This description was replaced while you were offline, so your offline edits were discarded.',
}

// The live description editor (loaded lazily by ./description); `onViewers` (a stable setter) gets the other people
// who have this issue open.
export default function LiveDescription({ workspace, issue, user, people: mentions, onViewers }: DescriptionProps) {
  const [session, setSession] = useState<Session | null>(null)
  const [live, setLive] = useState<CollabState | null>(null)
  const [offline, setOffline] = useState(false)
  const [empty, setEmpty] = useState(!issue.description)
  const [unsent, setUnsent] = useState(false)
  const [tooLong, setTooLong] = useState<string | null>(null)
  const [notice, setNotice] = useState<keyof typeof NOTICES | null>(null)
  const [round, setRound] = useState(0) // a new doc after a change too large to sync, or a reset
  const seed = useRef(issue.description)
  const long = useRef(false)
  const router = useRouter()
  useUnsavedPrompt(unsent)
  const slug = workspace.slug
  const identifier = issue.identifier
  const userId = user.id

  // Declared before the doc's effect, so a new round seeds from the description just loaded.
  useEffect(() => {
    seed.current = issue.description
  }, [issue.description])

  useEffect(() => {
    const doc = new Y.Doc()
    const awareness = new Awareness(doc)
    let dirty = false
    let seeding = false
    let discard = false
    let timer: ReturnType<typeof setTimeout> | undefined
    // The server would reject an over-long description, so it isn't sent; the live doc keeps it until trimmed.
    const flush = () => {
      clearTimeout(timer)
      if (dirty && !discard && !long.current && conn.save(JSON.stringify(yDocToJSON(doc)))) dirty = false
    }
    const conn = connectCollab(slug, identifier, doc, awareness, (s) => {
      if (!s) return setOffline(true)
      if (s.seed && seed.current && doc.getXmlFragment('default').length === 0) {
        seeding = true
        seedYDoc(doc, seed.current)
        seeding = false
      }
      setLive(s)
      setOffline(false)
      setUnsent(false)
      if (dirty) flush()
    }, (viewers) => onViewers(viewers.filter((v) => v.id !== userId)), () => {
      discard = true
      setNotice('tooLarge')
      setRound((r) => r + 1)
    }, (lost) => {
      discard = true
      if (lost) setNotice('replaced')
      // Seeded from a fetch of its own: the reloaded page data can reach this component after the new round starts.
      const fresh = api<Issue>(issuePath(workspace, identifier)).then((i) => void (seed.current = i.description))
      void Promise.allSettled([fresh, reloadPage(router)]).then(() => setRound((r) => r + 1))
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
      onViewers([])
    }
  }, [slug, identifier, userId, onViewers, round, router])

  const collab = useMemo(
    () => session && { ...session, user: { name: user.name, color: caretColor(user.id) } },
    [session, user.name, user.id],
  )
  const editable = issue.can_edit && !!live?.canEdit

  return (
    <section className="grid gap-2">
      {collab && live ? (
        <>
          <div className={!editable && empty ? 'hidden' : undefined}>
            <RichEditor
              aria-label="Description"
              collab={collab}
              editable={editable}
              placeholder="Add a description… Type @ to mention someone"
              mentions={mentions}
              minHeight={160}
              upload={(file) => uploadFile(workspace.slug, file)}
              onChange={(doc, length) => {
                const error = issueDescription(length)
                long.current = !!error
                setTooLong(error)
                setEmpty(!doc)
              }}
            />
          </div>
          {!editable && empty ? <StaticDescription doc={null} canEdit={false} /> : null}
        </>
      ) : (
        <StaticDescription doc={issue.description} canEdit={issue.can_edit} />
      )}
      {tooLong || notice ? (
        <p role="alert" className="text-caption text-accent m-0">
          {tooLong ?? NOTICES[notice!]}
        </p>
      ) : null}
      {offline ? <p className="text-caption text-secondary m-0">Offline, reconnecting…</p> : null}
    </section>
  )
}
