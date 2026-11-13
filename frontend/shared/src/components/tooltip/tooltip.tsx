import { useEffect, useLayoutEffect, useRef, useState, type FocusEvent, type PointerEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { computePosition, type Placement } from '../popover/position'
import './tooltip.css'

const DELAY_MS = 400

// Only one tooltip shows at a time: hovering one while another element has focus replaces it.
let hideShown: (() => void) | null = null

type Handlers = {
  onPointerEnter: (e: PointerEvent<HTMLElement>) => void
  onPointerLeave: () => void
  onPointerDown: () => void
  onFocus: (e: FocusEvent<HTMLElement>) => void
  onBlur: () => void
}

// A short label shown on hover (after a moment) or keyboard focus, portaled out of any clipping parent and kept inside
// the viewport (flipping below when there's no room above), with an arrow pointing at the element. Spread `handlers` on the element; render `tooltip` anywhere.
// No text, no tooltip. The element still needs its own accessible name: this repeats it for sighted users.
export function useTooltip(text: string | undefined, placement: Placement = 'top'): { handlers: Handlers | undefined; tooltip: ReactNode } {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])

  const hide = () => {
    clearTimeout(timer.current)
    setAnchor(null)
  }
  const show = (el: HTMLElement, delay: number) => {
    clearTimeout(timer.current)
    timer.current = setTimeout(() => {
      if (hideShown !== hide) hideShown?.()
      hideShown = hide
      setAnchor(el)
    }, delay)
  }

  if (!text) return { handlers: undefined, tooltip: null }
  return {
    handlers: {
      onPointerEnter: (e) => e.pointerType === 'mouse' && show(e.currentTarget, DELAY_MS),
      onPointerLeave: hide,
      onPointerDown: hide,
      onFocus: (e) => e.currentTarget.matches(':focus-visible') && show(e.currentTarget, 0),
      onBlur: hide,
    },
    tooltip: anchor ? <Panel anchor={anchor} text={text} placement={placement} onDone={hide} /> : null,
  }
}

// Render-prop form for any element: <Tooltip text="Archive">{(props) => <button {...props}>…</button>}</Tooltip>.
export function Tooltip({ text, placement, children }: { text?: string; placement?: Placement; children: (props: Partial<Handlers>) => ReactNode }) {
  const { handlers, tooltip } = useTooltip(text, placement)
  return (
    <>
      {children(handlers ?? {})}
      {tooltip}
    </>
  )
}

function Panel({ anchor, text, placement, onDone }: { anchor: HTMLElement; text: string; placement: Placement; onDone: () => void }) {
  const ref = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    const panel = ref.current
    if (!panel) return
    const place = () => {
      if (!anchor.isConnected) return onDone()
      const { top, left, side, arrow } = computePosition(
        anchor.getBoundingClientRect(),
        { width: panel.offsetWidth, height: panel.offsetHeight },
        placement,
        { width: innerWidth, height: innerHeight },
        8,
        8,
        8,
      )
      panel.style.top = `${top}px`
      panel.style.left = `${left}px`
      panel.style.setProperty('--arrow', `${arrow}px`)
      panel.dataset.side = side
    }
    place()
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && onDone()
    addEventListener('scroll', place, true)
    addEventListener('resize', place)
    addEventListener('keydown', esc)
    return () => {
      removeEventListener('scroll', place, true)
      removeEventListener('resize', place)
      removeEventListener('keydown', esc)
    }
  }, [anchor, placement, onDone])

  // A modal <dialog> sits in the top layer, above anything portaled to <body>.
  return createPortal(
    <div ref={ref} role="tooltip" className="tooltip">
      {text}
      <span className="tooltip-arrow" aria-hidden />
    </div>,
    anchor.closest('dialog') ?? document.body,
  )
}
