import { useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type RefObject } from 'react'
import type { Issue } from '@/api'

// Where a dragged issue would land: a group, and with manual order the slot in it (an index among the group's other
// issues); null when only the group matters.
export type DropAt = { group: string; index: number | null }
export type Drag = { issue: Issue; at: DropAt | null; width: number }

type Options = {
  targetAt: (x: number, y: number, issue: Issue, from: string) => DropAt | null
  onDrop: (issue: Issue, from: string, at: DropAt) => void
  // Scrolled sideways near its edges (the board); without it the window scrolls up and down.
  scroller?: RefObject<HTMLElement | null>
}

const HOLD_MS = 150
const SLOP_PX = 6
const EDGE_PX = 56

// Issues drag with pointer events so mouse, pen and touch all work; touch needs a short hold first, so a swipe still
// scrolls. The host renders the ghost (`ghostRef`) and marks targets from `drag`; the handlers are stable, so memoized
// cards and rows don't re-render while dragging.
export function useIssueDrag(options: Options) {
  const [drag, setDrag] = useState<Drag | null>(null)
  const opts = useRef(options)
  opts.current = options
  const dragging = useRef(false)
  const suppressClick = useRef(false)
  const pos = useRef({ x: 0, y: 0 })
  const ghost = useRef<HTMLElement | null>(null)

  useEffect(() => {
    const block = (e: TouchEvent) => dragging.current && e.preventDefault()
    document.addEventListener('touchmove', block, { passive: false })
    return () => document.removeEventListener('touchmove', block)
  }, [])

  const handlers = useMemo(() => {
    const place = () => {
      if (ghost.current) ghost.current.style.transform = `translate(${pos.current.x}px, ${pos.current.y}px)`
    }
    const same = (a: DropAt | null, b: DropAt | null) => a?.group === b?.group && a?.index === b?.index
    const retarget = (at: DropAt | null) => setDrag((d) => (d && !same(d.at, at) ? { ...d, at } : d))

    function press(e: ReactPointerEvent<HTMLElement>, issue: Issue, from: string) {
      suppressClick.current = false
      if (!issue.can_edit || !e.isPrimary || e.button !== 0) return
      const { pointerId, pointerType } = e
      const start = { x: e.clientX, y: e.clientY }
      const rect = e.currentTarget.getBoundingClientRect()
      const offset = { x: start.x - rect.left, y: start.y - rect.top }
      const targetAt = (x: number, y: number) => opts.current.targetAt(x, y, issue, from)
      pos.current = { x: rect.left, y: rect.top }

      const begin = () => {
        dragging.current = true
        suppressClick.current = true
        setDrag({ issue, at: targetAt(last.x, last.y), width: rect.width })
      }
      const timer = pointerType === 'touch' ? setTimeout(begin, HOLD_MS) : undefined
      const last = { x: start.x, y: start.y }
      let frame = 0
      const autoScroll = () => {
        frame = 0
        if (!dragging.current) return
        const el = opts.current.scroller?.current
        if (el) {
          const { left, right } = el.getBoundingClientRect()
          const speed = last.x < left + EDGE_PX ? -12 : last.x > right - EDGE_PX ? 12 : 0
          if (!speed) return
          el.scrollLeft += speed
        } else {
          const speed = last.y < EDGE_PX ? -12 : last.y > window.innerHeight - EDGE_PX ? 12 : 0
          if (!speed) return
          window.scrollBy(0, speed)
        }
        retarget(targetAt(last.x, last.y))
        frame = requestAnimationFrame(autoScroll)
      }

      const onMove = (ev: PointerEvent) => {
        if (ev.pointerId !== pointerId) return
        if (!dragging.current) {
          if (Math.hypot(ev.clientX - start.x, ev.clientY - start.y) <= SLOP_PX) return
          if (pointerType === 'touch') return end()
          begin()
        }
        pos.current = { x: ev.clientX - offset.x, y: ev.clientY - offset.y }
        place()
        last.x = ev.clientX
        last.y = ev.clientY
        retarget(targetAt(ev.clientX, ev.clientY))
        if (!frame) frame = requestAnimationFrame(autoScroll)
      }
      const onUp = (ev: PointerEvent) => {
        if (ev.pointerId !== pointerId) return
        const at = dragging.current ? targetAt(ev.clientX, ev.clientY) : null
        end()
        if (at) opts.current.onDrop(issue, from, at)
      }
      const onCancel = (ev: PointerEvent) => ev.pointerId === pointerId && end()

      function end() {
        clearTimeout(timer)
        cancelAnimationFrame(frame)
        window.removeEventListener('pointermove', onMove)
        window.removeEventListener('pointerup', onUp)
        window.removeEventListener('pointercancel', onCancel)
        if (dragging.current) setDrag(null)
        dragging.current = false
      }
      window.addEventListener('pointermove', onMove)
      window.addEventListener('pointerup', onUp)
      window.addEventListener('pointercancel', onCancel)
    }

    return {
      press,
      click: (e: ReactMouseEvent<HTMLElement>) => {
        if (!suppressClick.current || e.detail === 0) return
        e.preventDefault()
        suppressClick.current = false
      },
      contextMenu: (e: ReactMouseEvent<HTMLElement>) => dragging.current && e.preventDefault(),
      ghostRef: (el: HTMLElement | null) => {
        ghost.current = el
        place()
      },
    }
  }, [])

  return { drag, ...handlers }
}

// The slot under `y` among the issues rendered in `container` other than the dragged one: before the first whose
// middle is below it.
export function slotAt(container: Element, y: number, id: string) {
  const items = [...container.querySelectorAll<HTMLElement>('[data-issue]')].filter((el) => el.dataset.issue !== id)
  const i = items.findIndex((el) => {
    const r = el.getBoundingClientRect()
    return y < r.top + r.height / 2
  })
  return i < 0 ? items.length : i
}

// The rendered issue a drop line attaches to: the one at the slot (line above it) or, past the end, the last (below).
export function dropMark(issues: Issue[], drag: Drag | null, group: string) {
  if (!drag?.at || drag.at.group !== group || drag.at.index === null) return null
  const rest = issues.filter((i) => i.id !== drag.issue.id)
  const at = rest[drag.at.index]
  if (at) return { id: at.id, side: 'before' as const }
  const last = rest.at(-1)
  return last ? { id: last.id, side: 'after' as const } : null
}
