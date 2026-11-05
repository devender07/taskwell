import { useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Input } from '@taskwell/shared/input'
import { SmartAutocomplete, type Option } from '@taskwell/shared/smart-autocomplete'
import { fullTime, relativeTime } from '@taskwell/shared/time'
import { connectIntegration, disconnectIntegration, toFormErrors, updateIntegration, type FieldErrors, type Integration, type Provider, type Status } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { PROVIDER_NAME, ProviderIcon } from '@/components/issues/pull-requests'
import { ConfirmButton } from '@/components/tests/tests'

const ABOUT: Record<Provider, string> = {
  github: 'Link pull requests that mention an issue ID, and move the issue when they open and merge.',
  gitlab: 'Link merge requests that mention an issue ID, and move the issue when they open and merge.',
}

export function CopyButton({ text, label }: { text: string; label: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')
  return (
    <Button
      variant="secondary"
      aria-label={label}
      onClick={() =>
        navigator.clipboard.writeText(text).then(
          () => setState('copied'),
          () => setState('failed'),
        )
      }
      onBlur={() => setState('idle')}
    >
      {state === 'copied' ? 'Copied' : state === 'failed' ? "Couldn't copy" : 'Copy'}
    </Button>
  )
}

function Steps({ provider }: { provider: Provider }) {
  const steps =
    provider === 'github'
      ? [
          'In the repository or organization, open Settings → Webhooks → Add webhook.',
          'Paste the webhook URL into Payload URL.',
          'Set Content type to application/json.',
          'Paste the secret into Secret.',
          'Choose "Let me select individual events", tick Pull requests only, and save.',
        ]
      : [
          'In the project or group, open Settings → Webhooks → Add new webhook.',
          'Paste the webhook URL into URL.',
          'Paste the secret into Secret token.',
          'Under Trigger, tick Merge request events only, and save.',
        ]
  return (
    <ol className="grid gap-1 m-0 text-body-sm text-secondary" style={{ paddingLeft: 20 }}>
      {steps.map((s) => (
        <li key={s}>{s}</li>
      ))}
    </ol>
  )
}

type Props = { workspace: string; integration: Integration; statuses: Status[] }

export function IntegrationCard({ workspace, integration, statuses }: Props) {
  const router = useRouter()
  const { provider } = integration
  const name = PROVIDER_NAME[provider]
  const [secret, setSecret] = useState<string | null>(null)

  async function connect() {
    const { secret } = await connectIntegration(workspace, provider)
    setSecret(secret)
    await reloadPage(router)
  }

  const [connectError, runConnect, connecting] = useActionState(async () => {
    try {
      await connect()
    } catch (e) {
      return toFormErrors(e).error
    }
    return null
  }, null)

  return (
    <section className="grid gap-4 p-6 bg-surface border rounded-lg" aria-labelledby={`${provider}-heading`}>
      <div className="flex items-center gap-3">
        <ProviderIcon provider={provider} size={20} />
        <h2 id={`${provider}-heading`} className="text-label flex-grow">{name}</h2>
        {integration.connected ? <span className="text-caption text-success">Connected</span> : null}
      </div>
      {integration.connected && integration.webhook_url ? (
        <>
          {secret ? (
            <div className="grid gap-2 p-4 border rounded-lg" role="status" style={{ borderColor: 'var(--accent)' }}>
              <p className="text-body-sm m-0">Copy this secret now. You won't see it again.</p>
              <div className="flex items-center gap-3">
                <code className="flex-grow ellipsis" style={{ fontFamily: 'var(--mono)', fontSize: 14, lineHeight: '21px', minWidth: 0 }}>
                  {secret}
                </code>
                <CopyButton text={secret} label="Copy secret" />
              </div>
            </div>
          ) : null}
          <div className="flex items-end gap-3">
            <div className="flex-grow" style={{ minWidth: 0 }}>
              <Input label="Webhook URL" readOnly value={integration.webhook_url} onFocus={(e) => e.currentTarget.select()} />
            </div>
            <CopyButton text={integration.webhook_url} label="Copy webhook URL" />
          </div>
          <Steps provider={provider} />
          <p className="text-caption text-secondary m-0">
            {integration.last_delivery_at ? (
              <>
                Last delivery <time dateTime={integration.last_delivery_at} title={fullTime(integration.last_delivery_at)}>{relativeTime(integration.last_delivery_at)}</time>
              </>
            ) : (
              'No deliveries yet'
            )}
          </p>
          <StatusSettings workspace={workspace} integration={integration} statuses={statuses} />
          <div className="flex flex-wrap items-center gap-3">
            <ConfirmButton
              label="Regenerate secret"
              title="Regenerate the secret?"
              description="The old webhook URL and secret stop working."
              confirm="Regenerate secret"
              pending="Regenerating…"
              onConfirm={connect}
            />
            <ConfirmButton
              label="Disconnect"
              title={`Disconnect ${name}?`}
              description="New pull requests stop linking. Ones already linked stay on their issues."
              confirm="Disconnect"
              pending="Disconnecting…"
              onConfirm={async () => {
                await disconnectIntegration(workspace, provider)
                setSecret(null)
                await reloadPage(router)
              }}
            />
          </div>
        </>
      ) : (
        <form action={runConnect} className="grid gap-3">
          <p className="text-body-sm text-secondary m-0">{ABOUT[provider]}</p>
          <FormError>{connectError}</FormError>
          <div>
            <Button type="submit" disabled={connecting}>
              {connecting ? 'Connecting…' : `Connect ${name}`}
            </Button>
          </div>
        </form>
      )}
    </section>
  )
}

type Settings = { open_status: string; merge_status: string }
type State = { errors?: FieldErrors; error?: string | null }

function StatusSettings({ workspace, integration, statuses }: Props) {
  const router = useRouter()
  const done = statuses.filter((s) => s.category === 'completed')
  const openOptions: Option[] = [{ value: '', label: "Don't move" }, ...statuses.map((s) => ({ value: s.id, label: s.name }))]
  const mergeOptions: Option[] = done.map((s) => ({ value: s.id, label: s.name }))
  const openValue = integration.open_status?.id ?? ''
  const mergeValue = integration.merge_status?.id ?? done[0]?.id ?? ''

  const [{ errors, error }, save, pending] = useActionState(async (_prev: State, next: Settings): Promise<State> => {
    const form = new FormData()
    form.set('open_status', next.open_status)
    form.set('merge_status', next.merge_status)
    try {
      await updateIntegration(workspace, integration.provider, form)
    } catch (e) {
      return toFormErrors(e)
    }
    await reloadPage(router)
    return {}
  }, {})

  const change = (patch: Partial<Settings>) =>
    startTransition(() => save({ open_status: openValue, merge_status: integration.merge_status?.id ?? '', ...patch }))

  return (
    <div className="grid gap-3">
      <div className="grid gap-3" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))' }}>
        <SmartAutocomplete
          label="When a pull request opens"
          options={openOptions}
          value={openOptions.find((o) => o.value === openValue) ?? null}
          onChange={(o) => o && o.value !== openValue && change({ open_status: o.value })}
          error={errors?.open_status}
          disabled={pending}
        />
        <SmartAutocomplete
          label="When it merges"
          options={mergeOptions}
          value={mergeOptions.find((o) => o.value === mergeValue) ?? null}
          onChange={(o) => o && o.value !== mergeValue && change({ merge_status: o.value })}
          error={errors?.merge_status}
          disabled={pending}
        />
      </div>
      <FormError>{error}</FormError>
    </div>
  )
}
