import { useRouter, type ErrorComponentProps } from '@tanstack/react-router'
import { Button } from '../button/button'

export function Pending() {
  return <div role="status" className="grid place-center min-h-screen text-caption text-secondary">Loading…</div>
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
