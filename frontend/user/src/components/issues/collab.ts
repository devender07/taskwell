import * as Y from 'yjs'
import { applyAwarenessUpdate, encodeAwarenessUpdate, removeAwarenessStates, type Awareness } from 'y-protocols/awareness'
import { refreshAccess } from '@/api'

export type CollabState = { canEdit: boolean; seed: boolean; lastId: bigint; loaded: number }

// Origin of changes applied from the server, so they aren't sent back.
export const REMOTE = 'remote'

const [READY, UPDATE, AWARENESS, COMPACT, SAVE, SEED] = [0, 1, 2, 3, 4, 5]
const COMPACT_AFTER = 100

// Live editing over the issue's collab WebSocket (protocol in docs/rich-text-plan.md). onState gets the server's
// `ready` info, or null when the connection drops. Reconnects with backoff until destroy().
export function connectCollab(path: string, doc: Y.Doc, awareness: Awareness, onState: (s: CollabState | null) => void) {
  let ws: WebSocket | null = null
  let ready = false
  let state: CollabState | null = null
  let delay = 1000
  let timer: ReturnType<typeof setTimeout> | undefined
  let destroyed = false

  const send = (type: number, payload: Uint8Array) => {
    if (!ready || ws?.readyState !== WebSocket.OPEN) return false
    const frame = new Uint8Array(payload.length + 1)
    frame[0] = type
    frame.set(payload, 1)
    ws.send(frame)
    return true
  }
  const sendAwareness = (clients: number[]) => send(AWARENESS, encodeAwarenessUpdate(awareness, clients))

  const onUpdate = (update: Uint8Array, origin: unknown) => {
    if (origin !== REMOTE) send(UPDATE, update)
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

  const connect = () => {
    const sock = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api${path}`)
    sock.binaryType = 'arraybuffer'
    ws = sock
    const stored = new Y.Doc() // what the server replayed, to find local edits it hasn't seen
    let loaded = 0

    sock.onmessage = (e) => {
      if (!(e.data instanceof ArrayBuffer)) return
      const data = new Uint8Array(e.data)
      const payload = data.subarray(1)
      if (data[0] === UPDATE) {
        Y.applyUpdate(doc, payload, REMOTE)
        if (!ready) {
          Y.applyUpdate(stored, payload)
          loaded++
        }
      } else if (data[0] === AWARENESS) {
        applyAwarenessUpdate(awareness, payload, REMOTE)
      } else if (data[0] === READY && payload.length >= 9) {
        const flags = payload[0]!
        const lastId = new DataView(payload.buffer, payload.byteOffset + 1, 8).getBigUint64(0)
        const canEdit = (flags & 1) !== 0
        const unseen = Y.encodeStateAsUpdate(doc, Y.encodeStateVector(stored))
        stored.destroy()
        ready = true
        delay = 1000
        if (canEdit && unseen.length > 2) send(UPDATE, unseen) // edits made while offline; [0, 0] is an empty update
        if (canEdit && loaded > COMPACT_AFTER) {
          const state = Y.encodeStateAsUpdate(doc)
          const body = new Uint8Array(8 + state.length)
          new DataView(body.buffer).setBigUint64(0, lastId)
          body.set(state, 8)
          send(COMPACT, body)
        }
        if (awareness.getLocalState()) sendAwareness([awareness.clientID])
        state = { canEdit, seed: (flags & 2) !== 0, lastId, loaded }
        onState(state)
      } else if (data[0] === SEED && state) {
        state = { ...state, seed: true }
        onState(state)
      }
    }

    sock.onclose = () => {
      if (ws !== sock) return
      const wasReady = ready
      ws = null
      ready = false
      stored.destroy()
      removeAwarenessStates(awareness, [...awareness.getStates().keys()].filter((id) => id !== awareness.clientID), REMOTE)
      onState(null)
      timer = setTimeout(async () => {
        if (!wasReady) await refreshAccess().catch(() => {})
        if (!destroyed) connect()
      }, delay)
      delay = Math.min(delay * 2, 10000)
    }
  }
  connect()

  return {
    // Sends the document to store as the description. False when offline (not sent).
    save: (json: string) => send(SAVE, new TextEncoder().encode(json)),
    destroy() {
      destroyed = true
      clearTimeout(timer)
      removeAwarenessStates(awareness, [awareness.clientID], 'local')
      doc.off('update', onUpdate)
      awareness.off('update', onAwareness)
      awareness.off('change', onPeer)
      const sock = ws
      ws = null
      sock?.close()
    },
  }
}
