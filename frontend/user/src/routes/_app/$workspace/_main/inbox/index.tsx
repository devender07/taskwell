import { Navigate, createFileRoute, useLoaderData } from '@tanstack/react-router'

export const Route = createFileRoute('/_app/$workspace/_main/inbox/')({
  component: InboxIndex,
})

// Laptop screens open the workspace chat; below 1024px /inbox is the chat list itself.
function InboxIndex() {
  const { chats } = useLoaderData({ from: '/_app/$workspace/_main/inbox' })
  const search = Route.useSearch()
  const { workspace } = Route.useParams()
  const main = chats.find((c) => c.kind === 'workspace')
  if (!main || !matchMedia('(min-width: 1024px)').matches) return null
  return <Navigate to="/$workspace/inbox/c/$chatId" params={{ workspace, chatId: main.id }} search={search} replace />
}
