import { useLoaderData, useLocation, useMatch, useParams } from '@tanstack/react-router'
import { Suspense, createContext, lazy, use, useEffect, useEffectEvent, useMemo, useState, type ReactNode } from 'react'
import { Dialog } from '@tether/shared/dialog'
import { can } from '@/api'
import type { Prefill } from '@/components/issues/issue-form'
import { CommandPalette, type Mode } from './command-palette'
import './shortcuts.css'

const QuickCreate = lazy(() => import('./quick-create'))

export const isApple = typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent)
export const paletteKey = isApple ? '⌘K' : 'Ctrl+K'

const ISSUE_KEYS: Record<string, Mode> = { a: 'assignee', s: 'status', p: 'priority', l: 'labels' }
const HELP = [
  [paletteKey, 'Open the command palette'],
  ['/', 'Search'],
  ['C', 'Create issue'],
  ['S', 'Change status (on an issue)'],
  ['A', 'Assign (on an issue)'],
  ['P', 'Set priority (on an issue)'],
  ['L', 'Change labels (on an issue)'],
  ['P / F / S', 'Mark passed, failed or skipped (in a manual test run)'],
  ['Enter', 'Send a message (Shift+Enter for a new line)'],
  ['↑', 'Edit your last message (in an empty composer)'],
  ['?', 'Show keyboard shortcuts'],
  ['Esc', 'Close'],
]

type Actions = { openPalette: () => void; openCreate: () => void; createPrefilled: (prefill: Prefill) => void }
const ShortcutsContext = createContext<Actions>({ openPalette() {}, openCreate() {}, createPrefilled() {} })
export const useShortcuts = () => use(ShortcutsContext)

// Single-key shortcuts (C, /, ?, S, A, P, L) can be turned off in Settings → Profile (WCAG 2.1.4); ⌘K stays.
const SINGLE_KEYS = 'tether:single-key-shortcuts'
export function singleKeyShortcuts() {
  try {
    return localStorage.getItem(SINGLE_KEYS) !== 'off'
  } catch {
    return true
  }
}
export function setSingleKeyShortcuts(on: boolean) {
  try {
    if (on) localStorage.removeItem(SINGLE_KEYS)
    else localStorage.setItem(SINGLE_KEYS, 'off')
  } catch {}
}

export const typing = (t: EventTarget | null) => t instanceof HTMLElement && (t.isContentEditable || Boolean(t.closest('input, textarea, select')))

export function Shortcuts({ children }: { children: ReactNode }) {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const issue = useMatch({ from: '/_app/$workspace/_main/issue/$identifier', shouldThrow: false })?.loaderData
  const pageProject = useParams({ strict: false, select: (p) => p.key })
  const pathname = useLocation({ select: (l) => l.pathname })
  const [palette, setPalette] = useState<Mode | null>(null)
  const [creating, setCreating] = useState<{ prefill?: Prefill } | null>(null)
  const [help, setHelp] = useState(false)
  const [path, setPath] = useState(pathname)
  const canCreate = can(workspace, 'issues.create')

  if (pathname !== path) {
    setPath(pathname)
    setPalette(null)
    setCreating(null)
  }

  const actions = useMemo<Actions>(() => ({ openPalette: () => setPalette('root'), openCreate: () => setCreating({}), createPrefilled: (prefill) => setCreating({ prefill }) }), [])

  const onKey = useEffectEvent((e: KeyboardEvent) => {
    if (e.defaultPrevented || e.isComposing) return
    const open = document.querySelector('dialog[open]')
    if (e.key.toLowerCase() === 'k' && (isApple ? e.metaKey : e.ctrlKey) && !e.altKey && !e.shiftKey) {
      if (open && !open.classList.contains('palette')) return
      e.preventDefault()
      setPalette((p) => (p ? null : 'root'))
      return
    }
    if (open || e.metaKey || e.ctrlKey || e.altKey || typing(e.target) || !singleKeyShortcuts()) return
    const mode = ISSUE_KEYS[e.key]
    if (e.key === 'c' && canCreate) setCreating({})
    else if (e.key === '/') setPalette('root')
    else if (e.key === '?') setHelp(true)
    else if (mode && issue?.issue.can_edit) setPalette(mode)
    else return
    e.preventDefault()
  })

  useEffect(() => {
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  return (
    <ShortcutsContext value={actions}>
      {children}
      <CommandPalette
        workspace={workspace}
        mode={palette}
        issue={issue}
        projectKey={pageProject ?? issue?.issue.project.key}
        onCreate={
          canCreate
            ? () => {
                setPalette(null)
                setCreating({})
              }
            : undefined
        }
        onClose={() => setPalette(null)}
      />
      {canCreate ? (
        <Dialog open={creating !== null} onClose={() => setCreating(null)} title="New issue">
          <Suspense fallback={<p className="text-body-sm text-secondary">Loading…</p>}>
            <QuickCreate workspace={workspace} projectKey={pageProject ?? issue?.issue.project.key} prefill={creating?.prefill} />
          </Suspense>
        </Dialog>
      ) : null}
      <Dialog open={help} onClose={() => setHelp(false)} title="Keyboard shortcuts">
        <dl className="grid gap-3 m-0">
          {HELP.map(([key, label]) => (
            <div key={key} className="flex items-center justify-between gap-4">
              <dt className="text-body-sm">{label}</dt>
              <dd className="m-0">
                <kbd className="kbd">{key}</kbd>
              </dd>
            </div>
          ))}
        </dl>
      </Dialog>
    </ShortcutsContext>
  )
}
