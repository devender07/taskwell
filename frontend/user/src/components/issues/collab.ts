import * as Y from 'yjs'
import { applyAwarenessUpdate, encodeAwarenessUpdate, removeAwarenessStates, type Awareness } from 'y-protocols/awareness'
import { joinCollab, type Viewer } from '@/realtime'

export type CollabState = { canEdit: boolean; seed: boolean; lastId: bigint; loaded: number }

// Origin of changes applied from the server, so they aren't sent back.
export const REMOTE = 'remote'

const [READY, UPDATE, AWARENESS, COMPACT, SAVE, SEED] = [0, 1, 2, 3, 4, 5]
const COMPACT_AFTER = 100
// The server's caps (backend/user/internal/issue/collab.go); it drops the connection for a bigger frame.
const MAX_UPDATE = 256 << 10
const MAX_COMPACT = 1 << 20

// Live editing of one issue's description over the shared realtime socket (protocol in docs/rich-text-plan.md).
// onState gets the server's `ready` info, or null when the connection drops; it re-joins until destroy().
// onTooLarge fires instead of sending a change over the server's cap; after it nothing more is sent, since later
// changes build on the unsent one, so the caller starts over with a fresh doc.
export function connectCollab(
  workspace: string,
  issue: string,
  doc: Y.Doc,
  awareness: Awareness,
  onState: (s: CollabState | null) => void,
  onPresence: (viewers: Viewer[]) => void,
  onTooLarge: () => void,
) {
  let ready = false
  let blocked = false
  let state: CollabState | null = null
  let stored: Y.Doc | null = null // what the server replayed, to find local edits it hasn't seen
  let loaded = 0

  const send = (type: number, payload: Uint8Array) => ready && !blocked && channel.send(type, payload)
  const sendUpdate = (update: Uint8Array) => {
    if (blocked) return
    if (update.length <= MAX_UPDATE) return send(UPDATE, update)
    blocked = true
    onTooLarge()
  }
  const sendAwareness = (clients: number[]) => send(AWARENESS, encodeAwarenessUpdate(awareness, clients))

  const onUpdate = (update: Uint8Array, origin: unknown) => {
    if (origin !== REMOTE) sendUpdate(update)
  }
  const onAwareness = ({ added, updated, removed }: Record<string, number[]>, origin: unknown) => {
    if (origin !== REMOTE) sendAwareness([...added!, ...updated!, ...removed!])
  }
  // Someone joined: they only hear about us on our next change, so tell them now.
  const onPeer = ({ added }: Record<string, number[]>, origin: unknown) => {
    if (origin === REMOTE && added!.length && awareness.getLocalState()) sendAwareness([awareness.clientID])
  }
  doc.on('update', onUpdate)
  awareness.on('update', onAwareness)
  awareness.on('change', onPeer)

  const reset = () => {
    ready = false
    stored?.destroy()
    stored = null
  }

  const channel = joinCollab(workspace, issue, {
    onOpen() {
      reset()
      stored = new Y.Doc()
      loaded = 0
    },
    onFrame(type, payload) {
      if (type === UPDATE) {
        Y.applyUpdate(doc, payload, REMOTE)
        if (!ready && stored) {
          Y.applyUpdate(stored, payload)
          loaded++
        }
      } else if (type === AWARENESS) {
        applyAwarenessUpdate(awareness, payload, REMOTE)
      } else if (type === READY && payload.length >= 9 && stored) {
        const flags = payload[0]!
        const lastId = new DataView(payload.buffer, payload.byteOffset + 1, 8).getBigUint64(0)
        const canEdit = (flags & 1) !== 0
        const unseen = Y.encodeStateAsUpdate(doc, Y.encodeStateVector(stored))
        reset()
        ready = true
        if (canEdit && unseen.length > 2) sendUpdate(unseen) // edits made while offline; [0, 0] is an empty update
        const full = canEdit && loaded > COMPACT_AFTER ? Y.encodeStateAsUpdate(doc) : null
        if (full && full.length <= MAX_COMPACT) {
          const body = new Uint8Array(8 + full.length)
          new DataView(body.buffer).setBigUint64(0, lastId)
          body.set(full, 8)
          send(COMPACT, body)
        }
        if (awareness.getLocalState()) sendAwareness([awareness.clientID])
        state = { canEdit, seed: (flags & 2) !== 0, lastId, loaded }
        onState(state)
      } else if (type === SEED && state) {
        state = { ...state, seed: true }
        onState(state)
      }
    },
    onClose() {
      reset()
      removeAwarenessStates(awareness, [...awareness.getStates().keys()].filter((id) => id !== awareness.clientID), REMOTE)
      onState(null)
    },
    onPresence,
  })

  return {
    // Sends the document to store as the description. False when offline (not sent).
    save: (json: string) => send(SAVE, new TextEncoder().encode(json)),
    // False while offline: local changes are kept and sent on reconnect.
    ready: () => ready,
    destroy() {
      removeAwarenessStates(awareness, [awareness.clientID], 'local')
      doc.off('update', onUpdate)
      awareness.off('update', onAwareness)
      awareness.off('change', onPeer)
      channel.leave()
      reset()
    },
  }
}
