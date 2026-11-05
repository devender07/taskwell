import { useEffect, useRef, useState } from 'react'
import { clockTime, fullTime } from '@taskwell/shared/time'

const DELAY_MS = 500

// Runs `save` 500ms after the last schedule(), one save at a time; flush() saves now (closing). `save` returns
// whether it worked (or had nothing to do).
export function useAutosave(save: () => Promise<boolean>) {
  const latest = useRef(save)
  latest.current = save
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const chain = useRef(Promise.resolve(true))
  const [saving, setSaving] = useState(false)

  function flush() {
    clearTimeout(timer.current)
    chain.current = chain.current.then(async () => {
      setSaving(true)
      try {
        return await latest.current()
      } finally {
        setSaving(false)
      }
    })
    return chain.current
  }
  const schedule = () => {
    clearTimeout(timer.current)
    timer.current = setTimeout(flush, DELAY_MS)
  }
  useEffect(() => () => clearTimeout(timer.current), [])
  return { saving, schedule, flush }
}


// "Saving…", or when it was last saved (and by whom, when someone else): the time today, else the date and time.
export function SavedAt({ saving, at, by }: { saving: boolean; at: string | null; by?: string }) {
  if (saving) return <span className="text-caption text-secondary" role="status">Saving…</span>
  if (!at) return null
  const d = new Date(at)
  const today = d.toDateString() === new Date().toDateString()
  return (
    <span className="text-caption text-secondary" role="status">
      Last saved <time dateTime={at} title={fullTime(at)}>{today ? clockTime(d) : `${d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}, ${clockTime(d)}`}</time>
      {by ? ` by ${by}` : null}
    </span>
  )
}
