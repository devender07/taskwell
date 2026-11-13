import { createFileRoute, useLoaderData, useRouter } from '@tanstack/react-router'
import { useActionState, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { fullTime, relativeTime } from '@taskwell/shared/time'
import { api, can, emptyTrash, purgeIssue, restoreIssue, toFormErrors, trashPath, type TrashedIssue, type TrashPage, type Workspace } from '@/api'
import { loaderApi, reloadPage } from '@/realtime'
import { FormError } from '@/components/auth/auth-card'
import { extractDataFromArray } from '@taskwell/shared/list'

export const Route = createFileRoute('/_app/$workspace/_main/trash')({
  loader: (ctx) => loaderApi<TrashPage>(ctx, trashPath(ctx.params.workspace)),
  component: Trash,
})

function Trash() {
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const page = Route.useLoaderData()
  const purge = can(workspace, 'issues.edit_any')
  const [extra, setExtra] = useState<{ base: TrashPage; issues: TrashedIssue[]; has_more: boolean } | null>(null)
  const [loading, setLoading] = useState(false)
  const [moreError, setMoreError] = useState<string | null>(null)
  const [emptying, setEmptying] = useState(false)
  const more = extra?.base === page ? extra : null
  const issues = [...page.issues, ...extractDataFromArray(more, 'issues', [])]
  const hasMore = more ? more.has_more : page.has_more

  async function loadMore() {
    const last = issues.at(-1)
    if (!last) return
    setLoading(true)
    setMoreError(null)
    try {
      const next = await api<TrashPage>(`${trashPath(workspace.slug)}?${new URLSearchParams({ before: last.deleted_at })}`)
      setExtra({ base: page, issues: [...extractDataFromArray(more, 'issues', []), ...next.issues], has_more: next.has_more })
    } catch (e) {
      setMoreError(toFormErrors(e).error)
    }
    setLoading(false)
  }

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <header className="page-top flex flex-wrap items-end justify-between gap-4">
        <div className="grid gap-2">
          <h1 className="text-h2">Trash</h1>
          <p className="text-body-sm text-secondary">Deleted issues stay here for 30 days, then they're gone for good.</p>
        </div>
        {purge && issues.length ? (
          <Button variant="secondary" onClick={() => setEmptying(true)}>
            Empty trash
          </Button>
        ) : null}
      </header>
      {issues.length ? (
        <ul className="grid m-0 p-0 bg-surface border rounded-lg" style={{ listStyle: 'none' }}>
          {issues.map((issue) => (
            <TrashRow key={issue.id} workspace={workspace} issue={issue} purge={purge} />
          ))}
        </ul>
      ) : (
        <p className="text-body-sm text-secondary p-6 bg-surface border rounded-lg text-center">Trash is empty. Deleted issues stay here for 30 days.</p>
      )}
      <FormError>{moreError}</FormError>
      {hasMore ? (
        <Button variant="ghost" style={{ justifySelf: 'center' }} disabled={loading} onClick={loadMore}>
          {loading ? 'Loading…' : 'Show more'}
        </Button>
      ) : null}
      <EmptyTrash workspace={workspace} open={emptying} onClose={() => setEmptying(false)} />
    </main>
  )
}

function TrashRow({ workspace, issue, purge }: { workspace: Workspace; issue: TrashedIssue; purge: boolean }) {
  const router = useRouter()
  const [busy, setBusy] = useState<'restore' | 'purge' | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function run(kind: 'restore' | 'purge') {
    setBusy(kind)
    setError(null)
    try {
      await (kind === 'restore' ? restoreIssue(workspace.slug, issue.identifier) : purgeIssue(workspace.slug, issue.identifier))
      await reloadPage(router)
    } catch (e) {
      setError(toFormErrors(e).error)
    }
    setBusy(null)
  }

  return (
    <li className="grid gap-2 px-6 py-3">
      <div className="flex flex-wrap items-center gap-3">
        <div className="grid flex-grow" style={{ minWidth: 0, flexBasis: 240 }}>
          <span className="text-label text-primary ellipsis">
            <span className="text-secondary font-400" style={{ fontFamily: 'var(--mono)' }}>{issue.identifier}</span> {issue.title}
          </span>
          <span className="text-caption text-secondary ellipsis">
            {issue.project.name} · {issue.deleted_by ? `Deleted by ${issue.deleted_by.name}` : 'Deleted'} ·{' '}
            <time dateTime={issue.deleted_at} title={fullTime(issue.deleted_at)}>{relativeTime(issue.deleted_at)}</time>
          </span>
        </div>
        <div className="flex gap-2 flex-noshrink">
          {issue.can_restore ? (
            <Button variant="secondary" disabled={busy !== null} onClick={() => run('restore')} aria-label={`Restore ${issue.identifier}`}>
              {busy === 'restore' ? 'Restoring…' : 'Restore'}
            </Button>
          ) : null}
          {purge ? (
            <Button variant="ghost" style={{ color: 'var(--accent)' }} disabled={busy !== null} onClick={() => run('purge')} aria-label={`Delete ${issue.identifier} forever`}>
              {busy === 'purge' ? 'Deleting…' : 'Delete forever'}
            </Button>
          ) : null}
        </div>
      </div>
      <FormError>{error}</FormError>
    </li>
  )
}

function EmptyTrash({ workspace, open, onClose }: { workspace: Workspace; open: boolean; onClose: () => void }) {
  const router = useRouter()
  const [error, empty, pending] = useActionState(async () => {
    try {
      await emptyTrash(workspace.slug)
    } catch (e) {
      return toFormErrors(e).error
    }
    await reloadPage(router)
    onClose()
    return null
  }, null)

  return (
    <Dialog open={open} onClose={onClose} title="Empty trash?" description="Every issue in the trash is deleted for good, with its comments and history. This can't be undone.">
      <form action={empty} className="grid gap-4">
        <FormError>{error}</FormError>
        <Button type="submit" variant="danger" className="w-full" disabled={pending}>
          {pending ? 'Emptying…' : 'Empty trash'}
        </Button>
      </form>
    </Dialog>
  )
}
