import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import type { ActivityPage, Issue, NotificationPage, Project, ProjectStats, Status } from '@/api'
import { clearNotice, loaderApi, readNotice } from '@/realtime'
import { Activity, AssignedToMe, Inbox, NeedsAttention, Projects, RecentlyViewed } from '@/components/home/blocks'
import { greeting, splitMine } from '@/components/home/home'

export const Route = createFileRoute('/_app/$workspace/_main/')({
  loader: async (ctx) => {
    const path = `/workspaces/${encodeURIComponent(ctx.params.workspace)}`
    const [issues, statuses, notifications, activity, projects, stats] = await Promise.all([
      loaderApi<Issue[]>(ctx, `${path}/my-issues`),
      loaderApi<Status[]>(ctx, `${path}/statuses`),
      loaderApi<NotificationPage>(ctx, `${path}/notifications`),
      loaderApi<ActivityPage>(ctx, `${path}/activity`),
      loaderApi<Project[]>(ctx, `${path}/projects`),
      loaderApi<ProjectStats[]>(ctx, `${path}/project-stats`),
    ])
    return { issues, statuses, notifications, activity, projects, stats }
  },
  component: WorkspaceHome,
})

function WorkspaceHome() {
  const { user } = Route.useRouteContext()
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const { issues, statuses, notifications, activity, projects, stats } = Route.useLoaderData()
  const [notice] = useState(readNotice)
  useEffect(clearNotice, [])
  const [now] = useState(() => new Date())
  const { attention, rest } = splitMine(issues, now)

  return (
    <main className="grid gap-6 w-full px-6 py-10" style={{ alignContent: 'start' }}>
      <header className="grid gap-1">
        {notice ? <p role="status" className="text-caption text-secondary m-0">{notice}</p> : null}
        <h1 className="text-h2">
          {greeting(now)}, {user.name.split(' ')[0]}
        </h1>
        <p className="text-body-sm text-secondary m-0">{now.toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' })}</p>
      </header>
      <div className="home-grid" key={workspace.slug}>
        <div className="grid gap-6" style={{ minWidth: 0 }}>
          <NeedsAttention workspace={workspace.slug} issues={attention} />
          <AssignedToMe workspace={workspace} issues={rest} statuses={statuses} others={attention.length > 0} />
          <Activity workspace={workspace.slug} page={activity} />
        </div>
        <div className="grid gap-6" style={{ minWidth: 0 }}>
          <Inbox workspace={workspace.slug} page={notifications} />
          <RecentlyViewed workspace={workspace.slug} issues={issues} />
          <Projects workspace={workspace} projects={projects} stats={stats} />
        </div>
      </div>
    </main>
  )
}
