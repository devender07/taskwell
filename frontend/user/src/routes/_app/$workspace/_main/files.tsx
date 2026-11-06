import { Link, createFileRoute } from '@tanstack/react-router'
import { FolderIcon } from '@taskwell/shared/icons'
import { storagePath, workspacePath, type Project, type StorageUsage } from '@/api'
import { loaderApi } from '@/realtime'
import { formatBytes } from '@/components/files/bytes'
import '@/components/files/files.css'
import '@/components/home/home.css'
import '@/components/projects/projects.css'

export const Route = createFileRoute('/_app/$workspace/_main/files')({
  loader: async (ctx) => {
    const [projects, storage] = await Promise.all([
      loaderApi<Project[]>(ctx, `${workspacePath(ctx.params.workspace)}/projects`),
      loaderApi<StorageUsage>(ctx, storagePath(ctx.params.workspace)),
    ])
    return { projects, storage }
  },
  component: FileManager,
})

function FileManager() {
  const { workspace } = Route.useParams()
  const { projects, storage } = Route.useLoaderData()
  const { used_bytes: used, quota_bytes: quota } = storage
  const usage = `${formatBytes(used)} of ${formatBytes(quota)} used`

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top grid gap-3">
        <h1 className="text-h2">Files</h1>
        <div className="grid gap-2" style={{ maxWidth: 400 }}>
          <span className="text-caption text-secondary">{usage}</span>
          <span role="progressbar" aria-label="Storage used" aria-valuemin={0} aria-valuemax={quota} aria-valuenow={used} aria-valuetext={usage} className="home-bar">
            <span style={{ width: quota ? `${Math.min(100, (used / quota) * 100)}%` : 0 }} />
          </span>
        </div>
      </header>
      {projects.length ? (
        <ul className="grid m-0 p-0 bg-surface border rounded-lg overflow-hidden" style={{ listStyle: 'none' }}>
          {projects.map((p) => (
            <li key={p.id} className="project-row" style={{ paddingTop: 4, paddingBottom: 4 }}>
              <Link to="/$workspace/projects/$key/files" params={{ workspace, key: p.key }} className="file-main">
                <span className="file-thumb">
                  <FolderIcon />
                </span>
                <span className="grid" style={{ minWidth: 0 }}>
                  <span className="text-label text-primary ellipsis">{p.name}</span>
                  <span className="text-caption text-secondary">{p.key}</span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <section className="grid gap-4 p-6 bg-surface border rounded-lg justify-items-center text-center">
          <p className="text-body-sm text-secondary m-0">Files live in projects. Create a project to start uploading.</p>
        </section>
      )}
    </main>
  )
}
