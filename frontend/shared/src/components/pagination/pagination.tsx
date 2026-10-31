import type { ReactNode } from 'react'
import { Button } from '../button/button'

type Props = {
  page: number
  hasNext: boolean
  onPage: (page: number) => void
  summary?: ReactNode
  busy?: boolean
}

export function Pagination({ page, hasNext, onPage, summary, busy }: Props) {
  if (page <= 1 && !hasNext && !summary) return null
  return (
    <nav aria-label="Pagination" className="flex items-center justify-between gap-3">
      <span className="text-caption text-secondary" aria-live="polite">
        {summary ?? `Page ${page}`}
      </span>
      {page > 1 || hasNext ? (
        <div className="flex gap-2">
          <Button variant="ghost" disabled={page <= 1 || busy} onClick={() => onPage(page - 1)}>
            Previous
          </Button>
          <Button variant="ghost" disabled={!hasNext || busy} onClick={() => onPage(page + 1)}>
            {busy ? 'Loading…' : 'Next'}
          </Button>
        </div>
      ) : null}
    </nav>
  )
}
