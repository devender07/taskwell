import type { Plan } from '@/api'
import { formatBytes } from '@/components/files/bytes'

const count = (n: number, one: string) => `${n} ${n === 1 ? one : `${one}s`}`

// The check list on a pricing card, on the billing page and the landing page.
export const planChecks = (p: Plan) => [
  p.workspaces === null ? 'Unlimited workspaces' : count(p.workspaces, 'workspace'),
  p.projects === null ? 'Unlimited projects' : `${count(p.projects, 'project')} per workspace`,
  p.members === null ? 'Unlimited members' : `Up to ${p.members} members per workspace`,
  p.storage_bytes === null ? 'Unlimited storage' : `${formatBytes(p.storage_bytes)} storage per workspace`,
  'Every Taskwell feature',
]
