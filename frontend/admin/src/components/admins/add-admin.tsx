import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { email as checkEmail, name as checkName, password as checkPassword, required } from '@taskwell/shared/validate'
import { api, toFormErrors, type AdminAccount, type Enrolment, type FieldErrors } from '@/api'

type State = { values: Record<string, string>; errors?: FieldErrors; error?: string | null }
type Created = { admin: AdminAccount; enrolment: Enrolment }

const mono = { fontFamily: 'var(--mono)', overflowWrap: 'anywhere' } as const
const empty: State = { values: {} }

export function AddAdminButton() {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [created, setCreated] = useState<Created | null>(null)
  const [formKey, setFormKey] = useState(0)

  const close = () => {
    setOpen(false)
    setCreated(null)
    setFormKey((k) => k + 1)
  }

  const [state, submit, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const values = { name: String(form.get('name') ?? '').trim(), email: String(form.get('email') ?? '').trim() }
    const errors = {
      name: checkName(values.name),
      email: checkEmail(values.email),
      password: checkPassword(String(form.get('password') ?? '')),
      current_password: required('Your password')(String(form.get('current_password') ?? '')),
    }
    if (Object.values(errors).some(Boolean)) return { values, errors }
    form.set('name', values.name)
    form.set('email', values.email)
    try {
      setCreated(await api<Created>('/admins', form))
    } catch (e) {
      return { values, ...toFormErrors(e) }
    }
    await router.invalidate()
    return empty
  }, empty)

  return (
    <>
      <Button onClick={() => setOpen(true)}>Add admin</Button>
      <Dialog
        open={open}
        onClose={close}
        dismissible={!created}
        title={created ? `${created.admin.name} is an admin` : 'Add admin'}
        description={
          created
            ? 'Send these to them over a safe channel. They are shown only now.'
            : 'They sign in with this email and password, plus a code from their authenticator app.'
        }
      >
        {created ? (
          <Credentials created={created} onDone={close} />
        ) : (
          <form key={formKey} action={submit} noValidate className="grid gap-4">
            <Input label="Name" name="name" autoComplete="off" placeholder="Ada Lovelace" defaultValue={state.values.name} error={state.errors?.name} />
            <Input label="Email" name="email" type="email" autoComplete="off" placeholder="ada@company.com" defaultValue={state.values.email} error={state.errors?.email} />
            <Input label="Their password" name="password" type="password" autoComplete="new-password" placeholder="At least 8 characters" error={state.errors?.password} />
            <Input label="Your password" name="current_password" type="password" autoComplete="current-password" placeholder="To confirm it's you" error={state.errors?.current_password} />
            {state.error ? <p role="alert" className="text-caption text-accent">{state.error}</p> : null}
            <div className="flex justify-end gap-3">
              <Button variant="ghost" onClick={close}>Cancel</Button>
              <Button type="submit" disabled={pending}>{pending ? 'Adding…' : 'Add admin'}</Button>
            </div>
          </form>
        )}
      </Dialog>
    </>
  )
}

function Credentials({ created, onDone }: { created: Created; onDone: () => void }) {
  const { admin, enrolment } = created
  const [copied, setCopied] = useState('')
  const text = [
    `Taskwell admin: ${admin.email}`,
    `Authenticator setup key: ${enrolment.secret}`,
    `otpauth URL: ${enrolment.url}`,
    '',
    'Recovery codes (each signs in once):',
    ...enrolment.recovery_codes,
  ].join('\n')

  return (
    <div className="grid gap-4">
      <div className="grid gap-2">
        <span className="text-label">Authenticator setup key</span>
        <code className="p-3 border rounded" style={{ ...mono, background: 'rgba(var(--ink), 0.02)' }}>{enrolment.secret}</code>
        <span className="text-caption text-secondary">Time-based, 6 digits, 30 seconds.</span>
      </div>
      <div className="grid gap-2">
        <span className="text-label">Recovery codes</span>
        <ul className="grid gap-1 p-3 m-0 border rounded" style={{ listStyle: 'none', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))' }}>
          {enrolment.recovery_codes.map((c) => (
            <li key={c} className="text-body-sm" style={mono}>{c}</li>
          ))}
        </ul>
      </div>
      {copied ? <p role="status" className="text-caption text-secondary">{copied}</p> : null}
      <div className="flex justify-end gap-3">
        <Button
          variant="secondary"
          onClick={() => navigator.clipboard.writeText(text).then(() => setCopied('Copied.'), () => setCopied("Couldn't copy. Select the text instead."))}
        >
          Copy all
        </Button>
        <Button onClick={onDone}>Done</Button>
      </div>
    </div>
  )
}
