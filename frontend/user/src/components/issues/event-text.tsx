import type { ReactNode } from 'react'
import type { IssueEvent } from '@/api'
import { formatDate, priorityLabel } from './fields'
import { PROVIDER_NAME, prRef } from './pull-requests'

type Ref = { identifier: string; title: string }

// What an issue event did, after the actor's name. `subject` names the issue for feeds outside it ("moved ENG-12 …");
// the timeline leaves it out ("moved from Todo to Done"). `ref` renders the other issue in parent and link events.
export function eventText(e: IssueEvent, ref: (issue: Ref) => ReactNode, subject?: ReactNode): ReactNode {
  const s = subject ? <> {subject}</> : null
  const of = subject ? <> of{s}</> : null
  const from = subject ? <> from{s}</> : null
  const names = (ls: { name: string }[]) => ls.map((l) => l.name).join(', ')
  switch (e.kind) {
    case 'created':
      return subject ? <>created{s}</> : 'created the issue'
    case 'description':
      return <>edited the description{of}</>
    case 'title':
      return <>changed the title{of} to “{e.data.to}”</>
    case 'status': {
      const { via } = e.data
      if (via) return <>moved{s} to {e.data.to.name} when {prRef(via)} {e.data.to.category === 'completed' ? 'merged' : 'opened'}</>
      return <>moved{s} from {e.data.from.name} to {e.data.to.name}</>
    }
    case 'pr_linked':
    case 'pr_unlinked': {
      const kind = e.data.provider === 'gitlab' ? 'merge request' : 'pull request'
      return e.kind === 'pr_linked' ? <>linked {kind} {prRef(e.data)}{subject ? <> to{s}</> : null}</> : <>unlinked {kind} {prRef(e.data)}{from}</>
    }
    case 'assignee': {
      const { from: was, to } = e.data
      if (!to) return was ? <>unassigned {was.name}{from}</> : <>unassigned{s ?? ' the issue'}</>
      return <>assigned{s} to {to.id === e.actor?.id ? 'themselves' : to.name}</>
    }
    case 'priority':
      return e.data.to === 0 ? <>removed priority{from}</> : <>set priority{of} to {priorityLabel(e.data.to)}</>
    case 'due_date':
      return e.data.to ? <>set the due date{of} to {formatDate(e.data.to)}</> : <>removed the due date{from}</>
    case 'labels': {
      const text = [e.data.added.length ? `added ${names(e.data.added)}` : '', e.data.removed.length ? `removed ${names(e.data.removed)}` : '']
        .filter(Boolean)
        .join(', ')
      return text ? <>{text}{subject ? <> on{s}</> : null}</> : null
    }
    case 'parent':
      return e.data.to ? (
        <>made {subject ?? 'this'} a sub-issue of {ref(e.data.to)}</>
      ) : e.data.from ? (
        <>removed the parent {ref(e.data.from)}{from}</>
      ) : null
    case 'cycle':
      return e.data.to ? <>moved{s} to {e.data.to.name}</> : e.data.from ? <>removed{s} from {e.data.from.name}</> : null
    case 'link':
      return e.data.removed ? (
        <>removed the link{from} to {ref(e.data.issue)}</>
      ) : (
        <>marked {subject ?? 'this'} as {e.data.type === 'blocks' ? 'blocking' : 'blocked by'} {ref(e.data.issue)}</>
      )
    case 'test_run': {
      const d = e.data
      const tally = (['passed', 'failed', 'skipped', 'error'] as const).filter((k) => d[k]).map((k) => `${d[k]} ${k}`).join(', ')
      return <>ran {d.kind === 'ai' ? 'AI tests' : d.kind === 'replay' ? 'recorded tests' : 'tests manually'}{subject ? <> on{s}</> : null}{tally ? `: ${tally}` : ''}</>
    }
    case 'test_cases_regenerated':
      return <>regenerated the test cases{subject ? <> on{s}</> : null}</>
    default:
      return null
  }
}

// Who did it: the person, or GitHub / GitLab for changes a pull request made.
export function eventActor(e: IssueEvent) {
  if (e.actor) return e.actor.name
  const provider = e.kind === 'pr_linked' || e.kind === 'pr_unlinked' ? e.data.provider : e.kind === 'status' ? e.data.via?.provider : undefined
  return provider ? PROVIDER_NAME[provider] : 'Someone'
}
