import { createFileRoute } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { Checkbox } from '@taskwell/shared/checkbox'
import { SmartAutocomplete } from '@taskwell/shared/smart-autocomplete'
import { api, toFormErrors, type NotificationSettings } from '@/api'
import { loaderApi } from '@/realtime'

export const Route = createFileRoute('/_app/$workspace/_main/settings/notifications')({
  loader: (ctx) => loaderApi<NotificationSettings>(ctx, '/me/notification-settings'),
  component: NotificationSettingsPage,
})

type Kind = 'mentions' | 'assignments' | 'status' | 'comments' | 'chat' | 'due_dates'

const modes: { value: NotificationSettings['email']; label: string }[] = [
  { value: 'instant', label: 'Right away' },
  { value: 'digest', label: 'Once a day (daily digest)' },
  { value: 'off', label: 'Never' },
]
const kinds: { value: Kind; label: string; caption?: string }[] = [
  { value: 'mentions', label: 'Mentions' },
  { value: 'assignments', label: 'Assigned to me or added to a project' },
  { value: 'status', label: 'Status changes on issues I watch' },
  { value: 'comments', label: 'Comments on issues I watch' },
  { value: 'chat', label: 'Direct messages and chat mentions' },
  { value: 'due_dates', label: 'Due dates', caption: "The day before an issue assigned to you is due, and when it's overdue." },
]

const browserZone = Intl.DateTimeFormat().resolvedOptions().timeZone
const zones = [...new Set(['UTC', ...Intl.supportedValuesOf('timeZone')])].map((z) => ({ value: z, label: z.replaceAll('_', ' ') }))

// Never chosen (null): the browser's zone is suggested, and sent with the next save.
const withZone = (s: NotificationSettings) => (s.timezone === null ? { ...s, timezone: browserZone } : s)

function NotificationSettingsPage() {
  const data = Route.useLoaderData()
  const [loaded, setLoaded] = useState(data)
  const [settings, setSettings] = useState(() => withZone(data))
  const [saved, setSaved] = useState(0)
  const [error, setError] = useState<string | null>(null)

  if (data !== loaded) {
    setLoaded(data)
    setSettings(withZone(data))
  }

  useEffect(() => {
    if (!saved) return
    const t = setTimeout(() => setSaved(0), 2000)
    return () => clearTimeout(t)
  }, [saved])

  async function save(next: NotificationSettings) {
    const prev = settings
    setSettings(next)
    setSaved(0)
    setError(null)
    const form = new FormData()
    for (const [k, v] of Object.entries(next)) form.set(k, String(v ?? ''))
    try {
      setSettings(withZone(await api<NotificationSettings>('/me/notification-settings', form)))
      setSaved(Date.now())
    } catch (e) {
      setSettings(prev)
      const { errors, error } = toFormErrors(e)
      setError(errors.email ?? errors.weekly ?? errors.due_dates ?? errors.timezone ?? error)
    }
  }

  return (
    <main className="grid gap-6 w-full px-8 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top grid gap-2">
        <h1 className="text-h2">Notifications</h1>
        <p className="text-body-sm text-secondary">Activity always gets every notification. These settings choose what is emailed to you, in every workspace.</p>
      </header>
      <section className="grid gap-6 p-6 bg-surface border rounded-lg">
        <fieldset className="grid gap-1 m-0 p-0" style={{ border: 0 }}>
          <legend className="text-label text-primary mb-2 p-0">Email me</legend>
          {modes.map((m) => (
            <label key={m.value} className="checkbox" style={{ minHeight: 44 }}>
              <input type="radio" name="email" value={m.value} checked={settings.email === m.value} onChange={() => save({ ...settings, email: m.value })} />
              {m.label}
            </label>
          ))}
        </fieldset>
        <div className="grid gap-2" style={{ maxWidth: 360 }}>
          <SmartAutocomplete
            label="Digest timezone"
            options={zones}
            value={zones.find((z) => z.value === settings.timezone) ?? null}
            onChange={(o) => o && o.value !== settings.timezone && save({ ...settings, timezone: o.value })}
          />
          <p className="text-caption text-secondary m-0">The daily digest is sent by this timezone's clock.</p>
        </div>
        <fieldset className="grid gap-1 m-0 p-0" style={{ border: 0 }} disabled={settings.email === 'off'}>
          <legend className="text-label text-primary mb-2 p-0">Email me about</legend>
          {kinds.map((k) => (
            <div key={k.value}>
              <div className="flex items-center" style={{ minHeight: 44 }}>
                <Checkbox label={k.label} checked={settings[k.value]} onChange={(e) => save({ ...settings, [k.value]: e.target.checked })} />
              </div>
              {k.caption ? <p className="text-caption text-secondary m-0">{k.caption}</p> : null}
            </div>
          ))}
        </fieldset>
        <div className="grid gap-1">
          <div className="flex items-center" style={{ minHeight: 44 }}>
            <Checkbox label="Weekly summary" checked={settings.weekly} disabled={settings.email === 'off'} onChange={(e) => save({ ...settings, weekly: e.target.checked })} />
          </div>
          <p className="text-caption text-secondary m-0">
            {settings.email === 'off' ? 'Turn on email to get it.' : "Monday mornings in your digest timezone: what you closed last week, what's due and what's blocked."}
          </p>
        </div>
        <div role="status" className="text-caption" style={{ minHeight: 20 }}>
          {error ? <span className="text-accent">{error}</span> : saved ? <span className="text-success">Saved.</span> : null}
        </div>
      </section>
    </main>
  )
}
