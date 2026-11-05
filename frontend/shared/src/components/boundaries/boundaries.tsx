import { useRouter, useRouterState, type ErrorComponentProps } from '@tanstack/react-router'
import { Button } from '../button/button'
import './boundaries.css'

export function Pending() {
  return (
    <div role="status" className="grid place-center min-h-screen">
      <span className="page-spinner" aria-hidden />
      <span className="sr-only">Loading…</span>
    </div>
  )
}

// A bar across the top while a navigation loads its code and data; it only appears if that takes over 150ms.
export function NavigationProgress() {
  const loading = useRouterState({ select: (s) => s.status === 'pending' })
  return loading ? <div className="nav-progress" aria-hidden /> : null
}

export function ErrorFallback({ error, reset }: ErrorComponentProps) {
  const router = useRouter()
  return (
    <div role="alert" className="grid place-center min-h-screen p-4">
      <div className="grid justify-items-center gap-4 text-center">
        <p className="text-body-sm text-secondary">{error instanceof Error ? error.message : String(error)}</p>
        <Button onClick={() => router.invalidate().then(reset)}>Try again</Button>
      </div>
    </div>
  )
}

export function NotFound() {
  return <div className="grid place-center min-h-screen text-body-sm text-secondary">Not found</div>
}
