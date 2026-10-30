// Field validators: return an error message, or null when valid. Messages are shown as-is under the input.
type Check = (value: string) => string | null

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
const USERNAME = /^[a-z0-9][a-z0-9_.-]*$/i

export const required = (label: string): Check => (v) => (v.trim() ? null : `${label} is required.`)

export const email: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Email is required.'
  if (s.length > 254 || !EMAIL.test(s)) return 'Enter a valid email address.'
  return null
}

export const username: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Username is required.'
  if (s.length < 3 || s.length > 32) return 'Username must be 3 to 32 characters.'
  if (!USERNAME.test(s)) return 'Use letters, numbers, dots, dashes or underscores, starting with a letter or number.'
  return null
}

export const name: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Name is required.'
  if (s.length > 100) return 'Name must be 100 characters or fewer.'
  return null
}

// Length only (NIST 800-63B); 72 bytes is bcrypt's input limit.
export const password: Check = (v) => {
  if (!v) return 'Password is required.'
  if (v.length < 8) return 'Password must be at least 8 characters.'
  if (new TextEncoder().encode(v).length > 72) return 'Password is too long.'
  return null
}

export const usernameOrEmail: Check = (v) => (v.includes('@') ? email(v) : username(v))

const SLUG = /^[a-z0-9]+(-[a-z0-9]+)*$/
// Top-level app paths and likely future ones; mirrored in backend/user/internal/workspace/validate.go.
const RESERVED_SLUGS = new Set(
  'api admin app signin signup signout forgot-password reset-password new-workspace settings account invite invites join help support docs status about pricing blog www mail static assets'.split(' '),
)

export const workspaceName: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Workspace name is required.'
  if (s.length > 50) return 'Workspace name must be 50 characters or fewer.'
  return null
}

export const workspaceSlug: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Workspace URL is required.'
  if (s.length < 3 || s.length > 32) return 'Workspace URL must be 3 to 32 characters.'
  if (!SLUG.test(s)) return 'Use lowercase letters, numbers and single hyphens.'
  if (RESERVED_SLUGS.has(s)) return 'That URL is reserved.'
  return null
}

// "Acme, Inc." → "acme-inc". Accents are dropped ("Café" → "cafe"). Cut after trimming, then trim again so a
// cut never ends on a hyphen.
export const slugify = (name: string) =>
  name
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 32)
    .replace(/-+$/, '')
