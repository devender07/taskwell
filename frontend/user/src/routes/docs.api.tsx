import { createFileRoute } from '@tanstack/react-router'
import { ApiDocs } from '@/components/docs/api-docs'

export const Route = createFileRoute('/docs/api')({
  component: ApiDocs,
})
