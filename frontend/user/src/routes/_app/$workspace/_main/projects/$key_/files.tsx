import { Link, createFileRoute, notFound, redirect, useLoaderData, useNavigate, useRouter } from '@tanstack/react-router'
import { Fragment, useRef, useState, type ReactNode } from 'react'
import { apiUrl } from '@taskwell/shared/api-url'
import { BackLink } from '@taskwell/shared/back-link'
import { Button } from '@taskwell/shared/button'
import { IconButton } from '@taskwell/shared/icon-button'
import { ProgressRing } from '@taskwell/shared/progress-ring'
import { ChevronRightIcon, FileIcon, FolderIcon, MoreIcon, UploadIcon, XIcon } from '@taskwell/shared/icons'
import { Input } from '@taskwell/shared/input'
import { Popover } from '@taskwell/shared/popover'
import { ApiError, MEDIA_TYPES, can, filesListPath, uploadProjectFile, workspacePath, type FilesPage, type Project } from '@/api'
import { loaderApi, reloadPage } from '@/realtime'
import { formatBytes } from '@/components/files/bytes'
import { FileDialog, type Action, type Item } from '@/components/files/files'
import { formatDate } from '@/components/issues/fields'
import '@/components/files/files.css'
import '@/components/projects/projects.css'

type Search = { folder?: string; q?: string }
type Upload = { id: number; name: string; sent?: number; error: string | null }

let uploadId = 0

export const Route = createFileRoute('/_app/$workspace/_main/projects/$key_/files')({
  // folder: a folder's id, or "attachments" for the issue attachments; q searches names across the project.
  validateSearch: (search): Search => ({
    ...(typeof search.folder === 'string' && search.folder && { folder: search.folder }),
    ...(typeof search.q === 'string' && search.q.trim() && { q: search.q.trim() }),
  }),
  loaderDeps: ({ search }) => ({ folder: search.folder, q: search.q }),
  loader: async (ctx) => {
    const { params, deps } = ctx
    const [project, page] = await Promise.all([
      loaderApi<Project>(ctx, `${workspacePath(params.workspace)}/projects/${encodeURIComponent(params.key)}`),
      loaderApi<FilesPage>(ctx, filesListPath(params.workspace, params.key, deps.folder, deps.q)),
    ]).catch((e) => {
      if (e instanceof ApiError && e.status === 404) throw notFound()
      throw e
    })
    if (project.key !== params.key) {
      throw redirect({ to: '/$workspace/projects/$key/files', params: { workspace: params.workspace, key: project.key }, search: true, replace: true })
    }
    return { project, page }
  },
  component: FilesPageView,
})

function FilesPageView() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { project, page } = Route.useLoaderData()
  const { folder, q = '' } = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  const router = useRouter()
  const picker = useRef<HTMLInputElement>(null)
  const [uploads, setUploads] = useState<Upload[]>([])
  const [dragging, setDragging] = useState(false)
  const [action, setAction] = useState<Action | null>(null)
  const [open, setOpen] = useState(false)
  const [round, setRound] = useState(0)

  const attachments = folder === 'attachments'
  const current = attachments ? undefined : folder
  const canUpload = can(workspace, 'comments.create') && !project.archived && !attachments
  const params = { workspace: workspace.slug, key: project.key }
  const act = (next: Action) => {
    setAction(next)
    setRound((n) => n + 1)
    setOpen(true)
  }

  const crumbs = attachments ? [{ id: 'attachments', name: 'Attachments' }] : [...page.path]
  if (page.folder && crumbs.at(-1)?.id !== page.folder.id) crumbs.push({ id: page.folder.id, name: page.folder.name })

  async function uploadAll(files: File[]) {
    if (!files.length) return
    const batch = files.map((file) => ({ id: ++uploadId, name: file.name, error: null }))
    setUploads((u) => [...u, ...batch])
    const done = new Set<number>()
    await Promise.all(
      files.map((file, i) =>
        uploadProjectFile(workspace.slug, project.key, file, current, (sent) =>
          setUploads((u) => u.map((x) => (x.id === batch[i]!.id ? { ...x, sent } : x))),
        ).then(
          () => void done.add(batch[i]!.id),
          (e: Error) => setUploads((u) => u.map((x) => (x.id === batch[i]!.id ? { ...x, error: e.message } : x))),
        ),
      ),
    )
    if (done.size) await reloadPage(router)
    setUploads((u) => u.filter((x) => !done.has(x.id)))
  }

  const empty = !page.folders.length && !page.files.length
  const showAttachments = !folder && !q

  return (
    <main
      className="file-drop grid gap-6 w-full px-6 py-10"
      style={{ alignContent: 'start' }}
      data-dragging={dragging || undefined}
      onDragOver={(e) => {
        if (!canUpload || !e.dataTransfer.types.includes('Files')) return
        e.preventDefault()
        setDragging(true)
      }}
      onDragLeave={(e) => !e.currentTarget.contains(e.relatedTarget as Node | null) && setDragging(false)}
      onDrop={(e) => {
        e.preventDefault()
        setDragging(false)
        uploadAll([...e.dataTransfer.files])
      }}
    >
      <header className="page-top grid gap-2">
        <BackLink to="/$workspace/projects/$key" params={params}>
          {project.name}
        </BackLink>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <h1 className="text-h2 flex flex-wrap items-center gap-2" style={{ minWidth: 0 }}>
            {crumbs.length ? (
              <Link to="/$workspace/projects/$key/files" params={params} className="file-crumb">
                Files
              </Link>
            ) : (
              'Files'
            )}
            {crumbs.map((c, i) => (
              <Fragment key={c.id}>
                <ChevronRightIcon size={20} className="text-secondary flex-noshrink" />
                {i === crumbs.length - 1 ? (
                  <span className="ellipsis">{c.name}</span>
                ) : (
                  <Link to="/$workspace/projects/$key/files" params={params} search={{ folder: c.id }} className="file-crumb ellipsis">
                    {c.name}
                  </Link>
                )}
              </Fragment>
            ))}
          </h1>
          <div className="flex flex-wrap items-center gap-3">
            <div style={{ width: 240 }}>
              <Input
                type="search"
                aria-label="Search files"
                placeholder="Search files"
                defaultValue={q}
                onDebouncedChange={(v) => navigate({ search: (prev) => ({ ...prev, q: v.trim() || undefined }), replace: true })}
              />
            </div>
            {canUpload ? (
              <>
                <Button variant="secondary" onClick={() => act({ kind: 'new', parent: current ?? null })}>
                  New folder
                </Button>
                <Button className="flex items-center gap-2" onClick={() => picker.current?.click()}>
                  <UploadIcon />
                  Upload
                </Button>
                <input
                  ref={picker}
                  type="file"
                  multiple
                  hidden
                  accept={MEDIA_TYPES}
                  onChange={(e) => {
                    uploadAll([...(e.target.files ?? [])])
                    e.target.value = ''
                  }}
                />
              </>
            ) : null}
          </div>
        </div>
      </header>

      {uploads.length ? (
        <ul className="grid gap-2 m-0 p-4 bg-surface border rounded-lg" style={{ listStyle: 'none' }} aria-label="Uploads" aria-live="polite">
          {uploads.map((u) => (
            <li key={u.id} className="flex items-center gap-3" style={{ minHeight: 44 }}>
              <FileIcon className="text-secondary flex-noshrink" />
              <span className="text-body-sm ellipsis flex-grow">{u.name}</span>
              {u.error ? (
                <>
                  <span className="text-caption text-accent">{u.error}</span>
                  <IconButton aria-label={`Dismiss ${u.name}`} tooltip="Dismiss" onClick={() => setUploads((all) => all.filter((x) => x.id !== u.id))}>
                    <XIcon size={16} />
                  </IconButton>
                </>
              ) : (
                <>
                  <span className="text-caption text-secondary">{u.sent === undefined ? 'Uploading…' : u.sent < 1 ? `${Math.round(u.sent * 100)}%` : 'Saving…'}</span>
                  <ProgressRing value={u.sent} size={20} label={`Uploading ${u.name}`} />
                </>
              )}
            </li>
          ))}
        </ul>
      ) : null}

      {showAttachments || !empty ? (
        <ul className="grid m-0 p-0 bg-surface border rounded-lg overflow-hidden" style={{ listStyle: 'none' }}>
          {showAttachments ? (
            <Row>
              <Link to="/$workspace/projects/$key/files" params={params} search={{ folder: 'attachments' }} className="file-main">
                <span className="file-thumb">
                  <FolderIcon />
                </span>
                <span className="grid" style={{ minWidth: 0 }}>
                  <span className="text-label text-primary ellipsis">Attachments</span>
                  <span className="text-caption text-secondary">From this project's issues and comments</span>
                </span>
              </Link>
            </Row>
          ) : null}
          {page.folders.map((f) => {
            const item: Item = { kind: 'folder', id: f.id, name: f.name, parent: f.parent_id }
            return (
              <Row key={f.id} menu={f.can_edit ? <ItemMenu item={item} onAction={act} /> : null}>
                <Link to="/$workspace/projects/$key/files" params={params} search={{ folder: f.id }} className="file-main">
                  <span className="file-thumb">
                    <FolderIcon />
                  </span>
                  <span className="grid" style={{ minWidth: 0 }}>
                    <span className="text-label text-primary ellipsis">{f.name}</span>
                    <span className="text-caption text-secondary ellipsis">{[f.created_by?.name, formatDate(f.created_at)].filter(Boolean).join(' · ')}</span>
                  </span>
                </Link>
              </Row>
            )
          })}
          {page.files.map((f) => {
            const url = apiUrl(f.url)
            const item: Item = { kind: 'file', id: f.id, name: f.name, parent: q ? undefined : (current ?? null), attachment: attachments }
            return (
              <Row key={f.id} menu={<ItemMenu item={item} url={url} actions={!f.can_edit ? [] : attachments ? ['delete'] : undefined} onAction={act} />}>
                <a href={url} target="_blank" rel="noopener" className="file-main">
                  {f.content_type.startsWith('image/') ? (
                    <img src={url} alt="" width={32} height={32} loading="lazy" className="file-thumb" />
                  ) : (
                    <span className="file-thumb">
                      <FileIcon />
                    </span>
                  )}
                  <span className="grid" style={{ minWidth: 0 }}>
                    <span className="text-label text-primary ellipsis">{f.name}</span>
                    <span className="text-caption text-secondary ellipsis">
                      {[formatBytes(f.size), f.uploader?.name, formatDate(f.created_at)].filter(Boolean).join(' · ')}
                    </span>
                  </span>
                </a>
              </Row>
            )
          })}
        </ul>
      ) : null}

      {empty ? (
        <section className="grid gap-4 p-6 bg-surface border rounded-lg justify-items-center text-center">
          <p className="text-body-sm text-secondary m-0">
            {q ? 'No files match your search.' : attachments ? 'No attachments yet.' : "Upload files or create a folder to keep this project's files together."}
          </p>
        </section>
      ) : null}

      {action ? <FileDialog key={round} workspace={workspace.slug} project={project.key} action={action} open={open} onClose={() => setOpen(false)} /> : null}
    </main>
  )
}

function Row({ children, menu }: { children: ReactNode; menu?: ReactNode }) {
  return (
    <li className="project-row" style={{ paddingTop: 4, paddingBottom: 4 }}>
      {children}
      {menu ? <span className="flex-noshrink">{menu}</span> : null}
    </li>
  )
}

type Kind = 'rename' | 'move' | 'delete'

function ItemMenu({ item, url, actions = ['rename', 'move', 'delete'], onAction }: { item: Item; url?: string; actions?: Kind[]; onAction: (a: Action) => void }) {
  return (
    <Popover
      aria-label={`${item.name} actions`}
      placement="bottom-end"
      trigger={(props) => (
        <IconButton aria-label="More" tooltip="More" style={{ width: 44, height: 44 }} {...props}>
          <MoreIcon size={20} />
        </IconButton>
      )}
    >
      {(close) => (
        <div style={{ width: 160 }}>
          {url ? (
            <a className="popover-item" style={{ height: 44 }} href={url} download={item.name} onClick={close}>
              Download
            </a>
          ) : null}
          {actions.map((kind) => (
                <button key={kind} type="button" className="popover-item" style={{ height: 44 }} onClick={() => (close(), onAction({ kind, item }))}>
                  {kind === 'rename' ? 'Rename' : kind === 'move' ? 'Move to…' : 'Delete'}
                </button>
              ))}
        </div>
      )}
    </Popover>
  )
}
