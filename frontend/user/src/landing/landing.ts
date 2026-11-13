import '@taskwell/shared/tokens.css'
import '@taskwell/shared/utilities.css'
import '@taskwell/shared/button.css'
import './landing.css'
import { apiUrl } from '@taskwell/shared/api-url'

document.documentElement.classList.add('js')

const header = document.querySelector<HTMLElement>('.site-header')!
const onScroll = () => header.classList.toggle('is-scrolled', window.scrollY > 8)
window.addEventListener('scroll', onScroll, { passive: true })
onScroll()

// Mobile menu
const toggle = document.querySelector<HTMLButtonElement>('.nav-toggle')!
const nav = document.getElementById('site-nav')!
const setMenu = (open: boolean) => {
  toggle.setAttribute('aria-expanded', String(open))
  toggle.setAttribute('aria-label', open ? 'Close menu' : 'Open menu')
  nav.classList.toggle('is-open', open)
}
toggle.addEventListener('click', () => setMenu(toggle.getAttribute('aria-expanded') !== 'true'))
nav.addEventListener('click', (e) => (e.target as Element).closest('a') && setMenu(false))
document.addEventListener('keydown', (e) => e.key === 'Escape' && setMenu(false))

// Reveal sections, and play the AI test run once it scrolls into view
const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches
const replay = document.querySelector<HTMLButtonElement>('.replay')!
const playRun = (terminal: Element) => {
  const steps = terminal.querySelectorAll('[data-step]')
  replay.hidden = true
  steps.forEach((step) => step.classList.remove('is-shown'))
  steps.forEach((step, i) => {
    setTimeout(() => {
      step.classList.add('is-shown')
      if (i === steps.length - 1) replay.hidden = false
    }, reduced ? 0 : 500 + i * 600)
  })
}
replay.addEventListener('click', () => playRun(replay.closest('.terminal')!))

// Highlight the nav link of the section in view
const links = new Map([...nav.querySelectorAll<HTMLAnchorElement>('a[href^="#"]')].map((a) => [a.hash.slice(1), a]))
const spy = new IntersectionObserver(
  (entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue
      for (const a of links.values()) a.removeAttribute('aria-current')
      links.get(entry.target.id)?.setAttribute('aria-current', 'true')
    }
  },
  { rootMargin: '-50% 0px -50% 0px' },
)
document.querySelectorAll('main > section[id]').forEach((s) => spy.observe(s))
const observer = new IntersectionObserver(
  (entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue
      entry.target.classList.add('is-visible')
      if (entry.target.classList.contains('terminal')) playRun(entry.target)
      observer.unobserve(entry.target)
    }
  },
  { threshold: 0.15 },
)
document.querySelectorAll('.reveal').forEach((el) => observer.observe(el))

// Workflow tabs (arrow keys move between them, as the ARIA tabs pattern expects)
const tabs = [...document.querySelectorAll<HTMLButtonElement>('[role="tab"]')]
const select = (tab: HTMLButtonElement) => {
  for (const t of tabs) {
    const on = t === tab
    t.setAttribute('aria-selected', String(on))
    t.tabIndex = on ? 0 : -1
    document.getElementById(t.getAttribute('aria-controls')!)!.hidden = !on
  }
  tab.focus()
}
tabs.forEach((tab, i) => {
  tab.addEventListener('click', () => select(tab))
  tab.addEventListener('keydown', (e) => {
    const step = ({ ArrowRight: 1, ArrowLeft: -1 } as Record<string, number>)[e.key]
    if (step) select(tabs[(i + step + tabs.length) % tabs.length]!)
  })
})

// The hero board is playable: drag cards between columns (arrow keys too), press C or "New issue" to add one.
// Until the visitor touches it, the card in progress moves to Done every few seconds, like a teammate finishing work.
const frame = document.querySelector<HTMLElement>('.app-frame')!
const board = frame.querySelector<HTMLElement>('.board')!
const cols = [...board.querySelectorAll<HTMLElement>('.board-col')]
const colName = (col: Element) => col.querySelector('.board-col-title')!.textContent!.replace(/\d+$/, '').trim()
const statusLine = document.getElementById('board-status')!
const cards = () => board.querySelectorAll<HTMLElement>('.card-mock:not(.card-new)')
const counts = () => cols.forEach((c) => (c.querySelector('.count')!.textContent = String(c.querySelectorAll('.card-mock:not(.card-new)').length)))
const live = board.querySelector<HTMLElement>('.card-live')!

const ready = (card: HTMLElement) => {
  card.draggable = true
  card.tabIndex = 0
  card.setAttribute('aria-label', `${card.querySelector('.issue-key')!.textContent}: ${card.querySelector('p')!.textContent}`)
}
cards().forEach(ready)

const autoplay = reduced
  ? 0
  : setInterval(() => {
      const target = live.parentElement === cols[2] ? cols[1]! : cols[2]!
      live.querySelector('.typing')!.textContent = target === cols[2] ? 'Completed just now' : 'Asha is editing…'
      place(live, target, target.querySelector('.card-mock'), false)
    }, 4000)
let touched = false
const touch = () => {
  if (touched) return
  touched = true
  clearInterval(autoplay)
  live.querySelector('.typing')?.remove()
  live.classList.remove('card-live')
}

function place(card: HTMLElement, col: HTMLElement, before: Element | null, say = true) {
  col.insertBefore(card, before)
  card.classList.toggle('is-done', col === cols[2])
  counts()
  if (say) statusLine.textContent = `${card.querySelector('.issue-key')!.textContent} moved to ${colName(col)}`
}

let dragged: HTMLElement | null = null
board.addEventListener('dragstart', (e) => {
  dragged = (e.target as Element).closest<HTMLElement>('.card-mock')
  if (!dragged || !e.dataTransfer) return
  touch()
  e.dataTransfer.effectAllowed = 'move'
  e.dataTransfer.setData('text/plain', dragged.getAttribute('aria-label')!)
  requestAnimationFrame(() => dragged?.classList.add('is-dragging'))
})
board.addEventListener('dragend', () => {
  dragged?.classList.remove('is-dragging')
  dragged = null
  cols.forEach((c) => c.classList.remove('is-over'))
})
for (const col of cols) {
  col.addEventListener('dragover', (e) => {
    if (!dragged) return
    e.preventDefault()
    cols.forEach((c) => c.classList.toggle('is-over', c === col))
  })
  col.addEventListener('drop', (e) => {
    e.preventDefault()
    if (!dragged) return
    const below = [...col.querySelectorAll<HTMLElement>('.card-mock:not(.is-dragging)')].find((c) => {
      const r = c.getBoundingClientRect()
      return e.clientY < r.top + r.height / 2
    })
    place(dragged, col, below ?? null)
  })
}
board.addEventListener('keydown', (e) => {
  const card = (e.target as Element).closest<HTMLElement>('.card-mock:not(.card-new)')
  const step = ({ ArrowRight: 1, ArrowLeft: -1 } as Record<string, number>)[e.key]
  if (!card || !step) return
  const to = cols[cols.indexOf(card.parentElement as HTMLElement) + step]
  if (!to) return
  e.preventDefault()
  touch()
  place(card, to, to.querySelector('.card-mock'))
  card.focus()
})

let next = 143
const newButton = frame.querySelector<HTMLButtonElement>('.frame-new')!
function quickCreate() {
  touch()
  const open = board.querySelector<HTMLInputElement>('.card-new input')
  if (open) return open.focus()
  const todo = cols[0]!
  const form = document.createElement('form')
  form.className = 'card-mock card-new'
  form.innerHTML = '<span class="issue-key"></span><input aria-label="Issue title" placeholder="Issue title" maxlength="80" autocomplete="off" /><span class="text-caption text-secondary">Enter to create, Esc to cancel</span>'
  form.querySelector('.issue-key')!.textContent = `ENG-${next}`
  const input = form.querySelector('input')!
  const close = () => {
    form.remove()
    newButton.focus()
  }
  form.addEventListener('submit', (e) => {
    e.preventDefault()
    const title = input.value.trim()
    if (!title) return close()
    const card = document.createElement('div')
    card.className = 'card-mock is-new'
    card.innerHTML = '<span class="issue-key"></span><p></p><span class="tags"><i class="tag">New</i></span>'
    card.querySelector('.issue-key')!.textContent = `ENG-${next++}`
    card.querySelector('p')!.textContent = title
    ready(card)
    form.replaceWith(card)
    counts()
    statusLine.textContent = `${card.getAttribute('aria-label')} created in Todo`
    card.focus()
  })
  input.addEventListener('keydown', (e) => e.key === 'Escape' && (e.stopPropagation(), close()))
  input.addEventListener('blur', () => setTimeout(() => form.isConnected && !input.value.trim() && form.remove()))
  todo.insertBefore(form, todo.querySelector('.card-mock'))
  if (frame.getBoundingClientRect().top > innerHeight || frame.getBoundingClientRect().bottom < 0) frame.scrollIntoView({ block: 'center' })
  input.focus({ preventScroll: true })
}
newButton.addEventListener('click', quickCreate)
document.addEventListener('keydown', (e) => {
  if (e.key.toLowerCase() !== 'c' || e.metaKey || e.ctrlKey || e.altKey || e.repeat) return
  if ((e.target as Element).closest('input, textarea, [contenteditable]')) return
  e.preventDefault()
  quickCreate()
})

// Copy the MCP command
document.querySelectorAll<HTMLButtonElement>('[data-copy]').forEach((button) => {
  button.addEventListener('click', async () => {
    const label = button.ariaLabel
    try {
      await navigator.clipboard.writeText(button.dataset.copy!)
      button.dataset.copied = ''
      button.ariaLabel = button.title = 'Copied'
    } catch {
      button.ariaLabel = button.title = 'Press Ctrl+C'
    }
    setTimeout(() => {
      delete button.dataset.copied
      button.ariaLabel = button.title = label!
    }, 2000)
  })
})

// Signed in (the access cookie, or one refresh of it): swap sign-up buttons for a way back into the app.
async function currentUser() {
  const get = (path: string, init?: RequestInit) => fetch(apiUrl(path), { credentials: 'include', ...init })
  const me = () => get('/api/me')
  let res = await me()
  if (res.status === 401 && (await get('/api/auth/refresh', { method: 'POST' })).ok) res = await me()
  return res.ok ? ((await res.json()) as { name: string }) : null
}
currentUser()
  .then((user) => {
    if (!user) return
    document.querySelectorAll<HTMLElement>('[data-signed-out]').forEach((el) => (el.hidden = true))
    document.querySelectorAll<HTMLElement>('[data-signed-in]').forEach((el) => (el.hidden = false))
    fetch(apiUrl('/api/workspaces'), { credentials: 'include' })
      .then((res) => (res.ok ? (res.json() as Promise<{ slug: string }[]>) : []))
      .then(([last]) => {
        const href = last ? `/${encodeURIComponent(last.slug)}` : '/new-workspace'
        document.querySelectorAll<HTMLAnchorElement>('a[data-signed-in]').forEach((a) => (a.href = href))
      })
      .catch(() => {}) // the links keep /signin, which sends a signed-in user on to their workspace
    document.getElementById('greeting')!.textContent = `Welcome back, ${user.name.split(' ')[0]}`
  })
  .catch(() => {}) // API unreachable: the page stays as it is for visitors

fetch(apiUrl('/api/beta'))
  .then((res) => (res.ok ? (res.json() as Promise<{ beta: boolean }>) : { beta: false }))
  .then(({ beta }) => beta && document.querySelectorAll<HTMLElement>('[data-paid]').forEach((el) => (el.hidden = true)))
  .catch(() => {})

document.getElementById('year')!.textContent = String(new Date().getFullYear())
