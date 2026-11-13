import { createFileRoute, notFound, redirect } from '@tanstack/react-router'
import { api, workspaceBillingPath, type WorkspaceBilling } from '@/api'

// The billing emails' link: Settings → Billing in the first workspace the user owns, else their first non-guest one, else pricing.
export const Route = createFileRoute('/_app/billing')({
  beforeLoad: async ({ context: { workspaces, billing, beta } }) => {
    if (beta) throw notFound()
    const members = workspaces.filter((w) => !w.guest) // guests can't open Settings → Billing
    // ponytail: one billing GET per workspace to find an owned one; add an owner flag to /workspaces if lists grow.
    const owned = billing?.workspaces.used ? await Promise.all(members.map((w) => api<WorkspaceBilling>(workspaceBillingPath(w.slug)).then((b) => b.is_owner, () => false))) : []
    const target = members[owned.indexOf(true)] ?? members[0]
    if (!target) throw redirect({ to: '/pricing', replace: true })
    throw redirect({ to: '/$workspace/settings/billing', params: { workspace: target.slug }, replace: true })
  },
})
