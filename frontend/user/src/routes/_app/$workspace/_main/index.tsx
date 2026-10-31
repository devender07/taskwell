import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { clearNotice, readNotice } from '@/realtime'

export const Route = createFileRoute('/_app/$workspace/_main/')({
  component: WorkspaceHome,
})

function WorkspaceHome() {
  const { user } = Route.useRouteContext()
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  const [notice] = useState(readNotice)
  useEffect(clearNotice, [])
  return (
    <main className="grid gap-2 p-6">
      {notice ? <p role="status" className="text-caption text-secondary m-0">{notice}</p> : null}
      <h1 className="text-h2">Hi {user.name}</h1>
      <p className="text-body-sm text-secondary">{workspace.name}</p>
    </main>
  )
}
