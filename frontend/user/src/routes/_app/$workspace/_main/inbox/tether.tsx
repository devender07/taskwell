import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { TetherBot } from '@/components/inbox/tether-bot'

export const Route = createFileRoute('/_app/$workspace/_main/inbox/tether')({
  component: TetherPage,
})

function TetherPage() {
  const { workspace } = Route.useParams()
  const { bot } = useLoaderData({ from: '/_app/$workspace/_main/inbox' })
  return <TetherBot key={workspace} slug={workspace} page={bot} />
}
