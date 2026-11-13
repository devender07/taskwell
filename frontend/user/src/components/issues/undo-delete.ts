import { useRouter } from '@tanstack/react-router'
import { useToast } from '@taskwell/shared/toast'
import { restoreIssue, toFormErrors } from '@/api'
import { reloadPage } from '@/realtime'

// After issues move to the trash: "Deleted ABC-12" (or "Deleted 4 issues") with Undo, which restores them all.
export function useUndoDelete(slug: string) {
  const { show } = useToast()
  const router = useRouter()
  return (identifiers: string[]) =>
    show({
      message: identifiers.length === 1 ? `Deleted ${identifiers[0]}` : `Deleted ${identifiers.length} issues`,
      action: {
        label: 'Undo',
        run: async () => {
          const results = await Promise.allSettled(identifiers.map((id) => restoreIssue(slug, id)))
          const failed = results.find((r) => r.status === 'rejected')
          if (failed) show({ message: toFormErrors(failed.reason).error ?? "Couldn't restore. Try again from the trash." })
          await reloadPage(router)
        },
      },
    })
}
