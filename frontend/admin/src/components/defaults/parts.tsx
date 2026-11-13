import { useRouter } from '@tanstack/react-router'
import { useActionState, useState, type ReactNode } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { STATUS_CATEGORIES } from '@taskwell/shared/validate'
import { deleteDefault, toFormErrors, type DefaultsKind, type FieldErrors, type StatusCategory } from '@/api'

export type State = { errors?: FieldErrors; error?: string | null }

export const CATEGORIES = STATUS_CATEGORIES.map((value) => ({ value: value as StatusCategory, label: value.charAt(0).toUpperCase() + value.slice(1) }))
export const categoryLabel = (c: StatusCategory) => CATEGORIES.find((x) => x.value === c)?.label ?? c

export function FormError({ children }: { children?: string | null }) {
  return children ? <p role="alert" className="text-caption text-accent m-0">{children}</p> : null
}

export function Section({ title, description, action, children }: { title: string; description: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section className="grid gap-4" aria-label={title}>
      <header className="flex flex-wrap items-end gap-3">
        <div className="grid gap-1 flex-grow">
          <h2 className="m-0" style={{ font: '590 20px/26.6px var(--font)' }}>{title}</h2>
          <p className="text-body-sm text-secondary m-0">{description}</p>
        </div>
        {action}
      </header>
      {children}
    </section>
  )
}

export function ConfirmDelete({ kind, id, name, noun, description, disabled }: { kind: DefaultsKind; id: string; name: string; noun: string; description: string; disabled?: boolean }) {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [error, remove, pending] = useActionState(async () => {
    try {
      await deleteDefault(kind, id)
    } catch (e) {
      return toFormErrors(e).error
    }
    await router.invalidate()
    setOpen(false)
    return null
  }, null)

  return (
    <>
      <Button variant="ghost" disabled={disabled} onClick={() => setOpen(true)}>
        Delete
      </Button>
      <Dialog open={open} onClose={() => setOpen(false)} title={`Delete ${name}?`} description={description}>
        <form action={remove} className="grid gap-4">
          <FormError>{error}</FormError>
          <div className="flex justify-end gap-3">
            <Button variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="submit" variant="danger" disabled={pending}>
              {pending ? 'Deleting…' : `Delete ${noun}`}
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  )
}
