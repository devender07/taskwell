// The realtime WebSocket's lifecycle, shared by both apps (docs/realtime-plan.md): connect to /api/realtime, learn the
// connection id from `hello`, ping every 25s and drop a socket that misses its pong (common after an iPad sleeps),
// reconnect with jittered backoff (1s doubling to 10s, reset once a socket stays up 10s), and reconnect at once when
// the tab becomes visible again. Each app handles the messages.

type Options = {
  binary?: boolean // frames arrive as ArrayBuffer
  // A message other than hello and pong (an ArrayBuffer when binary).
  onMessage: (msg: { type: string; [k: string]: unknown } | ArrayBuffer) => void
  // After each hello: again is false the first time, true after a reconnect (refetch what may have been missed);
  // reauthed when the server closed only for a fresh token, so there's no need to spread refetches out.
  onHello: (client: string, again: boolean, reauthed: boolean) => void
  onDrop?: () => void
  // Before reconnecting when a connect failed before hello, which may be a lapsed session: false stops reconnecting.
  stillSignedIn: () => Promise<boolean>
}

export function createSocket({ binary, onMessage, onHello, onDrop, stillSignedIn }: Options) {
  let ws: WebSocket | null = null
  let client = ''
  let running = false
  let opened = false
  let reauthed = false // the last drop was the server asking for a fresh token, not a failure
  let delay = 1000
  let steady: ReturnType<typeof setTimeout> | undefined
  let retry: ReturnType<typeof setTimeout> | undefined
  let ping: ReturnType<typeof setInterval> | undefined
  let pong: ReturnType<typeof setTimeout> | undefined

  // Only after `hello`; callers re-send what they need from onHello.
  const send = (msg: object) => {
    if (client && ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg))
  }

  function receive(data: unknown) {
    if (data instanceof ArrayBuffer) return onMessage(data)
    let msg: { type: string; client?: string }
    try {
      msg = JSON.parse(String(data))
    } catch {
      return
    }
    if (msg.type === 'hello') {
      client = msg.client ?? ''
      clearTimeout(steady)
      steady = setTimeout(() => (delay = 1000), 10_000)
      onHello(client, opened, reauthed)
      opened = true
      clearInterval(ping)
      ping = setInterval(checkAlive, 25_000)
    } else if (msg.type === 'pong') clearTimeout(pong)
    else onMessage(msg)
  }

  function checkAlive() {
    if (ws?.readyState !== WebSocket.OPEN) return
    send({ type: 'ping' })
    clearTimeout(pong)
    pong = setTimeout(() => {
      const sock = ws
      if (!sock) return
      sock.onclose = null
      sock.close()
      reauthed = false
      dropped(sock, true)
    }, 10_000)
  }

  function connect() {
    const sock = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/realtime`)
    if (binary) sock.binaryType = 'arraybuffer'
    ws = sock
    sock.onmessage = (e) => receive(e.data)
    // 4001: the access token expired or the sign-in may be revoked, so refresh before reconnecting, without backing off.
    sock.onclose = (e) => {
      reauthed = e.code === 4001
      dropped(sock, client !== '' && !reauthed)
      if (reauthed) delay = 1000
    }
  }

  function dropped(sock: WebSocket, hello: boolean) {
    if (ws !== sock) return
    ws = null
    client = ''
    clearInterval(ping)
    clearTimeout(pong)
    clearTimeout(steady)
    onDrop?.()
    if (!running) return
    retry = setTimeout(async () => {
      if (!hello && !(await stillSignedIn())) return
      if (running && !ws) connect()
    }, delay * (0.5 + Math.random()))
    delay = Math.min(delay * 2, 10_000)
  }

  function onVisible() {
    if (document.visibilityState !== 'visible' || !running) return
    if (ws) return checkAlive()
    clearTimeout(retry)
    delay = 1000
    connect()
  }

  // Opens the socket; returns the stop.
  function start() {
    running = true
    opened = false
    connect()
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      running = false
      clearTimeout(retry)
      document.removeEventListener('visibilitychange', onVisible)
      const sock = ws
      if (sock) {
        sock.onclose = null
        sock.close()
        dropped(sock, true)
      }
    }
  }

  return {
    start,
    send,
    // Sends a binary frame; false when the socket isn't open.
    sendFrame(data: Uint8Array) {
      if (ws?.readyState !== WebSocket.OPEN) return false
      ws.send(data)
      return true
    },
    connected: () => client !== '',
  }
}
