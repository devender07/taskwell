import { useNavigate } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { fullTime, relativeTime } from '@tether/shared/time'
import { deleteWebhook, redeliver, rollWebhookSecret, testWebhook, toFormErrors, type Webhook, type WebhookDelivery } from '@/api'
import { FormError } from '@/components/auth/auth-card'
import { ConfirmButton } from '@/components/tests/tests'
import { DeliveryStatusText, SecretView, WebhookDialog, mono } from './webhooks'

function useQueue(run: () => Promise<unknown>) {
  return useActionState(async (): Promise<{ done?: boolean; error?: string | null }> => {
    try {
      await run()
      return { done: true }
    } catch (e) {
      return { error: toFormErrors(e).error }
    }
  }, {})
}

export function WebhookActions({ workspace, webhook }: { workspace: string; webhook: Webhook }) {
  const navigate = useNavigate()
  const [editing, setEditing] = useState(false)
  const [editKey, setEditKey] = useState(0)
  const [secret, setSecret] = useState<string | null>(null)
  const [test, sendTest, testing] = useQueue(() => testWebhook(workspace, webhook.id))

  return (
    <div className="grid gap-2">
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="secondary" onClick={() => {
            setEditKey((k) => k + 1)
            setEditing(true)
          }}>
          Edit
        </Button>
        <form action={sendTest}>
          <Button type="submit" variant="secondary" disabled={testing}>
            {testing ? 'Sending…' : 'Send test'}
          </Button>
        </form>
        <ConfirmButton
          label="Roll secret"
          title="Roll the signing secret?"
          description="The old secret stops working at once. Update your receiver with the new one."
          confirm="Roll secret"
          pending="Rolling…"
          onConfirm={async () => setSecret((await rollWebhookSecret(workspace, webhook.id)).secret)}
        />
        <ConfirmButton
          label="Delete"
          title="Delete this webhook?"
          description="Tether stops sending to it, and its delivery log is deleted."
          confirm="Delete webhook"
          pending="Deleting…"
          onConfirm={async () => {
            await deleteWebhook(workspace, webhook.id)
            await navigate({ to: '/$workspace/settings/webhooks', params: { workspace } })
          }}
        />
      </div>
      {test.done ? (
        <p className="text-caption text-secondary m-0" role="status">
          Test ping queued. It shows up below in a moment.
        </p>
      ) : null}
      <FormError>{test.error}</FormError>
      <WebhookDialog key={editKey} workspace={workspace} webhook={webhook} open={editing} onClose={() => setEditing(false)} />
      <Dialog open={secret !== null} onClose={() => setSecret(null)} title="New signing secret">
        {secret ? <SecretView secret={secret} onDone={() => setSecret(null)} /> : null}
      </Dialog>
    </div>
  )
}

export function DeliveryRow({ workspace, webhook, delivery: d }: { workspace: string; webhook: string; delivery: WebhookDelivery }) {
  const [state, resend, pending] = useQueue(() => redeliver(workspace, webhook, d.id))
  return (
    <details className="delivery">
      <summary className="text-body-sm">
        <code className="ellipsis" style={mono}>
          {d.event}
        </code>
        <DeliveryStatusText status={d.status} />
        <span className="delivery-col-extra text-secondary">{d.response_code ?? '—'}</span>
        <span className="delivery-col-extra text-secondary">{d.attempts}</span>
        <time className="delivery-col-extra text-secondary ellipsis" dateTime={d.created_at} title={fullTime(d.created_at)}>
          {relativeTime(d.created_at)}
        </time>
      </summary>
      <div className="grid gap-3 px-6 pb-4 pt-2">
        <p className="text-caption text-secondary m-0">
          {d.delivered_at ? `Delivered ${fullTime(d.delivered_at)}` : d.status === 'pending' ? `Next try ${fullTime(d.next_at)}` : `Gave up after ${d.attempts} attempts`}
          {d.response_code ? ` · HTTP ${d.response_code}` : ''}
        </p>
        {d.response_error ? (
          <pre className="m-0 p-4 bg-surface border rounded-lg text-accent" style={{ ...mono, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
            {d.response_error}
          </pre>
        ) : null}
        <pre className="m-0 p-4 bg-surface border rounded-lg overflow-x-auto" style={mono}>
          {JSON.stringify(d.payload, null, 2)}
        </pre>
        <form action={resend} className="flex items-center gap-3">
          <Button type="submit" variant="secondary" disabled={pending}>
            {pending ? 'Redelivering…' : 'Redeliver'}
          </Button>
          {state.done ? (
            <span className="text-caption text-secondary" role="status">
              Queued as a new delivery.
            </span>
          ) : null}
          <FormError>{state.error}</FormError>
        </form>
      </div>
    </details>
  )
}

export function Deliveries({ workspace, webhook, deliveries }: { workspace: string; webhook: string; deliveries: WebhookDelivery[] }) {
  if (!deliveries.length) return <p className="text-body-sm text-secondary m-0">No deliveries yet. Send a test to see one.</p>
  return (
    <div className="bg-surface border rounded-lg">
      <div className="delivery-head text-caption text-secondary">
        <span>Event</span>
        <span>Status</span>
        <span className="delivery-col-extra">Code</span>
        <span className="delivery-col-extra">Tries</span>
        <span className="delivery-col-extra">Time</span>
      </div>
      {deliveries.map((d) => (
        <DeliveryRow key={d.id} workspace={workspace} webhook={webhook} delivery={d} />
      ))}
    </div>
  )
}
