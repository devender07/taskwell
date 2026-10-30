import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import './dialog.css'

type DialogProps = {
  open: boolean
  onClose: () => void
  title: string
  description?: ReactNode
  children: ReactNode
  // false keeps it open on Esc and backdrop click and hides the close button (e.g. a required first step).
  dismissible?: boolean
}

const EXIT_MS = 120 // matches the fade-out in dialog.css

// Modal on the native <dialog>: the browser puts it in the top layer, traps focus, and restores focus on close.
// Fades in and out; closes on Esc, backdrop click, or the close button.
export function Dialog({ open, onClose, title, description, children, dismissible = true }: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  const [closing, setClosing] = useState(false)

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (open && !dialog.open) {
      setClosing(false)
      dialog.showModal()
    } else if (!open && dialog.open) {
      setClosing(true)
      const t = setTimeout(() => {
        dialog.close()
        setClosing(false)
      }, EXIT_MS)
      return () => clearTimeout(t)
    }
  }, [open])

  return (
    <dialog
      ref={ref}
      className="dialog"
      data-state={closing ? 'closing' : 'open'}
      aria-labelledby={titleId}
      onCancel={(e) => {
        e.preventDefault() // Esc: run our animated close instead of the browser's instant one
        if (dismissible) onClose()
      }}
      onClick={(e) => dismissible && e.target === e.currentTarget && onClose()} // the backdrop is the dialog box itself
    >
      {open || closing ? (
        <div className="dialog-body grid gap-5">
          <header className="grid gap-2" style={{ paddingRight: dismissible ? 32 : 0 }}>
            <h2 id={titleId} className="text-h2">
              {title}
            </h2>
            {description ? <p className="text-body-sm text-secondary">{description}</p> : null}
          </header>
          {children}
          {dismissible ? (
            <button type="button" className="dialog-close" aria-label="Close" onClick={onClose}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden>
                <path d="M18 6 6 18M6 6l12 12" />
              </svg>
            </button>
          ) : null}
        </div>
      ) : null}
    </dialog>
  )
}
