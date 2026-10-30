import { Link, useNavigate } from '@tanstack/react-router'
import '@tether/shared/button'
import { Input } from '@tether/shared/input'
import { PAGE_SIZE, type ListSearch } from '@/api'

type Props = { to: '/users' | '/workspaces'; search: ListSearch; placeholder: string }

export function SearchForm({ to, search, placeholder }: Props) {
  const navigate = useNavigate()
  return (
    <form
      role="search"
      noValidate
      style={{ maxWidth: 360 }}
      onSubmit={(e) => {
        e.preventDefault()
        const q = String(new FormData(e.currentTarget).get('q') ?? '').trim()
        navigate({ to, search: q ? { q } : {} })
      }}
    >
      <Input key={search.q} aria-label="Search" name="q" type="search" placeholder={placeholder} defaultValue={search.q} />
    </form>
  )
}

export function Pager({ to, search, total }: Omit<Props, 'placeholder'> & { total: number }) {
  const page = search.page ?? 1
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const first = total === 0 ? 0 : (page - 1) * PAGE_SIZE + 1
  const go = (p: number) => ({ ...(search.q && { q: search.q }), ...(p > 1 && { page: p }) })
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-caption text-secondary">
        {first}–{Math.min(page * PAGE_SIZE, total)} of {total}
      </span>
      {pages > 1 ? (
        <div className="flex gap-2">
          {page > 1 ? <Link to={to} search={go(page - 1)} className="button button-ghost">Previous</Link> : null}
          {page < pages ? <Link to={to} search={go(page + 1)} className="button button-ghost">Next</Link> : null}
        </div>
      ) : null}
    </div>
  )
}
