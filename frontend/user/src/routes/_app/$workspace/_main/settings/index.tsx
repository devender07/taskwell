import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_app/$workspace/_main/settings/')({
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/$workspace/settings/profile', params, replace: true })
  },
})
