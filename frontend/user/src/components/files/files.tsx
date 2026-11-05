import { useRouter } from '@tanstack/react-router'
import { useActionState, useEffect, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { ChevronRightIcon, FolderIcon } from '@taskwell/shared/icons'
import { Input } from '@taskwell/shared/input'
import { filled, folderName } from '@taskwell/shared/validate'
import { api, deleteFolder, deleteProjectFile, filesListPath, saveFolder, toFormErrors, updateProjectFile, type Folder } from '@/api'
import { reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'

// parent: the folder it sits in (null = root), undefined when unknown (a search result).
export type Item = { kind: 'folder' | 'file'; id: string; name: string; parent?: string | null; attachment?: boolean }
export type Action = { kind: 'new'; parent: string | null } | { kind: 'rename' | 'move' | 'delete'; item: Item }

type Props = { workspace: string; project: string; action: Action; open: boolean; onClose: () => void }

export function FileDialog(props: Props) {
  const { action } = props
  if (action.kind === 'new') return <NameDialog {...props} title="New folder" submit="Create folder" />
  if (action.kind === 'rename') return <NameDialog {...props} title={`Rename ${action.item.kind}`} submit="Rename" item={action.item} />
  if (action.kind === 'move') return <MoveDialog {...props} item={action.item} />
  return <DeleteDialog {...props} item={action.item} />
}

function NameDialog({ workspace, project, action, open, onClose, title, submit, item }: Props & { title: string; submit: string; item?: Item }) {
  const router = useRouter()
  const [state, save, pending] = useActionState(async (_prev: { name?: string; error?: string | null }, form: FormData) => {
    const name = String(form.get('name') ?? '').trim()
    const invalid = item?.kind === 'file' ? filled('Enter a name.')(name) : folderName(name)
    if (invalid) return { name: invalid }
    form.set('name', name)
    try {
      if (!item) {
        if (action.kind === 'new' && action.parent) form.set('parent_id', action.parent)
        await saveFolder(workspace, project, form)
      } else if (item.kind === 'folder') await saveFolder(workspace, project, form, item.id)
      else await updateProjectFile(workspace, project, item.id, form)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return errors.name ? { name: errors.name } : { error }
    }
    await reloadPage(router)
    onClose()
    return {}
  }, {})

  return (
    <Dialog open={open} onClose={onClose} title={title}>
      <form action={save} noValidate className="grid gap-4">
        <Input name="name" label="Name" autoComplete="off" autoFocus defaultValue={item?.name} error={state.name} />
        <FormError>{state.error}</FormError>
        <Button type="submit" className="w-full" disabled={pending}>
          {pending ? 'Saving…' : submit}
        </Button>
      </form>
    </Dialog>
  )
}

type Crumb = { id: string | null; name: string }

// Browses the folder tree one level at a time; the folder being moved (and so everything inside it) is left out.
function MoveDialog({ workspace, project, open, onClose, item }: Props & { item: Item }) {
  const router = useRouter()
  const [trail, setTrail] = useState<Crumb[]>([{ id: null, name: 'Files' }])
  const [folders, setFolders] = useState<Folder[] | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const here = trail.at(-1)!.id

  useEffect(() => {
    const ctrl = new AbortController()
    setFolders(null)
    setLoadError(null)
    api<{ folders: Folder[] }>(filesListPath(workspace, project, here ?? undefined), undefined, 'GET', ctrl.signal).then(
      (page) => setFolders(page.folders.filter((f) => !(item.kind === 'folder' && f.id === item.id))),
      (e) => !ctrl.signal.aborted && setLoadError(toFormErrors(e).error),
    )
    return () => ctrl.abort()
  }, [workspace, project, here, item])

  const [error, move, pending] = useActionState(async () => {
    const form = new FormData()
    form.set(item.kind === 'folder' ? 'parent_id' : 'folder_id', here ?? '')
    try {
      if (item.kind === 'folder') await saveFolder(workspace, project, form, item.id)
      else await updateProjectFile(workspace, project, item.id, form)
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      return errors.parent_id ?? errors.folder_id ?? error
    }
    await reloadPage(router)
    onClose()
    return null
  }, null)

  return (
    <Dialog open={open} onClose={onClose} title={`Move ${item.name}`}>
      <form action={move} className="grid gap-4">
        <nav aria-label="Folder" className="flex flex-wrap items-center gap-1 text-body-sm">
          {trail.map((c, i) =>
            i === trail.length - 1 ? (
              <span key={c.id ?? ''} className="text-primary" aria-current="location">{c.name}</span>
            ) : (
              <span key={c.id ?? ''} className="flex items-center gap-1">
                <button type="button" className="file-crumb" onClick={() => setTrail(trail.slice(0, i + 1))}>
                  {c.name}
                </button>
                <ChevronRightIcon size={14} className="text-secondary" />
              </span>
            ),
          )}
        </nav>
        <ul className="grid m-0 p-0 border rounded-lg overflow-y-auto" style={{ listStyle: 'none', maxHeight: 280 }}>
          {loadError ? (
            <li className="p-4 text-caption text-accent">{loadError}</li>
          ) : folders === null ? (
            <li className="p-4 text-caption text-secondary">Loading…</li>
          ) : folders.length === 0 ? (
            <li className="p-4 text-caption text-secondary">No folders here.</li>
          ) : (
            folders.map((f) => (
              <li key={f.id}>
                <button type="button" className="popover-item flex items-center gap-3" style={{ height: 44 }} onClick={() => setTrail([...trail, { id: f.id, name: f.name }])}>
                  <FolderIcon className="text-secondary flex-noshrink" />
                  <span className="ellipsis">{f.name}</span>
                </button>
              </li>
            ))
          )}
        </ul>
        <FormError>{error}</FormError>
        <Button type="submit" className="w-full" disabled={pending || here === item.parent}>
          {pending ? 'Moving…' : 'Move here'}
        </Button>
      </form>
    </Dialog>
  )
}

function DeleteDialog({ workspace, project, open, onClose, item }: Props & { item: Item }) {
  const router = useRouter()
  const [error, remove, pending] = useActionState(async () => {
    try {
      if (item.kind === 'folder') await deleteFolder(workspace, project, item.id)
      else await deleteProjectFile(workspace, project, item.id)
    } catch (e) {
      return toFormErrors(e).error
    }
    await reloadPage(router)
    onClose()
    return null
  }, null)

  return (
    <Dialog open={open} onClose={onClose} title={`Delete ${item.name}?`} description={item.kind === 'folder' ? 'Only an empty folder can be deleted.' : item.attachment ? "It's removed from the issue or comment it was added to. This can't be undone." : "This can't be undone."}>
      <form action={remove} className="grid gap-4">
        <FormError>{error}</FormError>
        <Button type="submit" variant="danger" className="w-full" disabled={pending}>
          {pending ? 'Deleting…' : `Delete ${item.kind}`}
        </Button>
      </form>
    </Dialog>
  )
}
