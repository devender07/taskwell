import { useEffect, useMemo, useRef, useState } from 'react'
import * as Y from 'yjs'
import { Awareness } from 'y-protocols/awareness'
import { RichEditor, seedYDoc, yDocToJSON } from '@tether/shared/rich-editor'
import type { Issue, Member, User, Workspace } from '@/api'
import { REMOTE, connectCollab, type CollabState } from './collab'
import { issuePath } from './issue-form'

type Session = { doc: Y.Doc; awareness: Awareness }

const CARET_COLORS = ['--accent', '--success', '--link']

function caretColor(id: string) {
  let h = 0
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) | 0
  return getComputedStyle(document.documentElement).getPropertyValue(CARET_COLORS[Math.abs(h) % CARET_COLORS.length]!).trim()
}

export function Description({ workspace, issue, user, members }: { workspace: Workspace; issue: Issue; user: User; members: Member[] }) {
  const [session, setSession] = useState<Session | null>(null)
  const [live, setLive] = useState<CollabState | null>(null)
  const [offline, setOffline] = useState(false)
  const [empty, setEmpty] = useState(!issue.description)
  const seed = useRef(issue.description)
  const mentions = useMemo(() => members.map((m) => ({ id: m.id, name: m.name })), [members])
  const path = `${issuePath(workspace, issue.identifier)}/collab`

  useEffect(() => {
    const doc = new Y.Doc()
    const awareness = new Awareness(doc)
    let dirty = false
    let seeding = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const flush = () => {
      clearTimeout(timer)
      if (dirty && conn.save(JSON.stringify(yDocToJSON(doc)))) dirty = false
    }
    const conn = connectCollab(path, doc, awareness, (s) => {
      if (!s) return setOffline(true)
      if (s.seed && seed.current && doc.getXmlFragment('default').length === 0) {
        seeding = true
        seedYDoc(doc, seed.current)
        seeding = false
      }
      setLive(s)
      setOffline(false)
      if (dirty) flush()
    })
    doc.on('update', (_: Uint8Array, origin: unknown) => {
      if (origin === REMOTE || seeding) return
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
  }, [path])

  const collab = useMemo(
    () => session && { ...session, user: { name: user.name, color: caretColor(user.id) } },
    [session, user.name, user.id],
  )
  const editable = issue.can_edit && !!live?.canEdit
  const none = <p className="text-body-sm text-secondary m-0">No description.</p>

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
              onChange={(doc) => setEmpty(!doc)}
            />
          </div>
          {!editable && empty ? none : null}
        </>
      ) : issue.description ? (
        <RichEditor aria-label="Description" content={issue.description} editable={false} />
      ) : issue.can_edit ? null : (
        none
      )}
      {offline ? <p className="text-caption text-secondary m-0">Offline, reconnecting…</p> : null}
    </section>
  )
}
