import { useNavigate } from '@tanstack/react-router'
import { useEffect } from 'react'
import { api, type Chat } from '@/api'
import { subscribePaths } from '@/realtime'

// ponytail: every open tab chimes and notifies; elect one tab (BroadcastChannel) if that gets noisy.

let audio: AudioContext | null = null

const at = (c: Chat) => (c.last_message_at ? Date.parse(c.last_message_at) : 0)

// A short two-note chime, made in the browser so there's no sound file to ship.
export function chime() {
  audio ??= new AudioContext()
  const t = audio.currentTime
  for (const [i, freq] of [880, 1320].entries()) {
    const osc = audio.createOscillator()
    const gain = audio.createGain()
    osc.frequency.value = freq
    gain.gain.setValueAtTime(0.0001, t + i * 0.12)
    gain.gain.exponentialRampToValueAtTime(0.2, t + i * 0.12 + 0.02)
    gain.gain.exponentialRampToValueAtTime(0.0001, t + i * 0.12 + 0.3)
    osc.connect(gain).connect(audio.destination)
    osc.start(t + i * 0.12)
    osc.stop(t + i * 0.12 + 0.3)
  }
}

// Chrome plays sound and asks for notification permission only after the user does something on the page, so
// both happen on the first click.
export function askOnFirstClick() {
  const first = () => {
    audio ??= new AudioContext()
    void audio.resume()
    if ('Notification' in window && Notification.permission === 'default') void Notification.requestPermission()
  }
  window.addEventListener('pointerdown', first, { once: true })
  return () => window.removeEventListener('pointerdown', first)
}

// A desktop notification for the chat's newest message; clicking it opens the chat.
export function notify(chat: Chat, open: () => void) {
  if (!('Notification' in window) || Notification.permission !== 'granted' || !chat.last_message) return
  const who = chat.last_message.author?.name
  const n = new Notification(chat.kind === 'direct' || !who ? (who ?? chat.name) : `${who} in ${chat.name}`, {
    body: chat.last_message.excerpt,
    icon: '/favicon.svg',
    tag: chat.id, // a newer message in the same chat replaces the notification
  })
  n.onclick = () => {
    window.focus()
    open()
    n.close()
  }
}

// Chimes when a chat in the workspace gets a message from someone else, with a desktop notification unless that
// chat is on screen: the chat list is refetched on each chat event, and a chat whose last message is newer than
// before has a new one.
export function useChatAlerts(slug: string | undefined, userId: string) {
  const navigate = useNavigate()
  useEffect(askOnFirstClick, [])
  useEffect(() => {
    if (!slug) return
    const path = `/workspaces/${encodeURIComponent(slug)}/chats`
    let last: Map<string, number> | null = null
    let stale = false
    const check = () =>
      api<Chat[]>(path).then((chats) => {
        if (stale) return
        const before = last
        last = new Map(chats.map((c) => [c.id, at(c)]))
        if (!before) return
        const chat = chats
          .filter((c) => c.last_message && c.last_message.author?.id !== userId && at(c) > (before.get(c.id) ?? 0))
          .sort((a, b) => at(b) - at(a))[0]
        if (!chat) return
        chime()
        const onScreen = document.visibilityState === 'visible' && document.hasFocus() && location.pathname.endsWith(`/inbox/c/${chat.id}`)
        if (!onScreen) // the chat being looked at gets only the sound
          notify(chat, () => navigate({ to: '/$workspace/inbox/c/$chatId', params: { workspace: slug, chatId: chat.id } }))
      }, () => {})
    check()
    const unsubscribe = subscribePaths([path], check)
    return () => {
      stale = true
      unsubscribe()
    }
  }, [slug, userId, navigate])
}
