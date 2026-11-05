import { useNavigate } from '@tanstack/react-router'
import { Input } from '@taskwell/shared/input'
import { Pagination } from '@taskwell/shared/pagination'
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
  const navigate = useNavigate()
  const page = search.page ?? 1
  const first = total === 0 ? 0 : (page - 1) * PAGE_SIZE + 1
  return (
    <Pagination
      page={page}
      hasNext={page * PAGE_SIZE < total}
      onPage={(p) => navigate({ to, search: { ...(search.q && { q: search.q }), ...(p > 1 && { page: p }) } })}
      summary={`${first}–${Math.min(page * PAGE_SIZE, total)} of ${total}`}
    />
  )
}
