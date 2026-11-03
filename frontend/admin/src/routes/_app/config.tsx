import { createFileRoute } from '@tanstack/react-router'
import type { Setting } from '@/api'
import { loaderApi } from '@/realtime'
import { SettingRow } from '@/components/config/setting-row'

export const Route = createFileRoute('/_app/config')({
  loader: (ctx) => loaderApi<Setting[]>(ctx, '/config'),
  component: Config,
})

const GROUPS = ['App', 'Auth and tokens', 'SMTP', 'Google', 'Uploads', 'Notifications'] as const

function group(key: string): (typeof GROUPS)[number] {
  if (key.startsWith('smtp_')) return 'SMTP'
  if (key.startsWith('google_')) return 'Google'
  if (key.startsWith('app_')) return 'App'
  if (key.startsWith('upload_') || key.startsWith('s3_')) return 'Uploads'
  if (key.startsWith('digest_') || key.startsWith('notification')) return 'Notifications'
  return 'Auth and tokens'
}

function Config() {
  const settings = Route.useLoaderData()
  return (
    <main className="grid gap-8 w-full px-6 py-10" style={{ maxWidth: 1040 }}>
      <header className="grid gap-2">
        <h1 className="text-h2">Config</h1>
        <p className="text-body-sm text-secondary">System settings, shared by every service. Changes apply on the next read.</p>
      </header>
      {GROUPS.map((name) => {
        const rows = settings.filter((s) => group(s.key) === name)
        return rows.length ? (
          <section key={name} className="grid gap-3">
            <h2 className="text-label">{name}</h2>
            <div className="bg-surface border rounded-lg">
              {rows.map((s) => (
                <SettingRow key={s.key} setting={s} />
              ))}
            </div>
          </section>
        ) : null
      })}
    </main>
  )
}
