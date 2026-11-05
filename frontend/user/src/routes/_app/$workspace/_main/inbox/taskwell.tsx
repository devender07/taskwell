import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { TaskwellBot } from '@/components/inbox/taskwell-bot'

export const Route = createFileRoute('/_app/$workspace/_main/inbox/taskwell')({
  component: TaskwellPage,
})

function TaskwellPage() {
  const { workspace } = Route.useParams()
  const { bot } = useLoaderData({ from: '/_app/$workspace/_main/inbox' })
  return <TaskwellBot key={workspace} slug={workspace} page={bot} />
}
