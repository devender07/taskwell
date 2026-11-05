import { deepEqual, defaultParseSearch, useLoaderData, useLocation, useRouter, type AnyRouter } from '@tanstack/react-router'
import { startTransition, useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Checkbox } from '@taskwell/shared/checkbox'
import { Dialog } from '@taskwell/shared/dialog'
import { Input } from '@taskwell/shared/input'
import { Popover } from '@taskwell/shared/popover'
import { ChevronDownIcon, LayersIcon } from '@taskwell/shared/icons'
import { viewName } from '@taskwell/shared/validate'
import { api, toFormErrors, viewPath, viewsPath, type FieldErrors, type SavedView, type Workspace } from '@/api'
import { reloadRoutes } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'

const MAIN = '/_app/$workspace/_main'

// This tab's own changes skip its realtime event, so the layout holding the views reloads by hand.
const reloadViews = (router: AnyRouter) => reloadRoutes(router, [MAIN])

// True when the view is this page: the same path under the workspace and the same search.
export function matchesView(view: SavedView, path: string, search: object) {
  const [pathname, query = ''] = view.path.split('?')
  return pathname === path && deepEqual(defaultParseSearch(query), search)
}

type Dialogs = 'save' | 'rename' | 'delete' | null

// The issue toolbar's "Save view", or the menu of the saved view this page is.
export function ViewMenu({ workspace }: { workspace: Workspace }) {
  const views = useLoaderData({ from: MAIN })
  const location = useLocation()
  const router = useRouter()
  const [dialog, setDialog] = useState<Dialogs>(null)
  const path = location.pathname.slice(workspace.slug.length + 1)
  const view = views.find((v) => v.can_edit && matchesView(v, path, location.search))
  const close = () => setDialog(null)

  const [error, share] = useActionState(async (_prev: string | null, v: SavedView) => {
    const form = new FormData()
    form.set('shared', v.shared ? '0' : '1')
    try {
      await api<SavedView>(viewPath(workspace.slug, v.id), form)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return Object.values(errors).find(Boolean) ?? error
    }
    await reloadViews(router)
    return null
  }, null)

  if (!view) {
    return (
      <>
        <Button variant="ghost" onClick={() => setDialog('save')}>
          Save view
        </Button>
        <Dialog open={dialog === 'save'} onClose={close} title="Save view">
          <ViewForm workspace={workspace} path={path + location.searchStr} onDone={close} />
        </Dialog>
      </>
    )
  }

  return (
    <>
      <Popover
        aria-label="View"
        trigger={(props) => (
          <Button variant="ghost" {...props} style={{ maxWidth: 240 }}>
            <LayersIcon size={16} className="flex-noshrink" />
            <span className="ellipsis">{view.name}</span>
            <ChevronDownIcon size={16} className="flex-noshrink" />
          </Button>
        )}
      >
        {(closeMenu) => (
          <div className="p-1" style={{ width: 200 }}>
            <button type="button" className="popover-item" onClick={() => (closeMenu(), setDialog('rename'))}>
              Rename
            </button>
            <button type="button" className="popover-item" onClick={() => (closeMenu(), startTransition(() => share(view)))}>
              {view.shared ? 'Stop sharing' : 'Share with workspace'}
            </button>
            <button type="button" className="popover-item" onClick={() => (closeMenu(), setDialog('delete'))}>
              Delete view
            </button>
          </div>
        )}
      </Popover>
      <FormError>{error}</FormError>
      <Dialog open={dialog === 'rename'} onClose={close} title="Rename view">
        <ViewForm workspace={workspace} view={view} path={path} onDone={close} />
      </Dialog>
      <DeleteView workspace={workspace} view={view} open={dialog === 'delete'} onClose={close} />
    </>
  )
}

type State = { errors?: FieldErrors; error?: string | null }

// Saves a new view of `path`, or renames `view`.
function ViewForm({ workspace, view, path, onDone }: { workspace: Workspace; view?: SavedView; path: string; onDone: () => void }) {
  const router = useRouter()
  const [name, setName] = useState(view?.name ?? '')
  const [shared, setShared] = useState(false)
  const [{ errors, error }, submit, pending] = useActionState(async (): Promise<State> => {
    const invalid = viewName(name)
    if (invalid) return { errors: { name: invalid } }
    const form = new FormData()
    form.set('name', name.trim())
    if (!view) {
      form.set('path', path)
      form.set('shared', shared ? '1' : '0')
    }
    try {
      await api<SavedView>(view ? viewPath(workspace.slug, view.id) : viewsPath(workspace.slug), form)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return { errors, error: error ?? errors.path ?? errors.shared }
    }
    await reloadViews(router)
    onDone()
    return {}
  }, {})

  return (
    <form action={submit} noValidate className="grid gap-4">
      <Input label="Name" autoFocus autoComplete="off" value={name} onChange={(e) => setName(e.target.value)} error={errors?.name} />
      {view ? null : <Checkbox label="Share with workspace" checked={shared} onChange={(e) => setShared(e.target.checked)} />}
      <FormError>{error}</FormError>
      <Button type="submit" className="w-full" disabled={pending}>
        {pending ? 'Saving…' : 'Save'}
      </Button>
    </form>
  )
}

function DeleteView({ workspace, view, open, onClose }: { workspace: Workspace; view: SavedView; open: boolean; onClose: () => void }) {
  const router = useRouter()
  const [error, remove, pending] = useActionState(async () => {
    try {
      await api(viewPath(workspace.slug, view.id), undefined, 'DELETE')
    } catch (e) {
      return toFormErrors(e).error
    }
    onClose()
    await reloadViews(router)
    return null
  }, null)

  return (
    <Dialog open={open} onClose={onClose} title={`Delete ${view.name}?`} description={view.shared ? 'It goes away for everyone in the workspace.' : undefined}>
      <form action={remove} className="grid gap-4">
        <FormError>{error}</FormError>
        <Button type="submit" variant="danger" className="w-full" disabled={pending}>
          {pending ? 'Deleting…' : 'Delete view'}
        </Button>
      </form>
    </Dialog>
  )
}
