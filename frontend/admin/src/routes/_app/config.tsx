import { createFileRoute } from '@tanstack/react-router'
import { Input } from '@taskwell/shared/input'
import type { Setting } from '@/api'
import { loaderApi } from '@/realtime'
import { SettingRow } from '@/components/config/setting-row'

export const Route = createFileRoute('/_app/config')({
  validateSearch: (search): { q?: string } => (typeof search.q === 'string' && search.q ? { q: search.q } : {}),
  loader: (ctx) => loaderApi<Setting[]>(ctx, '/config'),
  component: Config,
})

// First matching prefix wins; anything unmatched lands in "Auth and tokens".
const GROUPS: [string, string[]][] = [
  ['App', ['app_']],
  ['Payments', ['paddle_']],
  ['Email (SMTP)', ['smtp_']],
  ['Google sign-in', ['google_']],
  ['Storage and files', ['s3_', 'upload_', 'files_']],
  ['Invites', ['invite_']],
  ['AI testing', ['ai_']],
  ['Notifications', ['digest_', 'notification']],
  ['Auth and tokens', ['']],
]

function group(key: string) {
  return GROUPS.find(([, prefixes]) => prefixes.some((p) => key.startsWith(p)))![0]
}

function Config() {
  const settings = Route.useLoaderData()
  const { q = '' } = Route.useSearch()
  const navigate = Route.useNavigate()
  const needle = q.trim().toLowerCase().replace(/\s+/g, '_')
  const shown = settings.filter((s) => !s.key.startsWith('feature_') && (!needle || s.key.includes(needle) || group(s.key).toLowerCase().includes(q.trim().toLowerCase())))

  return (
    <main className="grid gap-8 w-full px-6 py-10">
      <header className="grid gap-2">
        <h1 className="text-h2">Config</h1>
        <p className="text-body-sm text-secondary">System settings, shared by every service. Changes apply on the next read.</p>
        <Input
          type="search"
          aria-label="Search settings"
          placeholder="Search settings, e.g. paddle or payments"
          defaultValue={q}
          onDebouncedChange={(v) => navigate({ search: v.trim() ? { q: v } : {}, replace: true })}
          style={{ maxWidth: 400 }}
        />
      </header>
      {GROUPS.map(([name]) => {
        const rows = shown.filter((s) => group(s.key) === name).sort((a, b) => a.key.localeCompare(b.key))
        return rows.length ? (
          <section key={name} className="grid gap-3">
            <h2 className="text-label">
              {name} <span className="text-secondary">· {rows.length}</span>
            </h2>
            <div className="bg-surface border rounded-lg">
              {rows.map((s) => (
                <SettingRow key={s.key} setting={s} />
              ))}
            </div>
          </section>
        ) : null
      })}
      {shown.length ? null : <p className="text-body-sm text-secondary">No settings match “{q}”.</p>}
    </main>
  )
}
