import { extractDataFromArray } from '@taskwell/shared/list'
import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { ColorPicker } from '@taskwell/shared/color-picker'
import { Input } from '@taskwell/shared/input'
import { labelColor, labelName } from '@taskwell/shared/validate'
import { api, toFormErrors, type FieldErrors, type Label, type Workspace, workspacePath } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { LabelDot } from '@/components/issues/fields'

type State = { errors?: FieldErrors; error?: string | null }

const DEFAULT_COLOR = '#5e6ad2'

const labelsPath = (workspace: Workspace) => `${workspacePath(workspace.slug)}/labels`

// Create (no label) or edit a label.
function LabelForm({ workspace, label, onDone }: { workspace: Workspace; label?: Label; onDone?: () => void }) {
  const router = useRouter()
  const [name, setName] = useState(extractDataFromArray(label, 'name', ''))
  const [color, setColor] = useState(extractDataFromArray(label, 'color', DEFAULT_COLOR))

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { name: labelName(name), color: labelColor(color) }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('name', name.trim())
    form.set('color', color.toLowerCase())
    try {
      await api<Label>(label ? `${labelsPath(workspace)}/${encodeURIComponent(label.id)}` : labelsPath(workspace), form)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadPage(router)
    if (!label) setName('')
    onDone?.()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-2">
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

export function AddLabel({ workspace }: { workspace: Workspace }) {
  return (
    <section className="grid gap-4 p-6 bg-surface border rounded-lg">
      <h2 className="text-label">Add a label</h2>
      <LabelForm workspace={workspace} />
    </section>
  )
}

export function LabelList({ workspace, labels, manage }: { workspace: Workspace; labels: Label[]; manage: boolean }) {
  if (labels.length === 0) {
    return (
      <section className="p-6 bg-surface border rounded-lg">
        <p className="text-body-sm text-secondary">No labels yet.</p>
      </section>
    )
  }
  return (
    <ul className="grid m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }} aria-label="Labels">
      {labels.map((l) => (
        <LabelRow key={l.id} workspace={workspace} label={l} manage={manage} />
      ))}
    </ul>
  )
}

function LabelRow({ workspace, label, manage }: { workspace: Workspace; label: Label; manage: boolean }) {
  const router = useRouter()
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const [error, remove, pending] = useActionState(async () => {
    try {
      await api(`${labelsPath(workspace)}/${encodeURIComponent(label.id)}`, undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    await reloadPage(router)
    setDeleting(false)
    return null
  }, null)

  if (editing) {
    return (
      <li className="px-6 py-3">
        <LabelForm workspace={workspace} label={label} onDone={() => setEditing(false)} />
      </li>
    )
  }

  return (
    <li className="flex flex-wrap items-center gap-3 px-6 py-2" style={{ minHeight: 56 }}>
      <LabelDot color={label.color} />
      <span className="text-label text-primary ellipsis flex-grow" style={{ minWidth: 0 }}>{label.name}</span>
      {manage ? (
        <>
          <Button variant="ghost" onClick={() => setEditing(true)}>
            Edit
          </Button>
          <Button variant="ghost" onClick={() => setDeleting(true)}>
            Delete
          </Button>
          <Dialog open={deleting} onClose={() => setDeleting(false)} title={`Delete ${label.name}?`} description="It will be removed from every issue.">
            <form action={remove} className="grid gap-4">
              <FormError>{error}</FormError>
              <Button type="submit" variant="danger" className="w-full" disabled={pending}>
                {pending ? 'Deleting…' : 'Delete label'}
              </Button>
            </form>
          </Dialog>
        </>
      ) : null}
    </li>
  )
}
