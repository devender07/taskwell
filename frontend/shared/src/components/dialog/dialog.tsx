import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { XIcon } from '../icons/icons'
import './dialog.css'

type DialogProps = {
  open: boolean
  onClose: () => void
  title: string
  description?: ReactNode
  children: ReactNode
  // false keeps it open on Esc and backdrop click and hides the close button (e.g. a required first step).
  dismissible?: boolean
  wide?: boolean // 880px instead of 440px, for two-column forms
}

const EXIT_MS = 120 // matches the fade-out in dialog.css

// Modal on the native <dialog>, portaled to <body> so no ancestor's form, overflow or transform affects it. The browser
// puts it in the top layer, traps focus, and restores focus on close. Fades in and out; closes on Esc, backdrop click,
// or the close button. React events still bubble to the component's parents.
export function Dialog({ open, onClose, title, description, children, dismissible = true, wide }: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  const [closing, setClosing] = useState(false)
  // Content stays mounted from opening until the fade-out ends, so it never collapses for a frame in between.
  const [mounted, setMounted] = useState(open)
  if (open && !mounted) setMounted(true)
  const exit = useRef<ReturnType<typeof setTimeout>>(undefined)

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (open) {
      clearTimeout(exit.current) // reopened while fading out
      setClosing(false)
      if (!dialog.open) dialog.showModal()
    } else if (dialog.open) {
      setClosing(true)
      exit.current = setTimeout(() => {
        dialog.close()
        setClosing(false)
        setMounted(false)
      }, EXIT_MS)
    }
  }, [open])
  useEffect(() => () => clearTimeout(exit.current), [])

  return createPortal(
    <dialog
      ref={ref}
      className={wide ? 'dialog dialog-wide' : 'dialog'}
      data-state={closing ? 'closing' : 'open'}
      aria-labelledby={titleId}
      aria-describedby={description ? `${titleId}-desc` : undefined}
      onCancel={(e) => {
        e.preventDefault() // Esc: run our animated close instead of the browser's instant one
        if (dismissible) onClose()
      }}
      onClick={(e) => dismissible && e.target === e.currentTarget && onClose()} // the backdrop is the dialog box itself
    >
      {mounted ? (
        <div className="dialog-body grid gap-5">
          <header className="grid gap-2" style={{ paddingRight: dismissible ? 32 : 0 }}>
            <h2 id={titleId} className="text-h2">
              {title}
            </h2>
            {description ? <p id={`${titleId}-desc`} className="text-body-sm text-secondary">{description}</p> : null}
          </header>
          {children}
          {dismissible ? (
            <button type="button" className="dialog-close" aria-label="Close" onClick={onClose}>
              <XIcon size={16} strokeWidth={2} />
            </button>
          ) : null}
        </div>
      ) : null}
    </dialog>,
    document.body,
  )
}
