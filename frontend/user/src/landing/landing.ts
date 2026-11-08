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
const playRun = (terminal: Element) => {
  terminal.querySelectorAll('[data-step]').forEach((step, i) => {
    setTimeout(() => step.classList.add('is-shown'), reduced ? 0 : 500 + i * 600)
  })
}
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

// The board card in progress moves to Done every few seconds, like a teammate finishing work
const live = document.querySelector('.card-live')
if (live && !reduced) {
  const cols = document.querySelectorAll('.board-col')
  const progress = cols[1]!
  const done = cols[2]!
  const typing = live.querySelector('.typing')!
  setInterval(() => {
    const finished = live.parentElement === done
    const target = finished ? progress : done
    live.classList.toggle('is-done', !finished)
    typing.textContent = finished ? 'Asha is editing…' : 'Completed just now'
    target.insertBefore(live, target.children[1] ?? null)
    for (const col of cols) col.querySelector('.count')!.textContent = String(col.querySelectorAll('.card-mock').length)
  }, 4000)
}

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

document.getElementById('year')!.textContent = String(new Date().getFullYear())
