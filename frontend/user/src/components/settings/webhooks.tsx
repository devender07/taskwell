import { Link, useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { fullTime, relativeTime } from '@taskwell/shared/time'
import { webhookEvents, webhookUrl } from '@taskwell/shared/validate'
import { createWebhook, toFormErrors, updateWebhook, type DeliveryStatus, type FieldErrors, type Webhook, type WebhookEvent } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { CopyButton } from './integrations'
import './webhooks.css'

export const mono = { fontFamily: 'var(--mono)', fontSize: 14, lineHeight: '21px' }

export const EVENTS: { name: WebhookEvent; about: string }[] = [
  { name: 'issue.created', about: 'An issue is created.' },
  { name: 'issue.updated', about: 'An issue changes: status, assignee, title, labels and more.' },
  { name: 'issue.deleted', about: 'An issue is deleted.' },
  { name: 'comment.created', about: 'Someone comments on an issue.' },
]

const STATUS_CLASS: Record<DeliveryStatus, string> = { delivered: 'text-success', failed: 'text-accent', pending: 'text-secondary' }
const STATUS_LABEL: Record<DeliveryStatus, string> = { delivered: 'Delivered', failed: 'Failed', pending: 'Pending' }

export function DeliveryStatusText({ status }: { status: DeliveryStatus }) {
  return <span className={STATUS_CLASS[status]}>{STATUS_LABEL[status]}</span>
}

export function EventChips({ events }: { events: string[] }) {
  return (
    <span className="flex flex-wrap gap-1">
      {events.map((e) => (
        <code key={e} className="webhook-chip">
          {e}
        </code>
      ))}
    </span>
  )
}

export function SecretView({ secret, onDone }: { secret: string; onDone: () => void }) {
  return (
    <div className="grid gap-4">
      <p className="text-body-sm m-0" role="status">
        Copy it now; you won't see it again. Use it to check the X-Taskwell-Signature header.
      </p>
      <div className="flex items-end gap-3">
        <div className="flex-grow" style={{ minWidth: 0 }}>
          <Input label="Signing secret" readOnly value={secret} onFocus={(e) => e.currentTarget.select()} style={mono} />
        </div>
        <CopyButton text={secret} label="Copy secret" />
      </div>
      <Button className="w-full" onClick={onDone}>
        Done
      </Button>
    </div>
  )
}

type State = { errors?: FieldErrors; error?: string | null; secret?: string }

export function WebhookDialog({ workspace, webhook, open, onClose }: { workspace: string; webhook?: Webhook; open: boolean; onClose: () => void }) {
  const router = useRouter()
  const [url, setUrl] = useState(webhook?.url ?? '')
  const [events, setEvents] = useState<string[]>(webhook?.events ?? EVENTS.map((e) => e.name))

  const [state, submit, pending] = useActionState(async (): Promise<State> => {
    const errors = { url: webhookUrl(url), events: webhookEvents(events) }
    if (errors.url || errors.events) return { errors }
    const form = new FormData()
    form.set('url', url.trim())
    for (const e of events) form.append('events', e)
    try {
      if (webhook) {
        await updateWebhook(workspace, webhook.id, form)
        await reloadPage(router)
        onClose()
        return {}
      }
      const { secret } = await createWebhook(workspace, form)
      await reloadPage(router)
      return { secret }
    } catch (e) {
      return toFormErrors(e)
    }
  }, {})

  const toggle = (name: string, on: boolean) => setEvents((prev) => (on ? [...prev, name] : prev.filter((e) => e !== name)))

  return (
    <Dialog open={open} onClose={onClose} title={state.secret ? 'Webhook added' : webhook ? 'Edit webhook' : 'Add webhook'}>
      {state.secret ? (
        <SecretView secret={state.secret} onDone={onClose} />
      ) : (
        <form action={submit} noValidate className="grid gap-4">
          <Input label="URL" placeholder="https://example.com/taskwell" autoComplete="off" autoFocus value={url} onChange={(e) => setUrl(e.target.value)} error={state.errors?.url} />
          <fieldset className="grid gap-3 m-0 p-0" style={{ border: 0 }}>
            <legend className="text-label mb-2">Events</legend>
            {EVENTS.map((e) => (
              <div key={e.name} className="grid gap-1">
                <Checkbox label={<code style={mono}>{e.name}</code>} checked={events.includes(e.name)} onChange={(ev) => toggle(e.name, ev.target.checked)} />
                <span className="text-caption text-secondary" style={{ paddingLeft: 24 }}>
                  {e.about}
                </span>
              </div>
            ))}
            <FormError>{state.errors?.events}</FormError>
          </fieldset>
          <FormError>{state.error}</FormError>
          <Button type="submit" className="w-full" disabled={pending}>
            {pending ? 'Saving…' : webhook ? 'Save' : 'Add webhook'}
          </Button>
        </form>
      )}
    </Dialog>
  )
}

export function AddWebhookButton({ workspace }: { workspace: string }) {
  const [open, setOpen] = useState(false)
  const [key, setKey] = useState(0)
  return (
    <>
      <Button
        variant="secondary"
        style={{ justifySelf: 'start' }}
        onClick={() => {
          setKey((k) => k + 1)
          setOpen(true)
        }}
      >
        Add webhook
      </Button>
      <WebhookDialog key={key} workspace={workspace} open={open} onClose={() => setOpen(false)} />
    </>
  )
}

export function ActiveToggle({ workspace, webhook }: { workspace: string; webhook: Webhook }) {
  const router = useRouter()
  const [error, toggle, pending] = useActionState(async (_prev: string | null, active: boolean) => {
    const form = new FormData()
    form.set('active', String(active))
    try {
      await updateWebhook(workspace, webhook.id, form)
      await reloadPage(router)
    } catch (e) {
      return toFormErrors(e).error
    }
    return null
  }, null)
  return (
    <span className="grid gap-1">
      <Checkbox label="Active" checked={webhook.active} disabled={pending} onChange={(e) => startTransition(() => toggle(e.target.checked))} />
      <FormError>{error}</FormError>
    </span>
  )
}

export function LastDelivery({ webhook }: { webhook: Webhook }) {
  const last = webhook.last_delivery
  if (!last) return <span className="text-caption text-secondary">No deliveries yet</span>
  return (
    <span className="text-caption text-secondary">
      <DeliveryStatusText status={last.status} />
      {last.response_code ? ` (${last.response_code})` : ''}
      {' · '}
      <time dateTime={last.created_at} title={fullTime(last.created_at)}>
        {relativeTime(last.created_at)}
      </time>
    </span>
  )
}

export function WebhookRow({ workspace, webhook }: { workspace: string; webhook: Webhook }) {
  return (
    <li className="flex items-center gap-4 px-6 py-3" style={{ minHeight: 56 }}>
      <div className="grid gap-2 flex-grow" style={{ minWidth: 0 }}>
        <Link to="/$workspace/settings/webhooks/$id" params={{ workspace, id: webhook.id }} className="webhook-link ellipsis" style={mono}>
          {webhook.url}
        </Link>
        <EventChips events={webhook.events} />
        <LastDelivery webhook={webhook} />
      </div>
      <ActiveToggle workspace={workspace} webhook={webhook} />
    </li>
  )
}
