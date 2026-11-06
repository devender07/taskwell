import { useSyncExternalStore } from 'react'

export type Theme = 'system' | 'light' | 'dark'
const KEY = 'taskwell:theme' // read before first paint by public/theme.js
const EVENT = 'taskwell:theme'
const dark = matchMedia('(prefers-color-scheme: dark)')

function saved(): Theme {
  try {
    const t = localStorage.getItem(KEY)
    return t === 'light' || t === 'dark' ? t : 'system'
  } catch {
    return 'system'
  }
}

export function setTheme(t: Theme) {
  try {
    if (t === 'system') localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, t)
  } catch {}
  if (t === 'system') delete document.documentElement.dataset.theme
  else document.documentElement.dataset.theme = t
  dispatchEvent(new Event(EVENT))
}

function subscribe(fn: () => void) {
  addEventListener(EVENT, fn)
  dark.addEventListener('change', fn)
  return () => {
    removeEventListener(EVENT, fn)
    dark.removeEventListener('change', fn)
  }
}

// The saved choice, and what's showing (system resolved).
export function useTheme() {
  const theme = useSyncExternalStore(subscribe, saved)
  const shown = useSyncExternalStore(subscribe, () => (theme === 'system' ? (dark.matches ? 'dark' : 'light') : theme))
  return { theme, shown }
}
