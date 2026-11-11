import { useState } from 'react'
import { Checkbox } from '@taskwell/shared/checkbox'
import { api, PLANS, toFormErrors, type Setting } from '@/api'

type Plan = keyof typeof PLANS
const ALL = Object.keys(PLANS) as Plan[]

// Saves one setting optimistically, rolling back with the error on failure; a newer value from a refetch wins.
function useSetting(setting: Setting) {
  const [latest, setLatest] = useState(setting)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  if (Date.parse(setting.updated_at) > Date.parse(latest.updated_at)) setLatest(setting)

  async function save(value: string) {
    const prev = latest
    setLatest({ ...prev, value })
    setPending(true)
    setError(null)
    const form = new FormData()
    form.set('value', value)
    try {
      setLatest(await api<Setting>(`/config/${encodeURIComponent(prev.key)}`, form))
    } catch (e) {
      setLatest(prev)
      const { errors, error } = toFormErrors(e)
      setError(errors.value ?? error)
    }
    setPending(false)
  }
  return { value: latest.value, pending, error, save }
}

// feature_<name> is the switch for everyone; feature_<name>_plans the plans that get it while it's on.
export function FeatureRow({ feature, plans }: { feature: Setting; plans: Setting }) {
  const toggle = useSetting(feature)
  const list = useSetting(plans)
  const label = feature.description || feature.key
  const enabled = toggle.value !== 'false'
  const on = ALL.filter((p) => list.value.split(',').includes(p))
  const error = toggle.error ?? list.error

  return (
    <tr>
      <td className="text-primary font-510">
        {label}
        {error ? <p role="alert" className="text-caption text-accent" style={{ whiteSpace: 'normal' }}>{error}</p> : null}
      </td>
      <td>
        <Checkbox label={null} aria-label={`${label}: on`} checked={enabled} disabled={toggle.pending} onChange={(e) => toggle.save(String(e.target.checked))} />
      </td>
      {ALL.map((p) => (
        <td key={p} style={enabled ? undefined : { opacity: 0.4 }}>
          <Checkbox
            label={null}
            aria-label={`${label}: ${PLANS[p]}`}
            checked={on.includes(p)}
            disabled={!enabled || list.pending}
            onChange={(e) => list.save(ALL.filter((x) => (x === p ? e.target.checked : on.includes(x))).join(','))}
          />
        </td>
      ))}
    </tr>
  )
}
