import { useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { required } from '@taskwell/shared/validate'
import { api, toFormErrors, type AdminAccount, type FieldErrors } from '@/api'

type State = { errors?: FieldErrors; error?: string | null }

export function RemoveAdminButton({ admin }: { admin: AdminAccount }) {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [formKey, setFormKey] = useState(0)

  const close = () => {
    setOpen(false)
    setFormKey((k) => k + 1)
  }

  const [state, confirm, pending] = useActionState(async (_prev: State, form: FormData): Promise<State> => {
    const error = required('Your password')(String(form.get('current_password') ?? ''))
    if (error) return { errors: { current_password: error } }
    try {
      await api(`/admins/${encodeURIComponent(admin.id)}/remove`, form)
    } catch (e) {
      return toFormErrors(e)
    }
    close()
    await router.invalidate()
    return {}
  }, {})

  return (
    <>
      <Button variant="ghost" onClick={() => setOpen(true)}>Remove</Button>
      <Dialog
        open={open}
        onClose={close}
        title={`Remove ${admin.name}?`}
        description={`${admin.email} is signed out of the admin app right away and can't sign in again. Their app account, if they have one, isn't touched.`}
      >
        <form key={formKey} action={confirm} noValidate className="grid gap-4">
          <Input label="Your password" name="current_password" type="password" autoComplete="current-password" placeholder="To confirm it's you" error={state.errors?.current_password} />
          {state.error ? <p role="alert" className="text-caption text-accent">{state.error}</p> : null}
          <div className="flex justify-end gap-3">
            <Button variant="ghost" onClick={close}>Cancel</Button>
            <Button type="submit" variant="danger" disabled={pending}>
              {pending ? 'Removing…' : 'Remove'}
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  )
}
