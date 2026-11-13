import { Link } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { Button } from '@taskwell/shared/button'
import { Dialog } from '@taskwell/shared/dialog'
import { useToast } from '@taskwell/shared/toast'
import { api, deleteToken, tokensPath, type ApiToken } from '@/api'

const KEY = 'taskwell:review-tokens'
const EVENT = 'taskwell:review-tokens'

// After a password change or reset: API tokens don't depend on the password, so the next workspace page offers to
// revoke them. A reset sets the flag before signing in; the settings form sets it and the prompt shows at once.
export function askToReviewTokens() {
  try {
    sessionStorage.setItem(KEY, '1')
  } catch {}
  dispatchEvent(new Event(EVENT))
}

function flagged() {
  try {
    return sessionStorage.getItem(KEY) === '1'
  } catch {
    return false
  }
}

function clear() {
  try {
    sessionStorage.removeItem(KEY)
  } catch {}
}

export function ReviewTokensPrompt({ slug }: { slug: string }) {
  const { show } = useToast()
  const [tokens, setTokens] = useState<ApiToken[]>([])
  const [pending, setPending] = useState(false)

  useEffect(() => {
    const check = () => {
      if (!flagged()) return
      api<ApiToken[]>(tokensPath).then((ts) => (ts.length ? setTokens(ts) : clear()), () => {})
    }
    check()
    addEventListener(EVENT, check)
    return () => removeEventListener(EVENT, check)
  }, [])

  function close() {
    clear()
    setTokens([])
  }

  async function revokeAll() {
    setPending(true)
    const failed = (await Promise.allSettled(tokens.map((t) => deleteToken(t.id)))).filter((r) => r.status === 'rejected').length
    setPending(false)
    close()
    show({ message: failed ? `${failed} ${failed === 1 ? "token couldn't" : "tokens couldn't"} be revoked. Try again in Settings.` : 'API tokens revoked' })
  }

  const n = tokens.length
  return (
    <Dialog
      open={n > 0}
      onClose={close}
      title="Revoke your API tokens?"
      description={`Your password changed, but ${n === 1 ? 'your API token still works' : `your ${n} API tokens still work`}. Revoke ${n === 1 ? 'it' : 'them'} if you think someone else had your password. AI tools using ${n === 1 ? 'it' : 'them'} will need to connect again.`}
    >
      <div className="flex flex-wrap justify-end gap-3">
        <Button variant="ghost" onClick={close} disabled={pending}>
          Keep {n === 1 ? 'it' : 'them'}
        </Button>
        <Link to="/$workspace/settings/api-tokens" params={{ workspace: slug }} className="button button-secondary" onClick={close}>
          Review tokens
        </Link>
        <Button variant="danger" onClick={revokeAll} disabled={pending}>
          {pending ? 'Revoking…' : n === 1 ? 'Revoke token' : 'Revoke all'}
        </Button>
      </div>
    </Dialog>
  )
}
