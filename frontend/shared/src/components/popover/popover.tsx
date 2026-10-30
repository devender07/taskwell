import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { computePosition, type Placement } from './position'
import './popover.css'

export type { Placement }

type TriggerProps = {
  ref: (el: HTMLElement | null) => void
  onClick: () => void
  'aria-expanded': boolean
  'aria-controls': string
}

type PopoverProps = {
  trigger: (props: TriggerProps) => ReactNode
  children: ReactNode | ((close: () => void) => ReactNode)
  placement?: Placement
  arrow?: boolean
  'aria-label': string
  className?: string
}

const EXIT_MS = 120 // matches the fade-out in popover.css

// Floating panel anchored to its trigger, rendered in a portal on <body>. Flips and clamps to stay in the
// viewport, fades in and out, and closes on outside click, Esc, or focus leaving it.
export function Popover({ trigger, children, placement = 'bottom-start', arrow = true, className, ...props }: PopoverProps) {
  const id = useId()
  const [state, setState] = useState<'closed' | 'open' | 'closing'>('closed')
  const triggerRef = useRef<HTMLElement | null>(null)
  const panelRef = useRef<HTMLDivElement>(null)

  const close = useCallback(() => setState((s) => (s === 'open' ? 'closing' : s)), [])
  const toggle = () => setState((s) => (s === 'open' ? 'closing' : 'open'))

  useEffect(() => {
    if (state !== 'closing') return
    const t = setTimeout(() => setState('closed'), EXIT_MS)
    return () => clearTimeout(t)
  }, [state])

  // Position before paint, then follow scroll and resize while open.
  useLayoutEffect(() => {
    const panel = panelRef.current
    const anchor = triggerRef.current
    if (state !== 'open' || !panel || !anchor) return
    const place = () => {
      const { top, left, side, arrow } = computePosition(
        anchor.getBoundingClientRect(),
        { width: panel.offsetWidth, height: panel.offsetHeight },
        placement,
        { width: window.innerWidth, height: window.innerHeight },
      )
      panel.style.top = `${top}px`
      panel.style.left = `${left}px`
      panel.dataset.side = side
      panel.style.setProperty('--arrow', `${arrow}px`)
    }
    place()
    panel.focus({ preventScroll: true })
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => {
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
    }
  }, [state, placement])

  useEffect(() => {
    if (state !== 'open') return
    const inside = (node: EventTarget | null) =>
      node instanceof Node && (panelRef.current?.contains(node) || triggerRef.current?.contains(node))
    const onPointerDown = (e: PointerEvent) => !inside(e.target) && close()
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.preventDefault() // inside a modal <dialog>, Esc closes only the popover
      close()
      triggerRef.current?.focus()
    }
    document.addEventListener('pointerdown', onPointerDown, true)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown, true)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [state, close])

  return (
    <>
      {trigger({
        ref: (el) => {
          triggerRef.current = el
        },
        onClick: toggle,
        'aria-expanded': state === 'open',
        'aria-controls': id,
      })}
      {state === 'closed'
        ? null
        : createPortal(
            <div
              ref={panelRef}
              id={id}
              role="dialog"
              tabIndex={-1}
              data-state={state}
              className={className ? `popover ${className}` : 'popover'}
              onBlur={(e) => !panelRef.current?.contains(e.relatedTarget) && e.relatedTarget !== triggerRef.current && close()}
              {...props}
            >
              {arrow ? <span className="popover-arrow" aria-hidden /> : null}
              {typeof children === 'function' ? children(close) : children}
            </div>,
            // A modal <dialog> sits in the top layer, above anything portaled to <body>.
            triggerRef.current?.closest('dialog') ?? document.body,
          )}
    </>
  )
}
