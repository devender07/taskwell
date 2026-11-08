import type { Plan } from '@/api'
import { formatBytes } from '@/components/files/bytes'

// The check list on a pricing card, on the billing page and the landing page.
export const planChecks = (p: Plan) => [
  p.members === null ? 'Unlimited members' : `Up to ${p.members} members`,
  p.storage_bytes === null ? 'Unlimited storage' : `${formatBytes(p.storage_bytes)} storage`,
  'Every Taskwell feature',
]
