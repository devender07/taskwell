import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { ColorPicker } from '@taskwell/shared/color-picker'
import { Input } from '@taskwell/shared/input'
import { labelColor, labelName } from '@taskwell/shared/validate'
import { saveDefault, toFormErrors, type DefaultLabel } from '@/api'
import { ConfirmDelete, FormError, Section, type State } from './parts'

const DEFAULT_COLOR = '#5e6ad2'

export const LabelDot = ({ color }: { color: string }) => (
  <span aria-hidden className="flex-noshrink" style={{ width: 8, height: 8, borderRadius: 9999, background: color }} />
)

export function Labels({ labels }: { labels: DefaultLabel[] }) {
  return (
    <Section title="Labels" description="New workspaces start with these labels.">
      <div className="p-6 bg-surface border rounded-lg">
        <LabelForm />
      </div>
      {labels.length ? (
        <ul className="m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }} aria-label="Labels">
          {labels.map((l) => (
            <LabelRow key={`${l.id}:${l.name}:${l.color}`} label={l} />
          ))}
        </ul>
      ) : (
        <p className="text-body-sm text-secondary m-0">No labels yet.</p>
      )}
    </Section>
  )
}

function LabelRow({ label }: { label: DefaultLabel }) {
  const [editing, setEditing] = useState(false)
  if (editing) {
    return (
      <li className="defaults-row py-3">
        <LabelForm label={label} onDone={() => setEditing(false)} />
      </li>
    )
  }
  return (
    <li className="defaults-row">
      <LabelDot color={label.color} />
      <span className="text-label text-primary ellipsis flex-grow" style={{ minWidth: 0 }}>{label.name}</span>
      <Button variant="ghost" onClick={() => setEditing(true)}>Edit</Button>
      <ConfirmDelete kind="labels" id={label.id} name={label.name} noun="label" description="New workspaces won't get it, and templates drop it." />
    </li>
  )
}

function LabelForm({ label, onDone }: { label?: DefaultLabel; onDone?: () => void }) {
  const router = useRouter()
  const [name, setName] = useState(label?.name ?? '')
  const [color, setColor] = useState(label?.color ?? DEFAULT_COLOR)

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { name: labelName(name), color: labelColor(color) }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('name', name.trim())
    form.set('color', color.toLowerCase())
    try {
      await saveDefault<DefaultLabel>('labels', form, label?.id)
    } catch (e) {
      return toFormErrors(e)
    }
    await router.invalidate()
    if (!label) setName('')
    onDone?.()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-2 flex-grow">
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex-noshrink" style={{ width: 152 }}>
          <ColorPicker aria-label="Color" value={color} onChange={setColor} error={errors?.color} />
        </div>
        <div className="flex-grow" style={{ minWidth: 160 }}>
          <Input aria-label="Label name" placeholder="Bug" autoComplete="off" autoFocus={Boolean(label)} value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
        </div>
        <Button type="submit" disabled={pending}>
          {label ? (pending ? 'Saving…' : 'Save') : pending ? 'Adding…' : 'Add label'}
        </Button>
        {label ? (
          <Button variant="ghost" onClick={onDone} disabled={pending}>
            Cancel
          </Button>
        ) : null}
      </div>
      <FormError>{error}</FormError>
    </form>
  )
}
