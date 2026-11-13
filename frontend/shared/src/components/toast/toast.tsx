import { createContext, use, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Button } from '../button/button'
import './toast.css'

type Toast = { message: string; action?: { label: string; run: () => void } }
type Shown = Toast & { id: number }

const TOAST_MS = 8000
const ToastContext = createContext<{ show: (toast: Toast) => void }>({ show: () => {} })

// One toast at a time, bottom-left, gone after 8 seconds or once its action runs. Mount the provider above anything
// that navigates, so a toast survives the page it was shown from.
export function ToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<Shown | null>(null)
  const value = useMemo(() => ({ show: (t: Toast) => setToast({ ...t, id: Date.now() }) }), [])

  useEffect(() => {
    if (!toast) return
    const timer = setTimeout(() => setToast(null), TOAST_MS)
    return () => clearTimeout(timer)
  }, [toast])

  return (
    <ToastContext value={value}>
      {children}
      <div role="status" className="toast-region">
        {toast ? (
          <div key={toast.id} className="toast">
            <span className="text-body-sm" style={{ minWidth: 0 }}>{toast.message}</span>
            {toast.action ? (
              <Button
                variant="ghost"
                className="flex-noshrink"
                onClick={() => {
                  setToast(null)
                  toast.action!.run()
                }}
              >
                {toast.action.label}
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>
    </ToastContext>
  )
}

export const useToast = () => use(ToastContext)
