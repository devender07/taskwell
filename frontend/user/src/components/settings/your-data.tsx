import { Link } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { sixDigitCode } from '@taskwell/shared/validate'
import { ApiError, api, deleteAccount, toFormErrors, type FieldErrors, type User } from '@/api'
import { FormError } from '@/components/auth/auth-card'

type Blocked = { slug: string; name: string }
type State = { errors?: FieldErrors; error?: string | null; workspaces?: Blocked[] }

export function YourData({ user }: { user: User }) {
  const [download, setDownload] = useState<{ pending?: boolean; error?: string | null }>({})
  const [open, setOpen] = useState(false)

  async function save() {
    setDownload({ pending: true })
    try {
      const data = await api<unknown>('/me/export')
      const a = document.createElement('a')
      a.href = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }))
      a.download = 'taskwell-my-data.json'
      a.click()
      URL.revokeObjectURL(a.href)
      setDownload({})
    } catch (e) {
      setDownload({ error: toFormErrors(e).error })
    }
  }

  return (
    <div className="grid gap-4">
      <div className="grid gap-1">
        <h2 className="text-label">Your data</h2>
        <p className="text-body-sm text-secondary">
          Download a copy of your account data, or delete your account. Workspace content is exported by the workspace's admins. See the{' '}
          <Link to="/privacy" className="auth-link">privacy notice</Link>.
        </p>
      </div>
      <FormError>{download.error}</FormError>
      <div className="flex flex-wrap gap-3">
        <Button variant="secondary" onClick={save} disabled={download.pending}>
          {download.pending ? 'Preparing…' : 'Download my data'}
        </Button>
        <Button variant="danger" onClick={() => setOpen(true)}>
          Delete account
        </Button>
      </div>
      <Dialog open={open} onClose={() => setOpen(false)} title="Delete account" description="This can't be undone.">
        {open ? <DeleteAccount user={user} onCancel={() => setOpen(false)} /> : null}
      </Dialog>
    </div>
  )
}

function DeleteAccount({ user, onCancel }: { user: User; onCancel: () => void }) {
  const [code, setCode] = useState<{ sent?: boolean; sending?: boolean; error?: string | null }>({})
  const [{ errors, error, workspaces }, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const bad = sixDigitCode(String(form.get('code') ?? ''))
    if (bad) return { errors: { code: bad } }
    try {
      await deleteAccount(form)
    } catch (e) {
      return { ...toFormErrors(e), workspaces: e instanceof ApiError ? (e.body?.workspaces as Blocked[] | undefined) : undefined }
    }
    location.assign('/signin')
    return {}
  }, {})

  async function sendCode() {
    setCode({ sending: true })
    try {
      await api('/me/delete/code', new FormData())
      setCode({ sent: true })
    } catch (e) {
      setCode({ error: toFormErrors(e).error })
    }
  }

  return (
    <form action={submit} noValidate className="grid gap-4">
      <div className="grid gap-2 text-body-sm">
        <p className="m-0">Deleting your account removes:</p>
        <ul className="grid gap-1 m-0" style={{ paddingLeft: 20 }}>
          <li>your account, sign-in methods and API tokens</li>
          <li>workspaces where you're the only member, with everything in them</li>
          <li>your notes</li>
        </ul>
        <p className="m-0 text-secondary">
          Issues and comments you wrote in shared workspaces stay there, shown as "Deleted user".
        </p>
      </div>
      <div className="flex items-center gap-3">
        <Button variant="secondary" onClick={sendCode} disabled={code.sending}>
          {code.sending ? 'Sending…' : code.sent ? 'Send a new code' : 'Send code'}
        </Button>
        {code.sent ? <p role="status" className="text-caption text-secondary">Code sent to {user.email}.</p> : null}
      </div>
      <FormError>{code.error}</FormError>
      {code.sent ? (
        <Input label="Code" name="code" inputMode="numeric" autoComplete="one-time-code" placeholder="123456" autoFocus error={errors?.code} />
      ) : null}
      <FormError>{error}</FormError>
      {workspaces?.length ? (
        <ul className="grid gap-1 m-0 text-body-sm" style={{ paddingLeft: 20 }}>
          {workspaces.map((w) => (
            <li key={w.slug}>
              <Link to="/$workspace/settings" params={{ workspace: w.slug }} className="auth-link">
                {w.name}
              </Link>
            </li>
          ))}
        </ul>
      ) : null}
      <div className="flex justify-end gap-3">
        <Button variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" variant="danger" disabled={!code.sent || pending}>
          {pending ? 'Deleting…' : 'Delete account'}
        </Button>
      </div>
    </form>
  )
}
