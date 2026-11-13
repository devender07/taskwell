import { useRouter } from '@tanstack/react-router'
import { Suspense, lazy, useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Dialog } from '@taskwell/shared/dialog'
import { StatusIcon } from '@taskwell/shared/icons'
import { Input } from '@taskwell/shared/input'
import type { Doc } from '@taskwell/shared/rich-text'
import { SmartAutocomplete, type Option } from '@taskwell/shared/smart-autocomplete'
import { issueDescription, templateName, templateTitle } from '@taskwell/shared/validate'
import { saveDefault, toFormErrors, type DefaultLabel, type DefaultStatus, type DefaultStatusType, type DefaultTemplate } from '@/api'
import { LabelDot } from './labels'
import { ConfirmDelete, FormError, Section, type State } from './parts'

const RichEditor = lazy(() => import('@taskwell/shared/rich-editor').then((m) => ({ default: m.RichEditor })))

const PRIORITIES: Option[] = ['No priority', 'Urgent', 'High', 'Medium', 'Low'].map((label, i) => ({ value: String(i), label }))

type Data = { statuses: DefaultStatus[]; types: DefaultStatusType[]; labels: DefaultLabel[] }

export function Templates({ templates, ...data }: Data & { templates: DefaultTemplate[] }) {
  const [open, setOpen] = useState<DefaultTemplate | 'new' | null>(null)
  const [title, setTitle] = useState('New template')
  const next = open === 'new' ? 'New template' : 'Edit template'
  if (open && title !== next) setTitle(next)
  return (
    <Section
      title="Templates"
      description="New workspaces start with these issue templates."
      action={<Button variant="secondary" onClick={() => setOpen('new')}>New template</Button>}
    >
      {templates.length ? (
        <ul className="m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }} aria-label="Templates">
          {templates.map((t) => (
            <li key={t.id} className="defaults-row">
              <div className="grid flex-grow" style={{ minWidth: 0 }}>
                <span className="text-label text-primary ellipsis">{t.name}</span>
                {t.title ? <span className="text-caption text-secondary ellipsis">{t.title}</span> : null}
              </div>
              <Button variant="ghost" onClick={() => setOpen(t)}>Edit</Button>
              <ConfirmDelete kind="templates" id={t.id} name={t.name} noun="template" description="New workspaces won't get this template." />
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-body-sm text-secondary m-0">No templates yet.</p>
      )}
      <Dialog open={open !== null} onClose={() => setOpen(null)} title={title} wide>
        {open ? <TemplateForm key={open === 'new' ? 'new' : open.id} {...data} template={open === 'new' ? undefined : open} onDone={() => setOpen(null)} /> : null}
      </Dialog>
    </Section>
  )
}

function TemplateForm({ statuses, types, labels, template: t, onDone }: Data & { template?: DefaultTemplate; onDone: () => void }) {
  const router = useRouter()
  const [name, setName] = useState(t?.name ?? '')
  const [title, setTitle] = useState(t?.title ?? '')
  const [description, setDescription] = useState<{ doc: Doc | null; length: number }>({ doc: t?.description ?? null, length: 0 })
  const [statusId, setStatusId] = useState(t?.status_id ?? '')
  const [priority, setPriority] = useState(String(t?.priority ?? 0))
  const [labelIds, setLabelIds] = useState(t?.label_ids ?? [])

  const statusOpts: Option[] = [
    { value: '', label: 'Default status' },
    ...statuses.map((s) => ({ value: s.id, label: s.name, icon: <StatusIcon status={types.find((x) => x.id === s.type_id) ?? s} /> })),
  ]

  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { name: templateName(name), title: templateTitle(title, false), description: issueDescription(description.length) }
    if (Object.values(errors).some(Boolean)) return { errors }
    const form = new FormData()
    form.set('name', name.trim())
    form.set('title', title.trim())
    form.set('description', description.doc ? JSON.stringify(description.doc) : '')
    form.set('priority', priority || '0')
    form.set('status_id', statusId)
    for (const id of labelIds) form.append('label_id', id)
    try {
      await saveDefault<DefaultTemplate>('templates', form, t?.id)
    } catch (e) {
      return toFormErrors(e)
    }
    await router.invalidate()
    onDone()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-6">
      <div className="grid gap-6" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))' }}>
        <div className="grid gap-4" style={{ alignContent: 'start', minWidth: 0 }}>
          <Input label="Name" placeholder="Bug report" autoComplete="off" autoFocus value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
          <Input label="Title" placeholder="Issue title" autoComplete="off" value={title} onChange={(e) => setTitle(e.target.value)} error={errors?.title} />
          <Suspense fallback={<p className="text-caption text-secondary m-0">Loading editor…</p>}>
            <RichEditor
              label="Description"
              placeholder="Add a description…"
              minHeight={200}
              content={t?.description}
              onChange={(doc, length) => setDescription({ doc, length })}
              error={errors?.description}
            />
          </Suspense>
        </div>
        <div className="grid gap-4" style={{ alignContent: 'start', minWidth: 0 }}>
          <SmartAutocomplete label="Status" options={statusOpts} value={statusOpts.find((o) => o.value === statusId) ?? null} onChange={(o) => setStatusId(o?.value ?? '')} error={errors?.status_id} />
          <SmartAutocomplete label="Priority" options={PRIORITIES} value={PRIORITIES.find((o) => o.value === priority) ?? null} onChange={(o) => setPriority(o?.value ?? '0')} error={errors?.priority} />
          <fieldset className="grid gap-2 m-0 p-0" style={{ border: 0 }}>
            <legend className="text-label text-secondary p-0 mb-2">Labels</legend>
            {labels.length ? (
              labels.map((l) => (
                <Checkbox
                  key={l.id}
                  checked={labelIds.includes(l.id)}
                  onChange={(e) => setLabelIds(e.target.checked ? [...labelIds, l.id] : labelIds.filter((id) => id !== l.id))}
                  label={
                    <span className="flex items-center gap-2" style={{ minWidth: 0 }}>
                      <LabelDot color={l.color} />
                      <span className="ellipsis">{l.name}</span>
                    </span>
                  }
                />
              ))
            ) : (
              <p className="text-caption text-secondary m-0">No default labels yet.</p>
            )}
            <FormError>{errors?.label_id}</FormError>
          </fieldset>
        </div>
      </div>
      <FormError>{error}</FormError>
      <div className="flex justify-end gap-3">
        <Button variant="ghost" onClick={onDone}>Cancel</Button>
        <Button type="submit" disabled={pending}>
          {t ? (pending ? 'Saving…' : 'Save template') : pending ? 'Creating…' : 'Create template'}
        </Button>
      </div>
    </form>
  )
}
