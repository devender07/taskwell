import { useRouter } from '@tanstack/react-router'
import { useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { useToast } from '@taskwell/shared/toast'
import { deleteNote, saveNote, type Note } from '@/api'
import { reloadPage } from '@/realtime'

const count = (n: number) => `${n} ${n === 1 ? 'note' : 'notes'}`

// Archive (or unarchive) and delete for the selected notes, one request each. Delete removes the caller's own notes
// for everyone and takes the shared ones out of their notes only.
// ponytail: one request per note, fine for the dozens a person selects; a bulk endpoint if that grows.
export function NotesBulkBar({ slug, notes, me, onClear }: { slug: string; notes: Note[]; me: string; onClear: () => void }) {
  const router = useRouter()
  const { show } = useToast()
  const [pending, setPending] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const archived = notes.every((n) => n.archived)
  const shared = notes.filter((n) => n.owner.id !== me).length

  async function run(each: (n: Note) => Promise<unknown>, done: string) {
    setPending(true)
    const results = await Promise.allSettled(notes.map(each))
    const failed = results.filter((r) => r.status === 'rejected').length
    await reloadPage(router)
    setPending(false)
    setConfirming(false)
    onClear()
    show({ message: failed ? `${count(failed)} couldn't be changed. Try again.` : done })
  }

  return (
    <div className="bulk-bar" role="region" aria-label="Selected notes">
      <span className="text-label" style={{ paddingInline: 8 }}>{notes.length} selected</span>
      <Button variant="ghost" onClick={onClear} disabled={pending}>
        Clear
      </Button>
      <Button
        variant="ghost"
        disabled={pending}
        onClick={() =>
          run((n) => saveNote(slug, { archived: archived ? '0' : '1' }, n.id), `${count(notes.length)} ${archived ? 'unarchived' : 'archived'}`)
        }
      >
        {archived ? 'Unarchive' : 'Archive'}
      </Button>
      <Button variant="ghost" disabled={pending} onClick={() => setConfirming(true)}>
        Delete
      </Button>
      {pending ? <span className="text-caption text-secondary">Saving…</span> : null}
      <Dialog
        open={confirming}
        onClose={() => setConfirming(false)}
        title={`Delete ${count(notes.length)}?`}
        description={
          shared
            ? `Yours are deleted for everyone. ${count(shared)} shared with you ${shared === 1 ? 'is' : 'are'} only removed from your notes.`
            : "They're deleted for everyone they're shared with. This can't be undone."
        }
      >
        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={() => setConfirming(false)} disabled={pending}>
            Cancel
          </Button>
          <Button variant="danger" disabled={pending} onClick={() => run((n) => deleteNote(slug, n.id), `${count(notes.length)} deleted`)}>
            {pending ? 'Deleting…' : 'Delete'}
          </Button>
        </div>
      </Dialog>
    </div>
  )
}
