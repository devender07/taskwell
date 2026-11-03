import { useRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useState } from 'react'
import { Button } from '@tether/shared/button'
import { Dialog } from '@tether/shared/dialog'
import { Input } from '@tether/shared/input'
import { fullTime, relativeTime } from '@tether/shared/time'
import { tokenName } from '@tether/shared/validate'
import { createToken, deleteToken, toFormErrors, type ApiToken, type FieldErrors } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { ConfirmButton } from '@/components/tests/tests'
import { CopyButton } from './integrations'

const mono = { fontFamily: 'var(--mono)', fontSize: 14, lineHeight: '21px' }

type State = { errors?: FieldErrors; error?: string | null; secret?: string }

export function NewTokenButton() {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')

  const [state, submit, pending] = useActionState(async (_prev: State, reset: boolean): Promise<State> => {
    if (reset) return {}
    const error = tokenName(name)
    if (error) return { errors: { name: error } }
    try {
      const { secret } = await createToken(name.trim())
      await reloadPage(router)
      return { secret }
    } catch (e) {
      return toFormErrors(e)
    }
  }, {})

  function start() {
    setName('')
    startTransition(() => submit(true))
    setOpen(true)
  }
  const close = () => setOpen(false)

  const { secret } = state
  const setup = secret ? `claude mcp add --transport http tether ${window.location.origin}/mcp --header "Authorization: Bearer ${secret}"` : ''

  return (
    <>
      <Button variant="secondary" style={{ justifySelf: 'start' }} onClick={start}>
        New token
      </Button>
      <Dialog open={open} onClose={close} title="New token">
        {secret ? (
          <div className="grid gap-4">
            <p className="text-body-sm m-0" role="status">
              Copy it now. You won't see it again.
            </p>
            <div className="flex items-end gap-3">
              <div className="flex-grow" style={{ minWidth: 0 }}>
                <Input label="Token" readOnly value={secret} onFocus={(e) => e.currentTarget.select()} style={mono} />
              </div>
              <CopyButton text={secret} label="Copy token" />
            </div>
            <div className="grid gap-2">
              <div className="flex items-center gap-3">
                <span className="text-label flex-grow">Add it to Claude Code</span>
                <CopyButton text={setup} label="Copy setup command" />
              </div>
              <pre className="m-0 p-4 bg-surface border rounded-lg" style={{ ...mono, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
                {setup}
              </pre>
            </div>
            <Button className="w-full" onClick={close}>
              Done
            </Button>
          </div>
        ) : (
          <form action={() => submit(false)} noValidate className="grid gap-4">
            <Input label="Name" placeholder="Claude Code on my laptop" autoComplete="off" autoFocus value={name} onChange={(e) => setName(e.target.value)} error={state.errors?.name} />
            <FormError>{state.error}</FormError>
            <Button type="submit" className="w-full" disabled={pending}>
              {pending ? 'Creating…' : 'Create token'}
            </Button>
          </form>
        )}
      </Dialog>
    </>
  )
}

export function TokenRow({ token }: { token: ApiToken }) {
  const router = useRouter()
  return (
    <li className="flex items-center gap-3 px-6 py-2" style={{ minHeight: 56 }}>
      <div className="grid gap-1 flex-grow" style={{ minWidth: 0 }}>
        <span className="text-body-sm ellipsis">{token.name}</span>
        <span className="text-caption text-secondary">
          Created <time dateTime={token.created_at} title={fullTime(token.created_at)}>{relativeTime(token.created_at)}</time>
          {' · '}
          {token.last_used_at ? (
            <>
              Last used <time dateTime={token.last_used_at} title={fullTime(token.last_used_at)}>{relativeTime(token.last_used_at)}</time>
            </>
          ) : (
            'Never used'
          )}
        </span>
      </div>
      <ConfirmButton
        label="Revoke"
        title={`Revoke ${token.name}?`}
        description="Tools using it stop working."
        confirm="Revoke"
        pending="Revoking…"
        onConfirm={async () => {
          await deleteToken(token.id)
          await reloadPage(router)
        }}
      />
    </li>
  )
}
