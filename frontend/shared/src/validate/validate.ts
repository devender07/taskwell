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

export const roleName: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Role name is required.'
  if (s.length > 40) return 'Role name must be 40 characters or fewer.'
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

const PROJECT_KEY = /^[A-Z][A-Z0-9]{1,4}$/

export const projectName: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Project name is required.'
  if (s.length > 60) return 'Project name must be 60 characters or fewer.'
  return null
}

export const projectKeyFormat: Check = (v) => {
  const s = v.trim().toUpperCase()
  if (!s) return 'Project key is required.'
  if (!PROJECT_KEY.test(s)) return 'Use 2 to 5 letters or numbers, starting with a letter.'
  return null
}

export const milestoneName: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Milestone name is required.'
  if (s.length > 60) return 'Milestone name must be 60 characters or fewer.'
  return null
}

export const projectDescription: Check = (v) => (v.trim().length > 10000 ? 'Description must be 10,000 characters or fewer.' : null)

// A key suggestion from the name: "Mobile app" → "MA", "Engineering" → "ENG". '' when nothing usable.
export function projectKey(name: string) {
  const words = name
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toUpperCase()
    .split(/[^A-Z0-9]+/)
    .filter(Boolean)
  if (words.length > 1) return words.map((w) => w[0]).join('').replace(/^[0-9]+/, '').slice(0, 5)
  return (words[0] ?? '').replace(/^[0-9]+/, '').slice(0, 3)
}

export const issueTitle: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Title is required.'
  if (s.length > 200) return 'Title must be 200 characters or fewer.'
  return null
}

// Takes the rich text's length (text + mention labels, UTF-16 units), as RichEditor's onChange reports it.
export const issueDescription = (length: number) => (length > 50000 ? 'Description must be 50,000 characters or fewer.' : null)

export const commentBody = (empty: boolean, length: number) =>
  empty ? 'Write a comment.' : length > 20000 ? 'Comment must be 20,000 characters or fewer.' : null

const DATE = /^(\d{4})-(\d{2})-(\d{2})$/

// Empty (no date) or a real calendar date as YYYY-MM-DD.
export const dueDate: Check = (v) => {
  if (!v) return null
  const m = DATE.exec(v)
  const d = m ? new Date(Date.UTC(+m[1]!, +m[2]! - 1, +m[3]!)) : null
  return d && d.toISOString().slice(0, 10) === v ? null : 'Enter a valid date.'
}

export const statusName: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Status name is required.'
  if (s.length > 30) return 'Status name must be 30 characters or fewer.'
  return null
}

const STATUS_CATEGORIES = ['backlog', 'unstarted', 'started', 'completed', 'canceled'] as const

export const statusCategory: Check = (v) => ((STATUS_CATEGORIES as readonly string[]).includes(v) ? null : 'Choose a category.')

export const statusTypeName: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Type name is required.'
  if (s.length > 30) return 'Type name must be 30 characters or fewer.'
  return null
}

export const labelName: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Label name is required.'
  if (s.length > 30) return 'Label name must be 30 characters or fewer.'
  return null
}

export const labelColor: Check = (v) => (/^#[0-9a-f]{6}([0-9a-f]{2})?$/.test(v.toLowerCase()) ? null : 'Choose a color.')

export const groupName: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Name the group.'
  if (s.length > 80) return 'Keep the name under 80 characters.'
  return null
}

export const messageBody = (empty: boolean, length: number) =>
  empty ? 'Write a message.' : length > 10000 ? 'Message must be 10,000 characters or fewer.' : null

// Test cases and AI testing settings; messages match the API's.
export const testCaseTitle: Check = (v) => (v.trim() ? null : 'Enter a title.')
export const testCaseSteps = (steps: string[]) => (steps.length ? null : 'Add at least one step.')
export const testCaseExpected: Check = (v) => (v.trim() ? null : 'Describe the expected result.')
export const testNote: Check = (v) => (v.length > 2000 ? 'Note must be 2,000 characters or fewer.' : null)

// http:// passes here for dev sites; the API decides whether this server allows it.
export const testSiteUrl: Check = (v) => {
  try {
    const u = new URL(v.trim())
    return (u.protocol === 'https:' || u.protocol === 'http:') && u.hostname ? null : 'Enter a URL starting with https://.'
  } catch {
    return 'Enter a URL starting with https://.'
  }
}
export const testAccountLabel: Check = (v) => (v.trim() ? null : 'Enter a label.')
export const testAccountUsername: Check = (v) => (v.trim() ? null : 'Enter a username.')
export const testAccountPassword: Check = (v) => (v ? null : 'Enter a password.')

// http:// passes here for dev; the API rejects it in production and checks the address it resolves to.
export const webhookUrl: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Enter a URL.'
  let u: URL
  try {
    u = new URL(s)
  } catch {
    return 'Enter a valid URL.'
  }
  if (u.protocol !== 'https:' && u.protocol !== 'http:') return 'Use an https URL.'
  return u.hostname && s.length <= 2000 ? null : 'Enter a valid URL.'
}
export const webhookEvents = (events: string[]) => (events.length ? null : 'Choose at least one event.')

export const tokenName: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Name the token.'
  if (s.length > 60) return 'Use 60 characters or fewer.'
  return null
}

// Customer feedback; messages match the API's. The public form requires an email, a member's entry doesn't.
export const feedbackBody: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Write the feedback.'
  if (s.length > 5000) return 'Feedback must be 5,000 characters or fewer.'
  return null
}
export const feedbackEmail = (v: string, needed: boolean) => {
  const s = v.trim()
  if (!s) return needed ? 'Enter your email.' : null
  return s.length > 254 || !EMAIL.test(s) ? 'Enter a valid email.' : null
}
export const feedbackName: Check = (v) => (v.trim().length > 80 ? 'Name must be 80 characters or fewer.' : null)

// For a field whose only check is being filled in, with the message to show when it isn't.
export const filled = (message: string): Check => (v) => (v.trim() ? null : message)

export const viewName: Check = (v) => {
  const s = v.trim()
  if (!s) return 'Enter a name.'
  return s.length > 60 ? 'Use 60 characters or fewer.' : null
}
