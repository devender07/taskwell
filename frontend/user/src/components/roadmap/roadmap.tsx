import { Link } from '@tanstack/react-router'
import { LockIcon } from '@taskwell/shared/icons'
import { useLayoutEffect, type CSSProperties, type RefObject } from 'react'
import type { RoadmapProject, Workspace } from '@/api'
import { formatDate, formatRange } from '@/components/issues/fields'
import { localDate } from '@/components/home/home'
import { months, percentDone, ticks, timeline, widthOf, xOf, type Timeline, type Zoom } from './timeline'
import '@/components/projects/projects.css'
import './roadmap.css'

const datesOf = (p: RoadmapProject) => [
  ...[p.start_date, p.target_date].filter((d): d is string => d !== null),
  ...p.milestones.map((m) => m.date),
  ...p.cycles.flatMap((c) => [c.starts_on, c.ends_on]),
]

// Scrolls the timeline so today sits in the middle of the part not covered by the sticky project column.
export function scrollToToday(el: HTMLElement, behavior: ScrollBehavior = 'smooth') {
  const today = el.querySelector<HTMLElement>('.roadmap-today')
  const lead = el.querySelector<HTMLElement>('.roadmap-lead')
  if (!today || !lead) return
  el.scrollTo({ left: today.offsetLeft - (el.clientWidth - lead.offsetWidth) / 2, behavior })
}

export function Roadmap({ workspace, projects, zoom, manage, scrollRef }: {
  workspace: Workspace
  projects: RoadmapProject[]
  zoom: Zoom
  manage: boolean
  scrollRef: RefObject<HTMLDivElement | null>
}) {
  const today = localDate(new Date())
  const tl = timeline(projects.flatMap(datesOf), today, zoom)
  const width = tl.days * tl.dayPx
  const monthMarks = months(tl)
  const tickMarks = zoom === 'year' ? [] : ticks(tl, zoom)

  useLayoutEffect(() => {
    if (scrollRef.current) scrollToToday(scrollRef.current, 'instant')
  }, [zoom, scrollRef])

  return (
    <div ref={scrollRef} className="roadmap bg-surface border rounded-lg" role="region" aria-label="Timeline" tabIndex={0}>
      <div className="relative" style={{ width: `calc(var(--lead) + ${width}px)` }}>
        <div className="roadmap-lines" style={{ width }} aria-hidden>
          {tickMarks.map((t) => (
            <span key={`t${t.x}`} className="roadmap-line" style={{ left: t.x }} />
          ))}
          {monthMarks.map((m) => (
            <span key={`m${m.x}`} className="roadmap-line" data-major="" style={{ left: m.x }} />
          ))}
          <span className="roadmap-today" style={{ left: xOf(tl, today) + tl.dayPx / 2 }} />
        </div>

        <div className="roadmap-row roadmap-head" aria-hidden>
          <div className="roadmap-lead text-caption text-secondary">Projects</div>
          <div className="relative">
            {monthMarks.map((m) => (
              <span key={m.x} className="roadmap-month text-caption" style={{ left: m.x }}>{m.label}</span>
            ))}
            {tickMarks.map((t) => (
              <span key={t.x} className="roadmap-tick" style={{ left: t.x, width: zoom === 'month' ? tl.dayPx : undefined }}>{t.label}</span>
            ))}
          </div>
        </div>

        <ul className="m-0 p-0" style={{ listStyle: 'none' }} aria-label="Projects">
          {projects.map((p) => (
            <Row key={p.key} workspace={workspace} project={p} tl={tl} manage={manage} />
          ))}
        </ul>
      </div>
    </div>
  )
}

function Row({ workspace, project: p, tl, manage }: { workspace: Workspace; project: RoadmapProject; tl: Timeline; manage: boolean }) {
  const pct = percentDone(p.progress)
  const params = { workspace: workspace.slug, key: p.key }
  const only = p.start_date ?? p.target_date
  const both = p.start_date && p.target_date ? { from: p.start_date, to: p.target_date } : null

  return (
    <li className="roadmap-row">
      <div className="roadmap-lead">
        <span className="project-key flex-noshrink" style={{ width: 48 }}>{p.key}</span>
        {p.private ? <LockIcon size={14} className="text-secondary flex-noshrink" aria-label="Private" /> : null}
        <Link to="/$workspace/projects/$key" params={params} className="roadmap-name text-body-sm flex-grow ellipsis">
          {p.name}
        </Link>
        <span className="text-caption text-secondary flex-noshrink">{pct}%</span>
      </div>
      <div className="relative">
        {both ? (
          <Link
            to="/$workspace/projects/$key"
            params={params}
            className="roadmap-bar"
            style={{ left: xOf(tl, both.from), width: Math.max(widthOf(tl, both.from, both.to), 12), '--done': `${pct}%` } as CSSProperties}
            aria-label={`${p.name}, ${formatDate(both.from)} to ${formatDate(both.to)}, ${pct}% done`}
          >
            <span className="roadmap-fill" aria-hidden />
          </Link>
        ) : only ? (
          <Link
            to="/$workspace/projects/$key"
            params={params}
            className="roadmap-dot-link"
            style={{ left: xOf(tl, only) + tl.dayPx / 2 }}
            aria-label={`${p.name}, ${p.start_date ? 'starts' : 'target'} ${formatDate(only)}, ${pct}% done`}
          >
            <span className="roadmap-dot" aria-hidden />
            <span className="text-caption text-secondary ellipsis" aria-hidden>
              {p.name} · {p.start_date ? 'starts' : 'target'} {formatDate(only)}
            </span>
          </Link>
        ) : (
          <span className="roadmap-undated text-caption text-secondary">
            No dates
            {manage ? (
              <Link to="/$workspace/projects/$key" params={params} search={{ edit: true }} className="roadmap-set">
                Set dates
              </Link>
            ) : null}
          </span>
        )}
        {p.cycles.map((c) => (
          <span
            key={c.number}
            className="roadmap-cycle"
            data-state={c.state}
            style={{ left: xOf(tl, c.starts_on), width: widthOf(tl, c.starts_on, c.ends_on) }}
            title={`${c.name} · ${formatRange(c)}`}
          />
        ))}
        {p.milestones.map((m) => (
          <span
            key={m.id}
            className="roadmap-milestone"
            style={{ left: xOf(tl, m.date) + tl.dayPx / 2 }}
            role="img"
            aria-label={`Milestone ${m.name}, ${formatDate(m.date)}`}
            title={`${m.name} · ${formatDate(m.date)}`}
          >
            <span className="milestone-diamond" />
          </span>
        ))}
      </div>
    </li>
  )
}
