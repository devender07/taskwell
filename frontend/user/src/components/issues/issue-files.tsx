import { useCallback, useEffect, useState } from 'react'
import { apiUrl } from '@taskwell/shared/api-url'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { IconButton } from '@taskwell/shared/icon-button'
import { FileIcon, XIcon } from '@taskwell/shared/icons'
import { api, deleteProjectFile, filesListPath, toFormErrors, uploadFile, type FilesPage, type Issue, type ProjectFile } from '@/api'
import { subscribePaths } from '@/realtime'
import { formatBytes } from '@/components/files/bytes'
import { formatDate } from '@/components/issues/fields'

type Upload = { id: number; name: string; error: string | null }

const SHOWN = 6
let uploadId = 0

// The issue's files (uploaded from its description's attach button, paste or drop), kept out of the description.
export function useIssueFiles(workspace: string, issue: Issue) {
  const key = issue.project.key
  const path = filesListPath(workspace, key, undefined, undefined, issue.id)
  const [files, setFiles] = useState<ProjectFile[]>([])
  const [uploads, setUploads] = useState<Upload[]>([])
  const load = useCallback(() => void api<FilesPage>(path).then((p) => setFiles(p.files), () => {}), [path])
  useEffect(() => {
    load()
    return subscribePaths([path], load)
  }, [load])

  function upload(list: File[]) {
    setUploads((u) => u.filter((x) => !x.error))
    for (const file of list) {
      const id = ++uploadId
      setUploads((u) => [...u, { id, name: file.name, error: null }])
      uploadFile(workspace, file, key, issue.id).then(
        () => (setUploads((u) => u.filter((x) => x.id !== id)), load()),
        (e: Error) => setUploads((u) => u.map((x) => (x.id === id ? { ...x, error: e.message } : x))),
      )
    }
  }
  const remove = (id: string) => deleteProjectFile(workspace, key, id).then(load)
  const dismiss = (id: number) => setUploads((u) => u.filter((x) => x.id !== id))
  return { files, uploads, upload, remove, dismiss }
}

export type IssueFilesState = ReturnType<typeof useIssueFiles>

export function IssueFiles({ state }: { state: IssueFilesState }) {
  const { files, uploads, dismiss } = state
  const [open, setOpen] = useState(false)
  const [removing, setRemoving] = useState<ProjectFile | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)
  if (!files.length && !uploads.length) return null
  const extra = files.length - SHOWN

  return (
    <section className="grid gap-3" aria-labelledby="issue-files-title">
      <div className="flex items-center justify-between gap-4">
        <h2 id="issue-files-title" className="text-label">
          Files{files.length ? ` (${files.length})` : ''}
        </h2>
        {files.length ? (
          <Button variant="ghost" onClick={() => setOpen(true)}>
            View all
          </Button>
        ) : null}
      </div>
      {files.length ? (
        <ul className="issue-files">
          {files.slice(0, extra > 0 ? SHOWN - 1 : SHOWN).map((f) => (
            <li key={f.id} className="issue-file-cell">
              <a href={apiUrl(f.url)} target="_blank" rel="noopener" className="issue-file" title={f.name}>
                <Thumb file={f} />
              </a>
              {f.can_edit ? (
                <button type="button" className="issue-file-remove" aria-label={`Delete ${f.name}`} title="Delete" onClick={() => (setError(null), setRemoving(f))}>
                  <XIcon size={14} />
                </button>
              ) : null}
            </li>
          ))}
          {extra > 0 ? (
            <li>
              <button type="button" className="issue-file issue-file-more" onClick={() => setOpen(true)}>
                +{extra + 1} more
              </button>
            </li>
          ) : null}
        </ul>
      ) : null}
      {uploads.length ? (
        <ul className="grid gap-1 m-0 p-0" style={{ listStyle: 'none' }} aria-label="Uploads" aria-live="polite">
          {uploads.map((u) => (
            <li key={u.id} className="flex items-center gap-2 text-caption" style={{ minHeight: 32 }}>
              {u.error ? (
                <>
                  <span className="text-accent">
                    Couldn't upload {u.name}: {u.error}
                  </span>
                  <IconButton aria-label={`Dismiss ${u.name}`} tooltip="Dismiss" onClick={() => dismiss(u.id)}>
                    <XIcon size={16} />
                  </IconButton>
                </>
              ) : (
                <span className="text-secondary">Uploading {u.name}…</span>
              )}
            </li>
          ))}
        </ul>
      ) : null}
      <Dialog open={!!removing} onClose={() => setRemoving(null)} title={`Delete ${removing?.name ?? 'file'}?`} description="This can't be undone.">
        <div className="grid gap-4">
          {error ? (
            <p role="alert" className="text-caption text-accent m-0">
              {error}
            </p>
          ) : null}
          <Button
            variant="danger"
            className="w-full"
            disabled={pending}
            onClick={() => {
              setPending(true)
              state.remove(removing!.id).then(
                () => (setPending(false), setRemoving(null)),
                (e) => (setPending(false), setError(toFormErrors(e).error)),
              )
            }}
          >
            {pending ? 'Deleting…' : 'Delete file'}
          </Button>
        </div>
      </Dialog>
      <Dialog open={open} onClose={() => setOpen(false)} title="Files" wide>
        <FileList state={state} />
      </Dialog>
    </section>
  )
}

function FileList({ state }: { state: IssueFilesState }) {
  const [confirm, setConfirm] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  if (!state.files.length) return <p className="text-body-sm text-secondary m-0">No files.</p>
  return (
    <div className="grid gap-2">
      {error ? (
        <p role="alert" className="text-caption text-accent m-0">
          {error}
        </p>
      ) : null}
      <ul className="grid m-0 p-0" style={{ listStyle: 'none' }}>
        {state.files.map((f) => {
          const url = apiUrl(f.url)
          return (
            <li key={f.id} className="flex items-center gap-3" style={{ minHeight: 56 }}>
              <a href={url} target="_blank" rel="noopener" className="issue-file issue-file-sm" aria-hidden tabIndex={-1}>
                <Thumb file={f} />
              </a>
              <span className="grid flex-grow" style={{ minWidth: 0 }}>
                <a href={url} target="_blank" rel="noopener" className="text-label text-primary ellipsis">
                  {f.name}
                </a>
                <span className="text-caption text-secondary ellipsis">{[formatBytes(f.size), f.uploader?.name, formatDate(f.created_at)].filter(Boolean).join(' · ')}</span>
              </span>
              <a className="button button-ghost flex-noshrink" href={url} download={f.name}>
                Download
              </a>
              {f.can_edit ? (
                <Button
                  variant={confirm === f.id ? 'danger' : 'ghost'}
                  className="flex-noshrink"
                  onClick={() => {
                    if (confirm !== f.id) return setConfirm(f.id)
                    setError(null)
                    state.remove(f.id).catch((e) => setError(toFormErrors(e).error))
                  }}
                  onBlur={() => setConfirm((c) => (c === f.id ? null : c))}
                >
                  {confirm === f.id ? 'Confirm delete' : 'Delete'}
                </Button>
              ) : null}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

function Thumb({ file }: { file: ProjectFile }) {
  const url = apiUrl(file.url)
  if (file.content_type.startsWith('image/')) return <img src={url} alt={file.name} loading="lazy" />
  if (file.content_type.startsWith('video/')) return <video src={url} muted preload="metadata" aria-label={file.name} />
  return <FileIcon aria-label={file.name} />
}
