import { Suspense, lazy } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { RichText, type Doc } from '@taskwell/shared/rich-text'
import { avatarSrc, type Issue, type User, type Workspace } from '@/api'
import type { PeopleSearch } from '@/components/workspace/member-pages'
import type { Viewer } from '@/realtime'

export type DescriptionProps = { workspace: Workspace; issue: Issue; user: User; people: PeopleSearch; onViewers: (viewers: Viewer[]) => void }

// The editor and Yjs load in the background; until then (and until live editing connects) the saved description shows
// read-only, so there is nothing to type into that could be lost.
const LiveDescription = lazy(() => import('./live-description'))

export function Description(props: DescriptionProps) {
  const fallback = <StaticDescription doc={props.issue.description} canEdit={props.issue.can_edit} />
  return (
    <Suspense fallback={<section className="grid gap-2">{fallback}</section>}>
      <LiveDescription {...props} />
    </Suspense>
  )
}

export function StaticDescription({ doc, canEdit }: { doc: Doc | null; canEdit: boolean }) {
  if (doc) return <RichText aria-label="Description" doc={doc} />
  return canEdit ? null : <p className="text-body-sm text-secondary m-0">No description.</p>
}

const SHOWN_VIEWERS = 5

export function Viewers({ viewers }: { viewers: Viewer[] }) {
  if (viewers.length === 0) return null
  const extra = viewers.length - SHOWN_VIEWERS
  return (
    <div
      role="img"
      aria-label={`Also viewing: ${viewers.map((v) => v.name).join(', ')}`}
      className="flex items-center flex-noshrink"
      style={{ height: 40, paddingLeft: 4 }}
    >
      {viewers.slice(0, SHOWN_VIEWERS).map((v) => (
        <span key={v.id} title={v.name} className="inline-grid rounded-full" style={{ marginLeft: -4, boxShadow: '0 0 0 2px var(--bg)' }}>
          <Avatar name={v.name} size={24} src={avatarSrc(v.id)} />
        </span>
      ))}
      {extra > 0 ? (
        <span className="text-caption text-secondary" style={{ marginLeft: 4 }} title={viewers.slice(SHOWN_VIEWERS).map((v) => v.name).join(', ')}>
          +{extra}
        </span>
      ) : null}
    </div>
  )
}
