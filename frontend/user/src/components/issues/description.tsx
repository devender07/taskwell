import { useEffect, useMemo, useRef, useState } from 'react'
import * as Y from 'yjs'
import { Awareness } from 'y-protocols/awareness'
import { Avatar } from '@tether/shared/avatar'
import { RichEditor, seedYDoc, yDocToJSON } from '@tether/shared/rich-editor'
import { issueDescription } from '@tether/shared/validate'
import { avatarSrc, uploadFile, type Issue, type Member, type User, type Workspace } from '@/api'
import type { Viewer } from '@/realtime'
import { REMOTE, connectCollab, type CollabState } from './collab'
import { useUnsavedPrompt } from './timeline'

type Session = { doc: Y.Doc; awareness: Awareness }

const CARET_COLORS = ['--accent', '--success', '--link']

function caretColor(id: string) {
  let h = 0
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) | 0
  return getComputedStyle(document.documentElement).getPropertyValue(CARET_COLORS[Math.abs(h) % CARET_COLORS.length]!).trim()
}

type Props = { workspace: Workspace; issue: Issue; user: User; members: Member[]; onViewers: (viewers: Viewer[]) => void }

// The live description editor; `onViewers` (a stable setter) gets the other people who have this issue open.
export function Description({ workspace, issue, user, members, onViewers }: Props) {
  const [session, setSession] = useState<Session | null>(null)
  const [live, setLive] = useState<CollabState | null>(null)
  const [offline, setOffline] = useState(false)
  const [empty, setEmpty] = useState(!issue.description)
  const [unsent, setUnsent] = useState(false)
  const [tooLong, setTooLong] = useState<string | null>(null)
  const [discarded, setDiscarded] = useState(false)
  const [round, setRound] = useState(0) // a new doc after a change too large to sync
  const seed = useRef(issue.description)
  const long = useRef(false)
  useUnsavedPrompt(unsent)
  const mentions = useMemo(() => members.map((m) => ({ id: m.id, name: m.name })), [members])
  const slug = workspace.slug
  const identifier = issue.identifier
  const userId = user.id

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
      setDiscarded(true)
      setRound((r) => r + 1)
    })
    doc.on('update', (_: Uint8Array, origin: unknown) => {
      if (origin === REMOTE || seeding || discard) return
      if (!conn.ready()) setUnsent(true)
      setDiscarded(false)
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
  }, [slug, identifier, userId, onViewers, round])

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
              upload={(file) => uploadFile(workspace.slug, file)}
              onChange={(doc, length) => {
                const error = issueDescription(length)
                long.current = !!error
                setTooLong(error)
                setEmpty(!doc)
              }}
            />
          </div>
          {!editable && empty ? none : null}
        </>
      ) : issue.description ? (
        <RichEditor aria-label="Description" content={issue.description} editable={false} />
      ) : issue.can_edit ? null : (
        none
      )}
      {tooLong || discarded ? (
        <p role="alert" className="text-caption text-accent m-0">
          {tooLong ?? 'That change was too large to save (over 256 KB), so it was undone.'}
        </p>
      ) : null}
      {offline ? <p className="text-caption text-secondary m-0">Offline, reconnecting…</p> : null}
    </section>
  )
}

const SHOWN_VIEWERS = 5

export function Viewers({ viewers }: { viewers: Viewer[] }) {
  if (viewers.length === 0) return null
  const extra = viewers.length - SHOWN_VIEWERS
  return (
    <div
      role="img"
      aria-label={`Also viewing: ${viewers.map((v) => v.name).join(', ')}`}
      className="flex items-center flex-noshrink"
      style={{ height: 40, paddingLeft: 4 }}
    >
      {viewers.slice(0, SHOWN_VIEWERS).map((v) => (
        <span key={v.id} title={v.name} className="inline-grid rounded-full" style={{ marginLeft: -4, boxShadow: '0 0 0 2px var(--bg)' }}>
          <Avatar name={v.name} size={24} src={avatarSrc(v.id)} />
        </span>
      ))}
      {extra > 0 ? (
        <span className="text-caption text-secondary" style={{ marginLeft: 4 }} title={viewers.slice(SHOWN_VIEWERS).map((v) => v.name).join(', ')}>
          +{extra}
        </span>
      ) : null}
    </div>
  )
}
