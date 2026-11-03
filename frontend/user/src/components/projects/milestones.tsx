import { useActionState, useEffect, useState } from 'react'
import { Button } from '@tether/shared/button'
import { DatePicker } from '@tether/shared/date-picker'
import { PlusIcon } from '@tether/shared/icons'
import { Input } from '@tether/shared/input'
import { milestoneName } from '@tether/shared/validate'
import { deleteMilestone, listMilestones, milestonesPath, saveMilestone, toFormErrors, type FieldErrors, type Milestone, type Project, type Workspace } from '@/api'
import { subscribePaths } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { formatDate } from '@/components/issues/fields'

type State = { errors?: FieldErrors; error?: string | null }

// A project's milestones, fetched while shown (in a dialog) and kept live. Read-only without `manage`; `titled` false
// when the dialog's own title already says "Milestones".
export function ProjectMilestones({ workspace, project, manage, titled = true }: { workspace: Workspace; project: Project; manage: boolean; titled?: boolean }) {
  const [milestones, setMilestones] = useState<Milestone[] | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [version, setVersion] = useState(0)
  const [editing, setEditing] = useState<string | null>(null)
  const [confirming, setConfirming] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const reload = () => setVersion((v) => v + 1)

  useEffect(() => {
    let live = true
    listMilestones(workspace.slug, project.key).then(
      (m) => live && setMilestones(m),
      (e) => live && setLoadError(toFormErrors(e).error),
    )
    return () => {
      live = false
    }
  }, [workspace.slug, project.key, version])
  useEffect(() => subscribePaths([milestonesPath(workspace.slug, project.key)], () => setVersion((v) => v + 1)), [workspace.slug, project.key])

  return (
    <section className={titled ? 'milestones-titled grid gap-3' : 'grid gap-3'} style={{ alignContent: 'start' }} aria-label="Milestones">
      {titled || manage ? (
        <div className="flex items-center justify-between gap-3">
          {titled ? <h3 className="text-label">Milestones</h3> : <span />}
          {manage && !adding ? (
            <Button variant="ghost" onClick={() => setAdding(true)}>
              <PlusIcon size={16} /> Add milestone
            </Button>
          ) : null}
        </div>
      ) : null}
      {milestones === null ? (
        loadError ? <FormError>{loadError}</FormError> : <p className="text-body-sm text-secondary m-0">Loading…</p>
      ) : milestones.length === 0 ? (
        adding ? null : (
          <p className="text-body-sm text-secondary m-0">No milestones yet.</p>
        )
      ) : (
        <ul className="grid m-0 p-0" style={{ listStyle: 'none' }}>
          {milestones.map((m) =>
            editing === m.id ? (
              <li key={m.id} className="py-2">
                <MilestoneForm workspace={workspace} project={project} milestone={m} onDone={() => { setEditing(null); reload() }} onCancel={() => setEditing(null)} />
              </li>
            ) : confirming === m.id ? (
              <ConfirmDelete key={m.id} workspace={workspace} project={project} milestone={m} onDone={() => { setConfirming(null); reload() }} onCancel={() => setConfirming(null)} />
            ) : (
              <li key={m.id} className="flex items-center gap-3" style={{ minHeight: 44 }}>
                <span className="milestone-diamond flex-noshrink" aria-hidden />
                <span className="text-body-sm flex-grow ellipsis">{m.name}</span>
                <span className="text-caption text-secondary flex-noshrink">{formatDate(m.date)}</span>
                {manage ? (
                  <>
                    <Button variant="ghost" aria-label={`Edit ${m.name}`} onClick={() => setEditing(m.id)}>Edit</Button>
                    <Button variant="ghost" aria-label={`Delete ${m.name}`} onClick={() => setConfirming(m.id)}>Delete</Button>
                  </>
                ) : null}
              </li>
            ),
          )}
        </ul>
      )}
      {manage && adding ? (
        <MilestoneForm
          workspace={workspace}
          project={project}
          onDone={() => {
            setAdding(false)
            reload()
          }}
          onCancel={() => setAdding(false)}
        />
      ) : null}
    </section>
  )
}

type FormProps = { workspace: Workspace; project: Project; milestone?: Milestone; onDone: () => void; onCancel?: () => void }

// Add (no milestone; clears itself after) or edit one inline.
function MilestoneForm({ workspace, project, milestone, onDone, onCancel }: FormProps) {
  const [name, setName] = useState(milestone?.name ?? '')
  const [date, setDate] = useState(milestone?.date ?? '')

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { name: milestoneName(name), date: date ? null : 'Choose a date.' }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('name', name.trim())
    form.set('date', date)
    try {
      await saveMilestone(workspace.slug, project.key, form, milestone?.id)
    } catch (e) {
      return toFormErrors(e)
    }
    if (!milestone) {
      setName('')
      setDate('')
    }
    onDone()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-3" aria-label={milestone ? `Edit ${milestone.name}` : 'Add milestone'}>
      <div className="grid gap-3" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))' }}>
        <Input label="Name" placeholder="Beta" autoComplete="off" autoFocus value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
        <DatePicker label="Date" value={date} onChange={setDate} error={errors?.date} />
      </div>
      <FormError>{error}</FormError>
      <div className="flex justify-end gap-2">
        {onCancel ? <Button variant="ghost" onClick={onCancel}>Cancel</Button> : null}
        <Button type="submit" disabled={pending}>
          {milestone ? (pending ? 'Saving…' : 'Save') : pending ? 'Adding…' : 'Add milestone'}
        </Button>
      </div>
    </form>
  )
}

function ConfirmDelete({ workspace, project, milestone, onDone, onCancel }: Required<Omit<FormProps, 'milestone'>> & { milestone: Milestone }) {
  const [error, remove, pending] = useActionState(async () => {
    try {
      await deleteMilestone(workspace.slug, project.key, milestone.id)
    } catch (e) {
      return toFormErrors(e).error
    }
    onDone()
    return null
  }, null)

  return (
    <li className="grid gap-2 py-2">
      <form action={remove} className="flex flex-wrap items-center gap-2" style={{ minHeight: 44 }}>
        <span className="text-body-sm flex-grow">Delete {milestone.name}?</span>
        <Button variant="ghost" onClick={onCancel}>Cancel</Button>
        <Button type="submit" variant="danger" disabled={pending} autoFocus>
          {pending ? 'Deleting…' : 'Delete'}
        </Button>
      </form>
      <FormError>{error}</FormError>
    </li>
  )
}
