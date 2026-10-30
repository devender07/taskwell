import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_app/')({
  component: Dashboard,
})

function Dashboard() {
  const { user } = Route.useRouteContext()
  return (
    <main className="p-6">
      <h1 className="text-h2">Hi {user.name}</h1>
    </main>
  )
}
