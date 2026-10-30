import { createFileRoute, useLoaderData } from '@tanstack/react-router'

export const Route = createFileRoute('/_app/$workspace/_main/')({
  component: WorkspaceHome,
})

function WorkspaceHome() {
  const { user } = Route.useRouteContext()
  const workspace = useLoaderData({ from: '/_app/$workspace' })
  return (
    <main className="grid gap-2 p-6">
      <h1 className="text-h2">Hi {user.name}</h1>
      <p className="text-body-sm text-secondary">{workspace.name}</p>
    </main>
  )
}
