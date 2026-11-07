import { useRouter } from '@tanstack/react-router'
import { useActionState, useMemo, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { DatePicker } from '@taskwell/shared/date-picker'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import type { Doc } from '@taskwell/shared/rich-text'
import { SmartAutocomplete, type Option } from '@taskwell/shared/smart-autocomplete'
import { Tabs } from '@taskwell/shared/tabs'
import { issueDescription, repeatEvery, startsOn, templateName, templateTitle } from '@taskwell/shared/validate'
import { api, templatePath, templatesPath, toFormErrors, uploadFile, type FieldErrors, type Label, type Project, type RepeatUnit, type Status, type Template, type Workspace } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { LabelSelect, assigneeOption, formatDate, loadAssignees, priorityOptions, statusOptions } from '@/components/issues/fields'
import { RichEditor } from '@/components/issues/rich-editor'
import { peopleSearch } from '@/components/workspace/member-pages'
import { repeatSummary } from './template'
import '@/components/projects/projects.css'
import { extractDataFromArray } from '@taskwell/shared/list'

type Data = { workspace: Workspace; projects: Project[]; statuses: Status[]; labels: Label[] }
type State = { errors?: FieldErrors; error?: string | null }

const UNITS: { value: RepeatUnit; label: string }[] = [
  { value: 'day', label: 'Days' },
  { value: 'week', label: 'Weeks' },
  { value: 'month', label: 'Months' },
]

export function Templates({ templates, manage, ...data }: Data & { templates: Template[]; manage: boolean }) {
  const [open, setOpen] = useState<Template | 'new' | null>(null)
  return (
    <>
      {manage ? (
        <Button style={{ justifySelf: 'start' }} onClick={() => setOpen('new')}>
          New template
        </Button>
      ) : null}
      {templates.length === 0 ? (
        <p className="text-body-sm text-secondary m-0">No templates yet.</p>
      ) : (
        <section className="bg-surface border rounded-lg overflow-hidden" aria-label="Templates">
          <ul className="grid m-0 p-0" style={{ listStyle: 'none' }}>
            {templates.map((t) => {
              const repeat = repeatSummary(t, formatDate)
              const body = (
                <div className="grid flex-grow" style={{ minWidth: 0 }}>
                  <span className="text-label text-primary ellipsis">{t.name}</span>
                  <span className="text-caption text-secondary ellipsis">
                    {[extractDataFromArray(t.project, 'name', 'Any project'), repeat].filter(Boolean).join(' · ')}
                  </span>
                </div>
              )
              return (
                <li key={t.id}>
                  {t.can_edit ? (
                    <button type="button" className="project-row w-full text-left" style={{ background: 'none', border: 0, font: 'inherit', cursor: 'pointer' }} onClick={() => setOpen(t)}>
                      {body}
                    </button>
                  ) : (
                    <div className="project-row">{body}</div>
                  )}
                </li>
              )
            })}
          </ul>
        </section>
      )}
      {manage ? (
        <Dialog open={open !== null} onClose={() => setOpen(null)} title={open === 'new' ? 'New template' : 'Edit template'} wide>
          {open ? <TemplateForm key={open === 'new' ? 'new' : open.id} {...data} template={open === 'new' ? undefined : open} onDone={() => setOpen(null)} /> : null}
        </Dialog>
      ) : null}
    </>
  )
}

function TemplateForm({ workspace, projects, statuses, labels, template: t, onDone }: Data & { template?: Template; onDone: () => void }) {
  const router = useRouter()
  const today = new Date().toLocaleDateString('en-CA')
  const [name, setName] = useState(extractDataFromArray(t, 'name', ''))
  const [title, setTitle] = useState(extractDataFromArray(t, 'title', ''))
  const [description, setDescription] = useState<{ doc: Doc | null; length: number }>({ doc: extractDataFromArray(t, 'description', null), length: 0 })
  const [project, setProject] = useState(extractDataFromArray(t, 'project.key', ''))
  const [statusId, setStatusId] = useState(extractDataFromArray(t, 'status.id', ''))
  const [priority, setPriority] = useState(String(extractDataFromArray(t, 'priority', 0)))
  const [assignee, setAssignee] = useState<Option | null>(assigneeOption(extractDataFromArray(t, 'assignee', null)))
  const [labelIds, setLabelIds] = useState(extractDataFromArray(t, 'labels', []).map((l) => l.id))
  const [repeats, setRepeats] = useState(Boolean(t?.repeat))
  const [every, setEvery] = useState(String(extractDataFromArray(t, 'repeat.every', 1)))
  const [unit, setUnit] = useState<RepeatUnit>(extractDataFromArray(t, 'repeat.unit', 'week'))
  const [starts, setStarts] = useState(extractDataFromArray(t, 'repeat.next_on', today))
  const [paused, setPaused] = useState(extractDataFromArray(t, 'repeat.paused', false))

  const projectOpts = useMemo(() => [{ value: '', label: 'No project' }, ...projects.map((p) => ({ value: p.key, label: p.name, hint: p.key }))], [projects])
  const statusOpts = useMemo(() => [{ value: '', label: 'Default status' }, ...statusOptions(statuses)], [statuses])
  const people = useMemo(() => peopleSearch(workspace.slug, project || undefined), [workspace.slug, project])
  // The repeat is sent only when it changes, so saving a paused template whose next date has passed still works.
  const repeatChanged = repeats !== Boolean(t?.repeat) || (repeats && (+every !== t?.repeat?.every || unit !== t?.repeat?.unit || starts !== t?.repeat?.next_on))

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = {
      name: templateName(name),
      title: templateTitle(title, repeats),
      description: issueDescription(description.length),
      project: repeats && !project ? 'A repeating template needs a project.' : null,
      repeat_every: repeats ? repeatEvery(every) : null,
      starts_on: repeats && repeatChanged ? startsOn(starts) : null,
    }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('name', name.trim())
    form.set('title', title.trim())
    form.set('description', description.doc ? JSON.stringify(description.doc) : '')
    form.set('project', project)
    form.set('status_id', statusId)
    form.set('assignee_id', extractDataFromArray(assignee, 'value', ''))
    form.set('priority', priority || '0')
    if (labelIds.length === 0) form.set('label_ids', '')
    for (const id of labelIds) form.append('label_ids', id)
    if (repeatChanged) {
      form.set('repeat_every', repeats ? every.trim() : '')
      form.set('repeat_unit', repeats ? unit : '')
      form.set('starts_on', repeats ? starts : '')
    }
    if (repeats) form.set('paused', paused ? '1' : '0')
    try {
      await api<Template>(t ? templatePath(workspace.slug, t.id) : templatesPath(workspace.slug), form)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadPage(router)
    onDone()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="project-edit">
      <div className="grid gap-4" style={{ alignContent: 'start', minWidth: 0 }}>
        <Input label="Name" placeholder="Bug report" autoComplete="off" autoFocus value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
        <Input label="Title" placeholder="Issue title" autoComplete="off" value={title} onChange={(e) => setTitle(e.target.value)} error={errors?.title} />
        <RichEditor
          label="Description"
          placeholder="Add a description…"
          mentions={people}
          minHeight={200}
          content={t?.description}
          onChange={(doc, length) => setDescription({ doc, length })}
          error={errors?.description}
          upload={(file, progress) => uploadFile(workspace.slug, file, undefined, undefined, progress)}
        />
      </div>
      <div className="grid gap-4" style={{ alignContent: 'start', minWidth: 0 }}>
        <SmartAutocomplete label="Project" options={projectOpts} value={projectOpts.find((o) => o.value === project) ?? null} onChange={(o) => setProject(extractDataFromArray(o, 'value', ''))} error={errors?.project} />
        <SmartAutocomplete label="Status" options={statusOpts} value={statusOpts.find((o) => o.value === statusId) ?? null} onChange={(o) => setStatusId(extractDataFromArray(o, 'value', ''))} error={errors?.status_id} />
        <SmartAutocomplete
          key={project}
          label="Assignee"
          placeholder="No assignee"
          loadOptions={loadAssignees(people)}
          value={assignee}
          onChange={(o) => setAssignee(o?.value ? o : null)}
          error={errors?.assignee_id}
        />
        <SmartAutocomplete label="Priority" options={priorityOptions} value={priorityOptions.find((o) => o.value === priority) ?? null} onChange={(o) => setPriority(extractDataFromArray(o, 'value', '0'))} error={errors?.priority} />
        <LabelSelect labels={labels} value={labelIds} onChange={setLabelIds} error={errors?.label_ids} />
        <Checkbox label="Repeat" checked={repeats} onChange={(e) => setRepeats(e.target.checked)} />
        {repeats ? (
          <div className="grid gap-4" style={{ paddingLeft: 32 }}>
            <div className="flex flex-wrap items-start gap-3">
              <div style={{ width: 96 }}>
                <Input label="Every" inputMode="numeric" autoComplete="off" value={every} onChange={(e) => setEvery(e.target.value)} error={errors?.repeat_every} />
              </div>
              <div className="grid gap-2" style={{ minWidth: 0 }}>
                <span className="text-label text-secondary">Unit</span>
                <Tabs aria-label="Repeat unit" items={UNITS} value={unit} onChange={setUnit} />
                <FormError>{errors?.repeat_unit}</FormError>
              </div>
            </div>
            <DatePicker label="Starting" value={starts} onChange={setStarts} error={errors?.starts_on} />
            <Checkbox label="Paused" checked={paused} onChange={(e) => setPaused(e.target.checked)} />
          </div>
        ) : null}
      </div>
      <div className="project-edit-footer" style={{ flexWrap: 'wrap' }}>
        <FormError>{error}</FormError>
        {t ? <DeleteTemplate workspace={workspace} template={t} onDone={onDone} /> : null}
        <Button type="submit" disabled={pending}>
          {t ? (pending ? 'Saving…' : 'Save template') : pending ? 'Creating…' : 'Create template'}
        </Button>
      </div>
    </form>
  )
}

// Asks inline (the form is already in a dialog), then deletes.
function DeleteTemplate({ workspace, template, onDone }: { workspace: Workspace; template: Template; onDone: () => void }) {
  const router = useRouter()
  const [asking, setAsking] = useState(false)
  const [error, remove, pending] = useActionState(async () => {
    try {
      await api(templatePath(workspace.slug, template.id), undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    await reloadPage(router)
    onDone()
    return null
  }, null)

  if (!asking)
    return (
      <Button variant="ghost" style={{ color: 'var(--accent)', marginRight: 'auto' }} onClick={() => setAsking(true)}>
        Delete
      </Button>
    )
  return (
    <span className="flex flex-wrap items-center gap-3" style={{ marginRight: 'auto' }}>
      <FormError>{error}</FormError>
      <Button type="submit" variant="danger" disabled={pending} formAction={remove}>
        {pending ? 'Deleting…' : 'Delete template'}
      </Button>
      <Button variant="ghost" onClick={() => setAsking(false)} disabled={pending}>
        Cancel
      </Button>
    </span>
  )
}
