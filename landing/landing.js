document.documentElement.classList.add('js')

const header = document.querySelector('.site-header')
const onScroll = () => header.classList.toggle('is-scrolled', window.scrollY > 8)
window.addEventListener('scroll', onScroll, { passive: true })
onScroll()

// Mobile menu
const toggle = document.querySelector('.nav-toggle')
const nav = document.getElementById('site-nav')
const setMenu = (open) => {
  toggle.setAttribute('aria-expanded', String(open))
  toggle.setAttribute('aria-label', open ? 'Close menu' : 'Open menu')
  nav.classList.toggle('is-open', open)
}
toggle.addEventListener('click', () => setMenu(toggle.getAttribute('aria-expanded') !== 'true'))
nav.addEventListener('click', (e) => e.target.closest('a') && setMenu(false))
document.addEventListener('keydown', (e) => e.key === 'Escape' && setMenu(false))

// Reveal sections, and play the AI test run once it scrolls into view
const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches
const playRun = (terminal) => {
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
const tabs = [...document.querySelectorAll('[role="tab"]')]
const select = (tab) => {
  for (const t of tabs) {
    const on = t === tab
    t.setAttribute('aria-selected', String(on))
    t.tabIndex = on ? 0 : -1
    document.getElementById(t.getAttribute('aria-controls')).hidden = !on
  }
  tab.focus()
}
tabs.forEach((tab, i) => {
  tab.addEventListener('click', () => select(tab))
  tab.addEventListener('keydown', (e) => {
    const step = { ArrowRight: 1, ArrowLeft: -1 }[e.key]
    if (step) select(tabs[(i + step + tabs.length) % tabs.length])
  })
})

// The board card in progress moves to Done every few seconds, like a teammate finishing work
const live = document.querySelector('.card-live')
if (live && !reduced) {
  const cols = document.querySelectorAll('.board-col')
  const progress = cols[1]
  const done = cols[2]
  const typing = live.querySelector('.typing')
  setInterval(() => {
    const finished = live.parentElement === done
    const target = finished ? progress : done
    live.classList.toggle('is-done', !finished)
    typing.textContent = finished ? 'Asha is editing…' : 'Completed just now'
    target.insertBefore(live, target.children[1])
    for (const col of cols) col.querySelector('.count').textContent = col.querySelectorAll('.card-mock').length
  }, 4000)
}

// Copy the MCP command
document.querySelectorAll('[data-copy]').forEach((button) => {
  button.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(button.dataset.copy)
      button.textContent = 'Copied'
    } catch {
      button.textContent = 'Press Ctrl+C'
    }
    setTimeout(() => (button.textContent = 'Copy'), 2000)
  })
})

document.getElementById('year').textContent = new Date().getFullYear()
