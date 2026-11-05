import { useRouter } from '@tanstack/react-router'
import { useRef, useState } from 'react'
import { Avatar } from '@taskwell/shared/avatar'
import { Button } from '@taskwell/shared/button'
import { api, bumpAvatar, toFormErrors, type User } from '@/api'
import { FormError } from '@/components/auth/auth-card'

export function AvatarForm({ user }: { user: User }) {
  const router = useRouter()
  const picker = useRef<HTMLInputElement>(null)
  const [pending, setPending] = useState<'upload' | 'remove' | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function save(file?: File) {
    setPending(file ? 'upload' : 'remove')
    setError(null)
    const form = new FormData()
    if (file) form.set('file', file)
    try {
      await api<User>('/me/avatar', file ? form : undefined, file ? 'POST' : 'DELETE')
      bumpAvatar(user.id)
      await router.invalidate()
    } catch (e) {
      const { errors, error } = toFormErrors(e)
      setError(errors.file ?? error)
    }
    setPending(null)
  }

  return (
    <div className="grid gap-4">
      <h2 className="text-label">Avatar</h2>
      <div className="flex items-center gap-4 flex-wrap">
        <Avatar name={user.name} size={64} src={user.avatar_url} />
        <div className="flex items-center gap-3">
          <Button variant="secondary" disabled={!!pending} onClick={() => picker.current?.click()}>
            {pending === 'upload' ? 'Uploading…' : 'Upload image'}
          </Button>
          {user.avatar_url ? (
            <Button variant="ghost" disabled={!!pending} onClick={() => save()}>
              {pending === 'remove' ? 'Removing…' : 'Remove'}
            </Button>
          ) : null}
        </div>
        <input
          ref={picker}
          type="file"
          accept="image/png,image/jpeg,image/gif,image/webp"
          hidden
          onChange={(e) => {
            const file = e.currentTarget.files?.[0]
            e.currentTarget.value = ''
            if (file) void save(file)
          }}
        />
      </div>
      <FormError>{error}</FormError>
    </div>
  )
}
